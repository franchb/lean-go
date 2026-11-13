#!/usr/bin/env python3
"""Regenerate a copied clamp demo with an installed CLI and run it with Go alone."""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import shutil
import sys
import tempfile

from check_release_candidate import base_environment, pinned_go
from export import ExportError, run
from export_module import check_python

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/clamp')
OUTPUT = ('clamp(7, 42) = 7\nclamp(100, 42) = 42\n'
          'clamp(18446744073709551615, 42) = 42\n')


def artifacts(project):
    package = project / 'clamp'
    result = {path.relative_to(project).as_posix(): path.read_bytes()
              for path in package.rglob('*') if path.is_file()}
    for name in ['clamp.ir.json', 'proof-evidence.json']:
        result[name] = (project / name).read_bytes()
    return result


def compare_artifacts(project, candidate):
    expected, actual = artifacts(project), artifacts(candidate)
    changed = sorted(name for name in expected.keys() | actual.keys()
                     if expected.get(name) != actual.get(name))
    if changed:
        raise ExportError('clamp demo artifacts differ: ' + ', '.join(changed))


def run_go_demo(project, go, environment, workspace):
    isolated = dict(environment, PATH=str(go.parent), CGO_ENABLED='0', GOTOOLCHAIN='local',
                    GOWORK='off', GOENV='off', GOFLAGS='', GOPROXY='off', GOSUMDB='off',
                    GOPRIVATE='', GONOPROXY='', GONOSUMDB='', LEAN_GO_DEMO_GO_ONLY='1',
                    GOPATH=str(workspace / 'gopath'), GOMODCACHE=str(workspace / 'modules'),
                    GOCACHE=str(workspace / 'go-cache'))
    run([str(go), 'test', '-count=1', '-buildvcs=false', '-mod=readonly', './...'], project, isolated)
    output = run([str(go), 'run', '-buildvcs=false', '-mod=readonly', '.'], project, isolated)
    if output != OUTPUT:
        raise ExportError('clamp demo returned unexpected output: ' + repr(output))


def verify_demo(cli, workspace, environment):
    project = workspace / 'copied clamp demo'
    shutil.copytree(ROOT / DEMO, project)
    go = pinned_go(workspace, environment)
    candidate = workspace / 'candidate'
    candidate.mkdir()
    command = [str(cli), 'generate', '--source-dir', str(project / 'source'),
               '--module', 'Clamp', '--root', 'Clamp.clamp', '--out', str(candidate / 'clamp'),
               '--ir-output', str(candidate / 'clamp.ir.json')]
    run(command, project, environment)
    checker = [str(cli), 'check-proofs', '--contract', str(project / 'proof-contract.json'),
               '--manifest', str(candidate / 'clamp/manifest.json')]
    run([*checker, '--output', str(candidate / 'proof-evidence.json')], project, environment)
    compare_artifacts(project, candidate)
    run([*checker, '--evidence', str(project / 'proof-evidence.json')], project, environment)
    evidence = json.loads((candidate / 'proof-evidence.json').read_bytes())
    if len(evidence['claims']) != 3:
        raise ExportError('clamp demo must check all three source claims')
    run_go_demo(project, go, environment, workspace)
    print('standalone clamp demo: identical artifacts, 3 fresh proofs, Go-only tests and executable passed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cli', default='lean-go', help='installed lean-go executable')
    args = parser.parse_args()
    check_python()
    cli = shutil.which(args.cli)
    if cli is None:
        raise ExportError('install the lean-go CLI first; see docs/cli.md')
    with tempfile.TemporaryDirectory(prefix='lean-go-clamp-demo-') as directory:
        verify_demo(Path(cli).resolve(), Path(directory).resolve(), base_environment())


if __name__ == '__main__':
    try:
        main()
    except (ExportError, OSError) as exc:
        print(f'clamp demo failed: {exc}', file=sys.stderr)
        sys.exit(1)
