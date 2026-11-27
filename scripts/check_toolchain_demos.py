#!/usr/bin/env python3
"""Regenerate the proof demos with an explicit supported Lean installation."""
import argparse
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile

import check_bytes_demo
import check_checked_frame
import check_clamp_demo
import check_clock_demo
import check_adler32_demo
import check_crc16_demo
import check_flags_demo
import check_header_demo
import check_words_demo
import check_wire16_demo
from check_release_candidate import base_environment, pinned_go
from compatibility_identity import compiler_bin, parse_identity
from export import ExportError, run
from export_module import check_python
from standalone_demo import copy_consumer_files, run_go_demo

DEMOS = (check_clamp_demo, check_flags_demo, check_words_demo, check_bytes_demo, check_header_demo,
         check_wire16_demo, check_crc16_demo, check_adler32_demo, check_clock_demo)


def check_demo(demo, cli, go, workspace, env, identity):
    spec = demo.SPEC
    source = demo.ROOT / demo.DEMO
    deployed = workspace / spec.name
    deployed.mkdir()
    package = deployed / spec.name
    ir = workspace / f'{spec.name}.ir.json'
    command = [str(cli), 'generate', '--source-dir', str(source / 'source'),
               '--module', spec.module, '--out', str(package), '--ir-output', str(ir)]
    for root in spec.roots:
        command.extend(['--root', root])
    run(command, workspace, env)
    checker = [str(cli), 'check-proofs', '--contract', str(source / 'proof-contract.json'),
               '--manifest', str(package / 'manifest.json')]
    evidence_path = workspace / f'{spec.name}-evidence.json'
    run([*checker, '--output', str(evidence_path)], workspace, env)
    run([*checker, '--evidence', str(evidence_path)], workspace, env)

    evidence = json.loads(evidence_path.read_bytes())
    reference = json.loads((source / 'proof-evidence.json').read_bytes())
    program = json.loads(ir.read_bytes())
    manifest = json.loads((package / 'manifest.json').read_bytes())
    for record in (program, manifest['program'], evidence):
        if (record['lean_version'], record['lean_commit']) != (identity['version'], identity['commit']):
            raise ExportError(f'{spec.name}: artifacts identify a different compiler')
    inventory = sorted((claim['name'], claim['expected']) for claim in evidence['claims'])
    if evidence['claims'] != reference['claims'] or inventory != sorted(spec.claims):
        raise ExportError(f'{spec.name}: source theorems, propositions or axioms changed')

    copy_consumer_files(source, deployed, spec.extra_go_files)
    if demo is check_wire16_demo:
        native = workspace / 'wire16-native'
        native.mkdir()
        for filename in ('Reference.lean', 'lakefile.toml'):
            shutil.copyfile(source / 'native' / filename, native / filename)
        shutil.copyfile(source / 'source' / 'Wire16.lean', native / 'Wire16.lean')
        run(['lake', 'build', 'reference'], native, env)
        oracle = native / 'oracle.bin'
        run([str(native / '.lake/build/bin/reference'), str(oracle)], native, env)
        env = dict(env, LEAN_GO_WIRE16_NATIVE=str(oracle))
    run_go_demo(deployed, go, env, workspace, demo.OUTPUT, run)
    if demo is check_wire16_demo:
        print('Compared native Lean and generated Go encoding, decoding and widening for all 65,536 words', flush=True)
    print(f"{spec.name}: {len(spec.claims)} fresh source proofs and Go-only consumer passed", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lean-bin', type=Path, required=True)
    parser.add_argument('--cli', default='lean-go')
    args = parser.parse_args()
    check_python()
    binary = compiler_bin(args.lean_bin)
    cli = shutil.which(args.cli)
    if not cli:
        raise ExportError('install the checkout CLI before checking compiler support')
    cli = str(Path(cli).resolve())
    env = base_environment()
    env['PATH'] = str(binary) + os.pathsep + env['PATH']
    with tempfile.TemporaryDirectory(prefix='lean-go-compiler-demos-') as directory:
        workspace = Path(directory).resolve()
        identity = parse_identity(run([str(binary / 'lean'), '--version'], workspace, env))
        env['LEAN_GO_TOOLCHAIN'] = identity['version']
        run([cli, 'doctor'], workspace, env)
        go = pinned_go(workspace, env)
        for demo in DEMOS:
            check_demo(demo, cli, go, workspace, env, identity)
        check_checked_frame.verify(cli, workspace, workspace, env)
        composed = workspace / 'checkedframe'
        copy_consumer_files(check_checked_frame.FRAME, composed, ('checkedframe/frame.go',))
        run_go_demo(composed, go, env, workspace, check_checked_frame.OUTPUT, run)
    print(f"Lean {identity['version']} ({identity['commit']}): "
          f"{sum(len(d.SPEC.claims) for d in DEMOS) + len(check_checked_frame.CLAIMS)} distinct source claims passed")


if __name__ == '__main__':
    try:
        main()
    except (ExportError, OSError, ValueError) as exc:
        sys.exit(f'Compiler demo verification failed: {exc}')
