#!/usr/bin/env python3
"""Check source-proof contracts freshly and bind evidence to generated Go."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile

from export import (DEFAULT_COMPILER, ExportError, parse_json, selected_compiler,
                    publish_files, run)
from export_module import MAX_SOURCE_BYTES, check_python, lake_config
from composition_contract import composition_inputs, load_composition, manifest_bindings
from proof_contract import (digest, load_contract, read_regular,
                            snapshot, verify_manifest)

TOOL_ROOT = Path(__file__).resolve().parents[1]
CHECKER = 'producer/LeanGo/Proofs/Check.lean'
TOOL_INPUTS = ('scripts/check_proofs.py', 'scripts/composition_contract.py', 'scripts/proof_contract.py', 'scripts/export.py',
               'scripts/export_module.py', 'scripts/diagnostics.py', 'scripts/source_locations.py',
               'scripts/module_diagnostics.py', 'producer/lean-toolchain', CHECKER)
AXIOMS = frozenset({'propext', 'Classical.choice', 'Quot.sound'})


def tool_snapshot(tool_root=TOOL_ROOT):
    sources = {relative: read_regular(Path(tool_root) / relative) for relative in TOOL_INPUTS}
    if sources['producer/lean-toolchain'] != DEFAULT_COMPILER.pin_file:
        raise ExportError('proof toolchain configuration differs from the pin')
    return sources


def validate_claims(claims, contract):
    if not isinstance(claims, list) or len(claims) != len(contract.claims):
        raise ExportError('unexpected checked theorem inventory')
    for (name, expected), claim in zip(contract.claims, claims, strict=True):
        if (not isinstance(claim, dict) or set(claim) != {'name', 'expected', 'statement', 'axioms'} or
                claim['name'] != name or claim['expected'] != expected or
                not isinstance(claim['statement'], str) or not claim['statement']):
            raise ExportError('malformed checked theorem evidence')
        axioms = claim['axioms']
        if (not isinstance(axioms, list) or
                any(not isinstance(axiom, str) or axiom not in AXIOMS for axiom in axioms) or
                axioms != sorted(set(axioms))):
            raise ExportError('unapproved or malformed theorem axiom evidence')
    return claims


def checker_command(lean, checker, lib, modules, contract):
    """The staged checker program: lean --run CHECKER LIB MODULE... -- THEOREM EXPECTED ..."""
    pairs = [name for claim in contract.claims for name in claim]
    return [lean, '--run', str(checker), str(lib), *sorted(modules), '--', *pairs]


def workspace_sources(workspace):
    sources = set()
    for directory, dirs, names in os.walk(workspace, followlinks=False):
        base = Path(directory)
        if any((base / name).is_symlink() for name in dirs):
            raise ExportError('proof workspace source directory is symlinked')
        if base == workspace:
            dirs[:] = [name for name in dirs if name != '.lake']
        sources.update((base / name).relative_to(workspace).as_posix()
                       for name in names if name.endswith('.lean'))
    return sources


def verify_workspace(workspace, files):
    if workspace_sources(workspace) != {name for name in files if name.endswith('.lean')}:
        raise ExportError('proof workspace source inventory changed')
    for relative, data in files.items():
        if read_regular(workspace / relative, MAX_SOURCE_BYTES) != data:
            raise ExportError(f'proof workspace input changed: {relative}')
    lock = parse_json(read_regular(workspace / 'lake-manifest.json'))
    if not isinstance(lock, dict) or lock.get('packages') != []:
        raise ExportError('proof workspace must have no Lake dependencies')


MAX_OLEAN_BYTES = 256 * 1024 * 1024


def olean_digests(lib, modules):
    digests = {}
    for module, (relative, _data) in modules.items():
        path = lib / (relative[:-len('.lean')] + '.olean')
        try:
            digests[module] = digest(read_regular(path, MAX_OLEAN_BYTES))
        except ExportError as exc:
            raise ExportError(f'proof workspace module was not built: {module}') from exc
    return digests


def prove(contract, project, tool_inputs, compiler):
    """Build only snapshots in fixed, dependency-free scaffolding, then decide
    the claims with the checker program in its own process outside the
    workspace (producer/LeanGo/Proofs/Check.lean), as the Go command does."""
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('LEAN_', 'LAKE_'))}
    env['ELAN_TOOLCHAIN'] = compiler.toolchain
    lean, lake = shutil.which('lean'), shutil.which('lake')
    if not lean or not lake:
        raise ExportError('pinned Lean and Lake executables must be available on PATH')
    with tempfile.TemporaryDirectory(prefix='lean-go-source-proofs-') as directory, \
            tempfile.TemporaryDirectory(prefix='lean-go-proof-checker-') as staging:
        workspace = Path(directory)
        files = {relative: data for relative, data in project.modules.values()}
        files.update({'lean-toolchain': compiler.pin_file, 'lakefile.toml': lake_config(project.modules)})
        for relative, data in files.items():
            target = workspace / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        compiler.check_version(run([lean, '--version'], workspace, env))
        compiler.check_version(run([lake, 'env', 'lean', '--version'], workspace, env))
        verify_workspace(workspace, files)
        # Trusted runtime modules are built alone first; their .olean files must
        # be unchanged after the untrusted proof modules are built.
        lib = workspace / '.lake/build/lib/lean'
        run([lake, 'build', *sorted(project.runtime)], workspace, env)
        verify_workspace(workspace, files)
        runtime = olean_digests(lib, project.runtime)
        run([lake, 'build', *sorted(project.modules)], workspace, env)
        verify_workspace(workspace, files)
        if olean_digests(lib, project.runtime) != runtime:
            raise ExportError('proof workspace runtime build changed while proof modules were built')
        # Staged only now, outside the workspace, so build code cannot replace it.
        checker = Path(staging) / Path(CHECKER).name
        with checker.open('xb') as output:
            output.write(tool_inputs[CHECKER])
        result = run(checker_command(lean, checker, lib, project.modules, contract), Path(staging), env)
        verify_workspace(workspace, files)
        try:
            claims = parse_json(result.encode())
        except ExportError as exc:
            raise ExportError(f'malformed proof checker output: {exc}') from exc
        return validate_claims(claims, contract)


def rebuild_package(manifest_path, contract):
    """Regenerate the package from the contract's sources with the Go command
    and require the bound package to equal it byte for byte (see
    internal/pipeline/proof_rebuild.go, which reports the cause in detail)."""
    from generate import go_cli
    manifest = parse_json(read_regular(manifest_path))
    mode = 'native' if isinstance(manifest, dict) and manifest.get('mode') == 'native' else 'metered'
    with tempfile.TemporaryDirectory(prefix='lean-go-rebuild-') as directory:
        out, ir = Path(directory) / 'package', Path(directory) / 'package.ir.json'
        arguments = ['generate', '--source-dir', str(contract.source_dir), '--module', contract.module,
                     *[item for root in contract.roots for item in ('--root', root)],
                     '--out', str(out), '--ir-output', str(ir), '--mode', mode]
        if go_cli(arguments) != 0:
            raise ExportError('generated package cannot be rebuilt from the contract sources')
        depth = manifest.get('max_depth') if mode == 'native' else None
        if depth is not None and depth != 10000:
            out = Path(directory) / 'emitted'
            if go_cli(['emit', '--in', str(ir), '--out', str(out), '--mode', 'native',
                       '--native-max-depth', str(depth)]) != 0:
                raise ExportError('generated package cannot be re-emitted')
        package = Path(manifest_path).absolute().parent
        fresh = {path.name: path.read_bytes() for path in out.iterdir()}
        found = {path.name for path in package.iterdir()}
        if found != set(fresh) or any(read_regular(package / name) != data for name, data in fresh.items()):
            raise ExportError('generated package differs from a fresh rebuild of its sources; regenerate the package')


def evidence(contract_path, manifest_path, tool_root=TOOL_ROOT):
    check_python()
    compiler = selected_compiler()
    contract = load_contract(contract_path)
    project, tools = snapshot(contract), tool_snapshot(tool_root)
    manifest = verify_manifest(manifest_path, contract, project, compiler)
    rebuild_package(manifest_path, contract)
    claims = prove(contract, project, tools, compiler)
    if (load_contract(contract_path) != contract or snapshot(contract) != project or
            tool_snapshot(tool_root) != tools or
            verify_manifest(manifest_path, contract, project, compiler) != manifest):
        raise ExportError('proof inputs or generated package changed during checking')
    result = {
        'schema': 'lean-go-proof-evidence/v3',
        'lean_version': compiler.version, 'lean_commit': compiler.commit,
        'module': contract.module, 'roots': list(contract.roots),
        'contract_sha256': digest(contract.raw),
        'project_inputs': {name: digest(data) for name, data in project.inputs.items()},
        'tool_inputs': {name: digest(data) for name, data in tools.items()},
        'generated_manifest_sha256': digest(manifest),
        'claims': claims,
        'assurance': ('Kernel-replayed source statements; extraction and Go execution '
                      'remain tested and trusted.'),
    }
    return (json.dumps(result, sort_keys=True, indent=2, ensure_ascii=False) + '\n').encode()


def composition_evidence(contract, bindings, tool_root=TOOL_ROOT):
    check_python()
    compiler = selected_compiler()
    tools = tool_snapshot(tool_root)
    project, components = composition_inputs(contract, bindings, compiler)
    for name, component in contract.components.items():
        rebuild_package(bindings[name], component)
    claims = prove(contract, project, tools, compiler)
    fresh = load_composition(contract.path)
    if (fresh != contract or composition_inputs(fresh, bindings, compiler) != (project, components)
            or tool_snapshot(tool_root) != tools):
        raise ExportError('composition inputs or generated packages changed during checking')
    record = dict(schema='lean-go-proof-evidence/v4', lean_version=compiler.version,
                  lean_commit=compiler.commit, contract_sha256=digest(contract.raw),
                  components=components, claims=claims,
                  project_inputs={key: digest(data) for key, data in project.inputs.items()},
                  tool_inputs={key: digest(data) for key, data in tools.items()},
                  assurance='Kernel-replayed source statements; extraction and Go execution remain tested and trusted.')
    return (json.dumps(record, sort_keys=True, indent=2, ensure_ascii=False) + '\n').encode()


def validate_composition_output(path, contract, bindings):
    for name, component in contract.components.items():
        output = validate_evidence_path(path, component, bindings[name])
    if output == contract.path or output.is_relative_to(contract.proof_dir):
        raise ExportError('evidence must stay outside the composition inputs')
    return output


def validate_evidence_path(path, contract, manifest, tool_root=TOOL_ROOT):
    output = Path(path).absolute()
    try:
        resolved = output.resolve()
    except ValueError as exc:
        raise ExportError('invalid evidence path') from exc
    if resolved != output or (output.exists() and not output.is_file()):
        raise ExportError('evidence path must be a regular file without symlinks')
    protected = {contract.path} | {Path(tool_root) / name for name in TOOL_INPUTS}
    if output in {value.resolve() for value in protected}:
        raise ExportError('evidence output must not overwrite proof or tool inputs')
    for directory in (contract.source_dir, contract.proof_dir, Path(manifest).absolute().parent):
        if output.is_relative_to(directory.resolve()):
            raise ExportError('evidence must stay outside source, proof and generated directories')
    return output


def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--contract', type=Path, required=True)
    packages = parser.add_mutually_exclusive_group(required=True)
    packages.add_argument('--manifest', type=Path, help='generated manifest for a v1 contract')
    packages.add_argument('--component-manifest', nargs=2, action='append', metavar=('NAME', 'PATH'),
                          help='bind a v2 component by name; repeat once per component')
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--evidence', type=Path, help='freshly verify existing evidence')
    mode.add_argument('--output', type=Path, help='write freshly checked evidence')
    return parser


def go_main(argv=None):
    """Script entry point: proof checking runs in the Go lean-go command.

    Paths are relative to the working directory, as before.
    """
    from generate import go_cli
    args = arguments().parse_args(sys.argv[1:] if argv is None else argv)
    command = ['check-proofs', '--contract', str(args.contract.absolute())]
    if args.manifest is not None:
        command.extend(['--manifest', str(args.manifest.absolute())])
    for name, path in args.component_manifest or []:
        command.extend(['--component-manifest', name, str(Path(path).absolute())])
    if args.output is not None:
        command.extend(['--output', str(args.output.absolute())])
    else:
        command.extend(['--evidence', str(args.evidence.absolute())])
    try:
        return go_cli(command)
    except (ValueError, OSError) as exc:
        print(f'proof check failed: {exc}', file=sys.stderr)
        return 1


def main(argv=None):
    args = arguments().parse_args(argv)
    path = args.output or args.evidence
    if args.component_manifest:
        contract = load_composition(args.contract)
        bindings = manifest_bindings(contract, args.component_manifest)
        output = validate_composition_output(path, contract, bindings)
        fresh = composition_evidence(contract, bindings)
        if load_composition(args.contract) != contract:
            raise ExportError('composition contract changed during checking')
        validate_composition_output(output, contract, bindings)
    else:
        contract = load_contract(args.contract)
        output = validate_evidence_path(path, contract, args.manifest)
        fresh = evidence(args.contract, args.manifest)
        if load_contract(args.contract) != contract:
            raise ExportError('proof contract changed during checking')
        validate_evidence_path(output, contract, args.manifest)
    if args.output:
        publish_files([(output, fresh)])
        print(f'Checked {len(contract.claims)} source theorems and wrote bound proof evidence.')
    else:
        tracked = read_regular(output)
        parse_json(tracked)
        if tracked != fresh:
            raise ExportError('proof evidence is stale; regenerate with --output')
        print(f'Checked {len(contract.claims)} source theorems; evidence and generated files match.')
    return 0


if __name__ == '__main__':
    sys.exit(go_main())
