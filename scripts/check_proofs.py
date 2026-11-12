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

from export import (COMMIT, TOOLCHAIN, ExportError, check_version, parse_json,
                    publish_files, run)
from export_module import MAX_SOURCE_BYTES, check_python, lake_config
from proof_contract import (digest, load_contract, read_regular,
                            snapshot, verify_manifest)

TOOL_ROOT = Path(__file__).resolve().parents[1]
CHECKER = 'producer/LeanGo/Proofs/Check.lean'
TOOL_INPUTS = ('scripts/check_proofs.py', 'scripts/proof_contract.py', 'scripts/export.py',
               'scripts/export_module.py', 'producer/lean-toolchain', CHECKER)
AXIOMS = frozenset({'propext', 'Classical.choice', 'Quot.sound'})


def tool_snapshot(tool_root=TOOL_ROOT):
    sources = {relative: read_regular(Path(tool_root) / relative) for relative in TOOL_INPUTS}
    if sources['producer/lean-toolchain'] != TOOLCHAIN:
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


def harness(contract, modules):
    imports = ''.join(f'import {module}\n' for module in sorted(modules))
    pairs = ',\n    '.join(f'(``{name}, ``{expected})' for name, expected in contract.claims)
    return ('import LeanGo.Proofs.Check\n' + imports + '\nopen Lean Elab Command\n' +
            'elab "#lean_go_proof_check" : command => do\n' +
            '  let claims ← liftTermElabM <| LeanGo.Proofs.checkTheorems [\n    ' + pairs + ']\n' +
            '  IO.FS.writeFile "claims.json" ((toJson claims).pretty ++ "\\n")\n\n' +
            '#lean_go_proof_check\n').encode()


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


def prove(contract, project, tool_inputs):
    """Compile only snapshots in fixed, dependency-free scaffolding."""
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('LEAN_', 'LAKE_'))}
    env['ELAN_TOOLCHAIN'] = TOOLCHAIN.decode().strip()
    lean, lake = shutil.which('lean'), shutil.which('lake')
    if not lean or not lake:
        raise ExportError('pinned Lean and Lake executables must be available on PATH')
    with tempfile.TemporaryDirectory(prefix='lean-go-source-proofs-') as directory:
        workspace = Path(directory)
        files = {relative: data for relative, data in project.modules.values()}
        files.update({'lean-toolchain': TOOLCHAIN, 'lakefile.toml': lake_config(project.modules),
                      'LeanGo/Proofs/Check.lean': tool_inputs[CHECKER]})
        for relative, data in files.items():
            target = workspace / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        check_version(run([lean, '--version'], workspace, env))
        check_version(run([lake, 'env', 'lean', '--version'], workspace, env))
        verify_workspace(workspace, files)
        run([lake, 'build', 'LeanGo.Proofs.Check', *sorted(project.modules)], workspace, env)
        verify_workspace(workspace, files)
        harness_name = 'LeanGo/Proofs/Harness.lean'
        files[harness_name] = harness(contract, project.modules)
        # User elaboration may have written files; never follow or replace its harness.
        try:
            with (workspace / harness_name).open('xb') as output:
                output.write(files[harness_name])
        except FileExistsError as exc:
            raise ExportError('proof workspace reserved harness path already exists') from exc
        run([lake, 'env', 'lean', 'LeanGo/Proofs/Harness.lean'], workspace, env)
        verify_workspace(workspace, files)
        return validate_claims(parse_json(read_regular(workspace / 'claims.json')), contract)


def evidence(contract_path, manifest_path, tool_root=TOOL_ROOT):
    check_python()
    contract = load_contract(contract_path)
    project, tools = snapshot(contract), tool_snapshot(tool_root)
    manifest = verify_manifest(manifest_path, contract, project)
    claims = prove(contract, project, tools)
    if (load_contract(contract_path) != contract or snapshot(contract) != project or
            tool_snapshot(tool_root) != tools or
            verify_manifest(manifest_path, contract, project) != manifest):
        raise ExportError('proof inputs or generated package changed during checking')
    result = {
        'schema': 'lean-go-proof-evidence/v1',
        'lean_version': '4.35.0', 'lean_commit': COMMIT,
        'module': contract.module, 'roots': list(contract.roots),
        'contract_sha256': digest(contract.raw),
        'project_inputs': {name: digest(data) for name, data in project.inputs.items()},
        'tool_inputs': {name: digest(data) for name, data in tools.items()},
        'generated_manifest_sha256': digest(manifest),
        'claims': claims,
        'assurance': ('Kernel-checked source statements; extraction and Go execution '
                      'remain tested and trusted.'),
    }
    return (json.dumps(result, sort_keys=True, indent=2, ensure_ascii=False) + '\n').encode()


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


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--contract', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--evidence', type=Path, help='freshly verify existing evidence')
    mode.add_argument('--output', type=Path, help='write freshly checked evidence')
    args = parser.parse_args(argv)
    contract = load_contract(args.contract)
    output = validate_evidence_path(args.output or args.evidence, contract, args.manifest)
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
    try:
        sys.exit(main())
    except (ExportError, OSError) as exc:
        print(f'proof check failed: {exc}', file=sys.stderr)
        sys.exit(1)
