#!/usr/bin/env python3
"""Build, audit, install and exercise the CLI outside the source checkout."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import tomllib
import zipfile

from check_clamp_demo import verify_demo
from check_release_candidate import base_environment, copy_fixture, pinned_go, run_consumer
from export_module import check_python

ROOT = Path(__file__).resolve().parents[1]


def run(command, cwd, env, *, succeeds=True):
    completed = subprocess.run([str(value) for value in command], cwd=cwd, env=env,
                               capture_output=True, text=True, timeout=900, check=False)
    if (completed.returncode == 0) != succeeds:
        raise AssertionError(f'command {command[0]} returned {completed.returncode}:\n'
                             f'{completed.stdout}\n{completed.stderr}')
    return completed.stdout + completed.stderr


def package_sources():
    config = tomllib.loads((ROOT / 'pyproject.toml').read_text())
    wheel = config['tool']['hatch']['build']['targets']['wheel']
    sources = {destination: (ROOT / source).read_bytes()
               for source, destination in wheel['force-include'].items()}
    sources.update({path.removeprefix('src/'): (ROOT / path).read_bytes()
                    for path in wheel['only-include']})
    return config, sources


def audit_wheel(path, sources):
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        assert len(names) == len(set(names)), 'duplicate wheel members'
        payload = {name: archive.read(name) for name in names if '.dist-info/' not in name}
        assert payload == sources, 'wheel source inventory or bytes differ'
        for name in names:
            assert not any(part in {'.git', '.lake', '__pycache__', 'tests'} for part in Path(name).parts)
            assert not name.endswith(('_test.go', '.pyc', '.olean'))
            assert not Path(name).name.startswith('test_')


def audit_sdist(path, config):
    expected = {name.removeprefix('/') for name in
                config['tool']['hatch']['build']['targets']['sdist']['include']}
    with tarfile.open(path) as archive:
        files = {member.name.split('/', 1)[1]: archive.extractfile(member).read()
                 for member in archive.getmembers() if member.isfile()}
    assert set(files) == expected | {'PKG-INFO'}, 'sdist contains unexpected files'
    for name in expected:
        assert files[name] == (ROOT / name).read_bytes(), f'sdist differs: {name}'


def build(workspace, env):
    uv = shutil.which('uv')
    if uv is None:
        raise AssertionError('uv is required to build and install the package')
    config, sources = package_sources()
    direct, source, rebuilt = [workspace / name for name in ['wheel', 'sdist', 'rebuilt']]
    run([uv, 'build', '--python', sys.executable, '--wheel', '--out-dir', direct, ROOT], workspace, env)
    run([uv, 'build', '--python', sys.executable, '--sdist', '--out-dir', source, ROOT], workspace, env)
    sdist = next(source.glob('*.tar.gz'))
    audit_sdist(sdist, config)
    run([uv, 'build', '--python', sys.executable, '--wheel', '--out-dir', rebuilt, sdist], workspace, env)
    wheels = [next(directory.glob('*.whl')) for directory in [direct, rebuilt]]
    for wheel in wheels:
        audit_wheel(wheel, sources)
    return uv, wheels, sources


def install(uv, wheel, directory, env):
    venv = directory / 'venv'
    run([uv, 'venv', '--python', sys.executable, venv], directory, env)
    binaries = venv / ('Scripts' if os.name == 'nt' else 'bin')
    python = binaries / ('python.exe' if os.name == 'nt' else 'python')
    run([uv, 'pip', 'install', '--python', python, '--no-deps', wheel], directory, env)
    inspection = run([python, '-I', '-c',
                      'import json, lean_go; from importlib import metadata; '
                      'print(json.dumps([lean_go.__file__, str(metadata.distribution("lean-go-tools").'
                      'locate_file("lean_go/_payload"))]))'], directory, env)
    module, payload = map(Path, json.loads(inspection))
    assert module.resolve().is_relative_to(venv.resolve()), 'import escaped the installed venv'
    assert payload.resolve().is_relative_to(venv.resolve()), 'payload escaped the installed venv'
    return binaries / ('lean-go.exe' if os.name == 'nt' else 'lean-go'), payload


def snapshot(directory):
    return {path.relative_to(directory).as_posix(): path.read_bytes()
            for path in directory.rglob('*') if path.is_file()}


def read_only(directory):
    paths = [directory, *directory.rglob('*')]
    saved = [(path, stat.S_IMODE(path.stat().st_mode)) for path in paths]
    for path, mode in reversed(saved):
        path.chmod(mode & ~0o222)
    return saved


def multi_root_workflow(cli, directory, contract, go, env):
    consumer = directory / 'multi consumer'
    consumer.mkdir()
    package = consumer / 'clamp'
    run([cli, 'generate', '--source-dir', contract.source_dir, '--module', contract.module,
         '--root', 'Clamp.clamp', '--root', 'Clamp.Bounds.cap',
         '--out', package, '--ir-output', directory / 'multi.json'], directory, env)
    manifest = json.loads((package / 'manifest.json').read_bytes())
    assert manifest['program']['roots'] == ['Clamp.Bounds.cap', 'Clamp.clamp']
    assert 'func RunTyped(' not in (package / 'api.go').read_text()
    (consumer / 'go.mod').write_text('module example.com/cli-multi\n\ngo 1.27.4\n')
    (consumer / 'main.go').write_text('''package main
import clamp "example.com/cli-multi/clamp"
func main() {
 limits := clamp.DefaultLimits()
 got, err := clamp.RunClamp(100, 7, limits)
 if err != nil || got != 7 { panic("clamp mismatch") }
 got, err = clamp.RunCap(^uint64(0), 9, limits)
 if err != nil || got != 9 { panic("cap mismatch") }
}
''')
    isolated = dict(env, PATH=str(go.parent), GOPROXY='off', GOSUMDB='off',
                    GOPATH=str(directory / 'gopath'), GOMODCACHE=str(directory / 'modules'),
                    GOCACHE=str(directory / 'go-cache'))
    run([go, 'run', '-buildvcs=false', '-mod=readonly', '.'], consumer, isolated)


def producer_workflow(cli, directory, env, *, multiple_roots):
    contract = copy_fixture(ROOT, directory / 'project with spaces')
    caller = directory / 'caller'
    caller.mkdir()
    package = directory / 'generated with spaces'
    ir = directory / 'program with spaces.json'
    proof = directory / 'proof with spaces.json'
    relative = lambda path: os.path.relpath(path, caller)
    generation = [cli, 'generate', '--source-dir', relative(contract.source_dir),
                  '--module', contract.module, '--root', contract.roots[0],
                  '--out', relative(package), '--ir-output', relative(ir)]
    run([cli, 'doctor'], caller, env)
    run(generation, caller, env)
    before = snapshot(package)
    checker = [cli, 'check-proofs', '--contract', relative(contract.path),
               '--manifest', relative(package / 'manifest.json')]
    run([*checker, '--output', relative(proof)], caller, env)
    run([*checker, '--evidence', relative(proof)], caller, env)
    evidence = json.loads(proof.read_bytes())
    assert len(evidence['claims']) == 3
    go = pinned_go(ROOT, env)
    count, _ = run_consumer(directory, package, go, env)
    if multiple_roots:
        multi_root_workflow(cli, directory, contract, go, env)
    # The selected unsupported source must not replace any successful output.
    previous_ir, previous_proof = ir.read_bytes(), proof.read_bytes()
    (contract.source_dir / 'Unsupported.lean').write_text('def bad (x : Nat) : Nat := x\n')
    rejected = [cli, 'generate', '--source-dir', relative(contract.source_dir),
                '--module', 'Unsupported', '--root', 'bad',
                '--out', relative(package), '--ir-output', relative(ir)]
    failure = run(rejected, caller, env, succeeds=False)
    assert 'generation failed' in failure
    assert snapshot(package) == before and ir.read_bytes() == previous_ir
    run([*checker, '--evidence', relative(proof)], caller, env, succeeds=False)
    assert proof.read_bytes() == previous_proof
    return count


def tool_install(uv, workspace, env):
    isolated = dict(env, UV_TOOL_DIR=str(workspace / 'uv tools'),
                    UV_TOOL_BIN_DIR=str(workspace / 'uv bin'))
    run([uv, 'tool', 'install', '--python', sys.executable, ROOT], workspace, isolated)
    executable = Path(isolated['UV_TOOL_BIN_DIR']) / ('lean-go.exe' if os.name == 'nt' else 'lean-go')
    run([executable, '--version'], workspace, dict(isolated, PATH=''))
    print('uv tool install from local path: passed')


def smoke(workspace, without_lean):
    env = base_environment()
    for name in ['PYTHONPATH', 'PYTHONHOME', 'VIRTUAL_ENV']:
        env.pop(name, None)
    env['PYTHONDONTWRITEBYTECODE'] = '1'
    uv, wheels, sources = build(workspace, env)
    tool_install(uv, workspace, env)
    expected = {name.removeprefix('lean_go/_payload/'): body for name, body in sources.items()
                if name.startswith('lean_go/_payload/')}
    for index, wheel in enumerate(wheels):
        directory = workspace / f'installed {index}'
        directory.mkdir()
        cli, payload = install(uv, wheel, directory, env)
        assert snapshot(payload) == expected
        permissions = read_only(payload)
        try:
            for arguments in [['--help'], ['--version'], ['generate', '--help'], ['check-proofs', '--help']]:
                run([cli, *arguments], directory, dict(env, PATH=''))
            missing = run([cli, 'doctor'], directory, dict(env, PATH=''), succeeds=False)
            assert 'missing from PATH' in missing
            count = None if without_lean else producer_workflow(cli, directory, env, multiple_roots=index == 0)
            if not without_lean and index == 0:
                demo = directory / 'standalone demo'
                demo.mkdir()
                verify_demo(cli, demo, env)
            assert snapshot(payload) == expected, 'installed payload was changed'
        finally:
            for path, mode in permissions:
                path.chmod(mode)
        print(f'{"sdist rebuilt" if index else "direct"} wheel: installed payload verified; '
              f'{"producer checks explicitly skipped" if count is None else str(count) + " Go-only consumer cases passed"}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--without-lean', action='store_true',
                        help='audit packaging, installed help and missing-tool errors only')
    args = parser.parse_args()
    check_python()
    with tempfile.TemporaryDirectory(prefix='lean-go-install-smoke-') as directory:
        smoke(Path(directory).resolve(), args.without_lean)


if __name__ == '__main__':
    main()
