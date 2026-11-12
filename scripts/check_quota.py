#!/usr/bin/env python3
"""Check quota source proofs freshly and bind sidecar evidence to generated Go."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile

from export import (COMMIT, TOOLCHAIN, ExportError, check_version, parse_json,
                    publish_files, run)
from export_module import check_python

SOURCE = 'libraries/quota/source/Quota.lean'
PROOFS = 'libraries/quota/proof/QuotaProofs.lean'
CHECKER = 'libraries/quota/proof/Check.lean'
EVIDENCE = 'libraries/quota/proof-evidence.json'
MANIFEST = 'generated/quota/manifest.json'
INPUTS = (SOURCE, PROOFS, CHECKER, 'scripts/check_quota.py', 'scripts/export.py',
          'scripts/export_module.py', 'producer/lean-toolchain')
ROOTS = ['Quota.create', 'Quota.release', 'Quota.remaining', 'Quota.reserve']
CLAIMS = ['create_valid', 'reserve_accepted', 'reserve_preserves', 'reserve_rejected',
          'reserve_exact', 'release_accepted', 'release_preserves', 'release_rejected',
          'release_exact', 'remaining_exact']
AXIOMS = {'propext', 'Classical.choice', 'Quot.sound'}
GENERATED = {'api.go', 'control.go', 'runtime.go', 'schema.go'}
CONFIG = b'''name = "quota_proofs"
version = "0.0.0"

[[lean_lib]]
name = "Quota"

[[lean_lib]]
name = "QuotaProofs"
'''


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read_regular(path):
    if path.resolve() != path.absolute() or not path.is_file():
        raise ExportError(f'missing or symlinked input: {path.name}')
    if path.stat().st_size > 4 * 1024 * 1024:
        raise ExportError(f'input exceeds 4 MiB: {path.name}')
    data = path.read_bytes()
    if len(data) > 4 * 1024 * 1024:
        raise ExportError(f'input exceeds 4 MiB: {path.name}')
    return data


def snapshot(root):
    sources = {relative: read_regular(root / relative) for relative in INPUTS}
    if sources['producer/lean-toolchain'] != TOOLCHAIN:
        raise ExportError('quota toolchain configuration differs from the pin')
    return sources


def verify_manifest(path, source):
    """Bind source and root set, then verify the fixed generated file inventory."""
    raw = read_regular(path)
    manifest = parse_json(raw)
    if not isinstance(manifest, dict) or manifest.get('schema') != 'lean-go-manifest/v7':
        raise ExportError('quota requires a v7 generated manifest')
    program = manifest.get('program')
    expected = dict(schema='lean-go-ir/v7', profile='pure-module/v5', stage='pure-mono',
                    module='Quota', roots=ROOTS, lean_version='4.35.0', lean_commit=COMMIT)
    if (not isinstance(program, dict) or 'root' in program or
            any(program.get(k) != v for k, v in expected.items())):
        raise ExportError('quota generated program, roots, or Lean pin differ')
    inputs = program.get('inputs')
    if (not isinstance(inputs, dict) or
            inputs.get('source/Quota.lean') != digest(source) or
            {name for name in inputs if name.startswith('source/')} != {'source/Quota.lean'}):
        raise ExportError('quota source hash differs from generated manifest')
    files = manifest.get('generated_files')
    if not isinstance(files, dict) or set(files) != GENERATED:
        raise ExportError('quota generated file inventory differs')
    if {entry.name for entry in path.parent.iterdir()} != GENERATED | {path.name}:
        raise ExportError('quota generated directory inventory differs')
    for name in sorted(GENERATED):
        if files[name] != digest(read_regular(path.parent / name)):
            raise ExportError(f'quota generated file hash differs: {name}')
    return raw


def validate_claims(claims):
    if not isinstance(claims, list) or len(claims) != len(CLAIMS):
        raise ExportError('unexpected quota theorem inventory')
    for name, claim in zip(CLAIMS, claims, strict=True):
        if (not isinstance(claim, dict) or set(claim) != {'name', 'statement', 'axioms'} or
                claim['name'] != 'QuotaProofs.' + name or
                not isinstance(claim['statement'], str) or not claim['statement']):
            raise ExportError('malformed quota theorem evidence')
        axioms = claim['axioms']
        if (not isinstance(axioms, list) or
                any(not isinstance(axiom, str) or axiom not in AXIOMS for axiom in axioms) or
                axioms != sorted(set(axioms))):
            raise ExportError('unapproved or malformed quota axiom evidence')
    return claims


def prove(sources):
    """Compile only snapshotted sources with fixed, dependency-free Lake scaffolding."""
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('LEAN_', 'LAKE_'))}
    env['ELAN_TOOLCHAIN'] = TOOLCHAIN.decode().strip()
    lean, lake = shutil.which('lean'), shutil.which('lake')
    if not lean or not lake:
        raise ExportError('pinned Lean and Lake executables must be available on PATH')
    with tempfile.TemporaryDirectory(prefix='lean-go-quota-proofs-') as directory:
        workspace = Path(directory)
        for relative, name in [(SOURCE, 'Quota.lean'), (PROOFS, 'QuotaProofs.lean'),
                               (CHECKER, 'Check.lean')]:
            (workspace / name).write_bytes(sources[relative])
        (workspace / 'lean-toolchain').write_bytes(TOOLCHAIN)
        (workspace / 'lakefile.toml').write_bytes(CONFIG)
        check_version(run([lean, '--version'], workspace, env))
        check_version(run([lake, 'env', 'lean', '--version'], workspace, env))
        lock = parse_json(read_regular(workspace / 'lake-manifest.json'))
        if not isinstance(lock, dict) or lock.get('packages') != []:
            raise ExportError('quota proof workspace must have no Lake dependencies')
        run([lake, 'build', 'QuotaProofs'], workspace, env)
        run([lake, 'env', 'lean', 'Check.lean'], workspace, env)
        return validate_claims(parse_json(read_regular(workspace / 'claims.json')))


def evidence(root, manifest):
    check_python()
    sources = snapshot(root)
    manifest_raw = verify_manifest(manifest, sources[SOURCE])
    claims = prove(sources)
    if snapshot(root) != sources or verify_manifest(manifest, sources[SOURCE]) != manifest_raw:
        raise ExportError('quota proof inputs or generated package changed during checking')
    result = {
        'schema': 'lean-go-quota-proofs/v1',
        'lean_version': '4.35.0', 'lean_commit': COMMIT,
        'roots': ROOTS,
        'inputs': {relative: digest(data) for relative, data in sources.items()},
        'generated_manifest_sha256': digest(manifest_raw),
        'claims': claims,
        'assurance': ('Kernel-checked source statements; extraction and Go execution '
                      'remain tested and trusted.'),
    }
    return (json.dumps(result, sort_keys=True, indent=2, ensure_ascii=False) + '\n').encode()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--manifest', type=Path, help='generated package manifest to bind')
    parser.add_argument('--output', type=Path, help='write freshly checked evidence')
    args = parser.parse_args(argv)
    root = args.root.resolve()
    manifest = args.manifest.absolute() if args.manifest else root / MANIFEST
    output = args.output.absolute() if args.output else root / EVIDENCE
    protected = {root / relative for relative in INPUTS} | {manifest}
    protected |= {manifest.parent / name for name in GENERATED}
    if output.resolve() in {path.resolve() for path in protected}:
        raise ExportError('evidence output must not overwrite proof or generated inputs')
    if output.resolve().is_relative_to(manifest.parent.resolve()):
        raise ExportError('evidence output must stay outside the generated package')
    fresh = evidence(root, manifest)
    if args.output:
        publish_files([(output, fresh)])
        print('Checked ten quota theorems and wrote bound proof evidence.')
    else:
        tracked = read_regular(output)
        parse_json(tracked)
        if tracked != fresh:
            raise ExportError('quota proof evidence is stale; regenerate with --output')
        print('Checked ten quota theorems; tracked evidence and generated file hashes match.')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ExportError, OSError) as exc:
        print(f'quota proof check failed: {exc}', file=sys.stderr)
        sys.exit(1)
