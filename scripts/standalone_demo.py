"""Shared fresh-artifact and Go-only checks for the copyable proof examples."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import json
from pathlib import Path
import shutil
import sys
import tempfile

from check_release_candidate import base_environment
from export import ExportError
from export_module import check_python


@dataclass(frozen=True)
class Demo:
    name: str
    module: str
    roots: tuple[str, ...]
    claims: tuple[tuple[str, str], ...]


def artifacts(project, name):
    result = {path.relative_to(project).as_posix(): path.read_bytes()
              for path in (project / name).rglob('*') if path.is_file()}
    for artifact in [name + '.ir.json', 'proof-evidence.json']:
        result[artifact] = (project / artifact).read_bytes()
    return result


def compare_artifacts(project, candidate, name):
    expected, actual = artifacts(project, name), artifacts(candidate, name)
    changed = sorted(path for path in expected.keys() | actual.keys()
                     if expected.get(path) != actual.get(path))
    if changed:
        raise ExportError(name + ' demo artifacts differ: ' + ', '.join(changed))


def run_go_demo(project, go, environment, workspace, output, run):
    isolated = dict(environment, PATH=str(go.parent), CGO_ENABLED='0', GOTOOLCHAIN='local',
                    GOWORK='off', GOENV='off', GOFLAGS='', GOPROXY='off', GOSUMDB='off',
                    GOPRIVATE='', GONOPROXY='', GONOSUMDB='', LEAN_GO_DEMO_GO_ONLY='1',
                    GOPATH=str(workspace / 'gopath'), GOMODCACHE=str(workspace / 'modules'),
                    GOCACHE=str(workspace / 'go-cache'))
    run([str(go), 'test', '-count=1', '-buildvcs=false', '-mod=readonly', './...'], project, isolated)
    actual = run([str(go), 'run', '-buildvcs=false', '-mod=readonly', '.'], project, isolated)
    if actual != output:
        raise ExportError('standalone demo returned unexpected output: ' + repr(actual))


def copy_go_project(project, destination, name):
    files = ['go.mod', 'main.go', 'main_test.go']
    files.extend(name + '/' + file for file in ['api.go', 'control.go', 'runtime.go', 'schema.go'])
    for file in files:
        target = destination / file
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(project / file, target)


def verify_demo(cli, workspace, environment, source, spec, *, run, pinned_go, consumer):
    project = workspace / ('copied ' + spec.name + ' demo')
    shutil.copytree(source, project)
    go = pinned_go(workspace, environment)
    candidate = workspace / 'candidate'
    candidate.mkdir()
    command = [str(cli), 'generate', '--source-dir', str(project / 'source'),
               '--module', spec.module, '--out', str(candidate / spec.name),
               '--ir-output', str(candidate / (spec.name + '.ir.json'))]
    for root in spec.roots:
        command.extend(['--root', root])
    run(command, project, environment)
    checker = [str(cli), 'check-proofs', '--contract', str(project / 'proof-contract.json'),
               '--manifest', str(candidate / spec.name / 'manifest.json')]
    run([*checker, '--output', str(candidate / 'proof-evidence.json')], project, environment)
    compare_artifacts(project, candidate, spec.name)
    run([*checker, '--evidence', str(project / 'proof-evidence.json')], project, environment)
    evidence = json.loads((candidate / 'proof-evidence.json').read_bytes())
    claims = sorted((claim['name'], claim['expected']) for claim in evidence['claims'])
    if claims != sorted(spec.claims):
        raise ExportError(f'{spec.name} demo must check all {len(spec.claims)} source claims')
    deployed = workspace / ('Go-only ' + spec.name + ' demo')
    copy_go_project(project, deployed, spec.name)
    consumer(deployed, go, environment, workspace)
    print(f'standalone {spec.name} demo: identical artifacts, {len(claims)} fresh proofs, '
          'Go-only tests and executable passed')


def main(verify, name):
    parser = argparse.ArgumentParser(description='Verify the standalone ' + name + ' demo.')
    parser.add_argument('--cli', default='lean-go', help='installed lean-go executable')
    args = parser.parse_args()
    try:
        check_python()
        cli = shutil.which(args.cli)
        if cli is None:
            raise ExportError('install the lean-go CLI first; see docs/cli.md')
        with tempfile.TemporaryDirectory(prefix='lean-go-' + name + '-demo-') as directory:
            verify(Path(cli).resolve(), Path(directory).resolve(), base_environment())
    except (ExportError, OSError) as exc:
        print(f'{name} demo failed: {exc}', file=sys.stderr)
        sys.exit(1)
