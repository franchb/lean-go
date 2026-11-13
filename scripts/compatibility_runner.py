"""Retained, isolated compiler runs for the compatibility research harness."""
from __future__ import annotations

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import zipfile

from compatibility_identity import (HarnessError, PINNED_COMMIT, PINNED_VERSION, adapt_pins,
                                    compiler_bin, digest, matching_identity, parse_identity,
                                    source_snapshot, write_snapshot)
from compatibility_results import (PASS_STATUSES, canonical_ir, compare_cases, extraction_outcome,
                                   ir_diff, ir_shapes, validate_cases)
from differential_backend import run_process
from export import ExportError, parse_json
from export_module import check_python

HARNESS_FILES = ('scripts/check_compatibility.py', 'scripts/compatibility_identity.py',
                 'scripts/compatibility_results.py', 'scripts/compatibility_runner.py',
                 'scripts/differential_backend.py', 'pyproject.toml')
ENV_KEYS = ('PATH', 'CGO_ENABLED', 'GOTOOLCHAIN', 'GOWORK', 'GOPROXY', 'GOSUMDB',
            'GOENV', 'GOFLAGS', 'ELAN_TOOLCHAIN', 'PYTHONDONTWRITEBYTECODE')


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + '\n')


def environment(bin_dir, go):
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('LEAN_', 'LAKE_', 'ELAN_', 'PYTHON')) and key not in {'GOOS', 'GOARCH'}}
    env.update(PATH=os.pathsep.join((str(bin_dir), str(Path(go).parent), os.defpath)),
               CGO_ENABLED='0', GOTOOLCHAIN='local', GOWORK='off', GOPROXY='off',
               GOSUMDB='off', GOENV='off', GOFLAGS='', PYTHONDONTWRITEBYTECODE='1')
    return env


class Commands:
    def __init__(self, timeout):
        self.timeout = timeout

    def run(self, stage, command, cwd, env, logs):
        record = {'stage': stage, 'argv': [str(arg) for arg in command], 'cwd': str(cwd),
                  'timeout_seconds': self.timeout,
                  'environment': {key: env[key] for key in ENV_KEYS if key in env}}
        start = time.monotonic()
        try:
            result = run_process(record['argv'], cwd=cwd, env=env, data=None, timeout=self.timeout)
        except (OSError, subprocess.TimeoutExpired) as error:
            def decoded(value):
                return value.decode(errors='replace') if isinstance(value, bytes) else value or ''
            record['error'] = str(error)
            result = subprocess.CompletedProcess(record['argv'], -1,
                decoded(getattr(error, 'output', '')), decoded(getattr(error, 'stderr', '')))
        record.update(returncode=result.returncode, stdout=result.stdout, stderr=result.stderr,
                      elapsed_seconds=time.monotonic() - start)
        with (logs / 'commands.jsonl').open('a') as stream:
            stream.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + '\n')
        return result


def checked_output(result, label):
    if result.returncode:
        raise HarnessError(f'{label} failed; inspect commands.jsonl')
    return result.stdout


def observe_compiler(bin_dir, workspace, env, commands, logs):
    direct = checked_output(commands.run('lean-version', [bin_dir / 'lean', '--version'], workspace, env, logs), 'lean identity')
    identity = parse_identity(direct)
    env['ELAN_TOOLCHAIN'] = 'leanprover/lean4:v' + identity['version']
    through_lake = checked_output(commands.run('lake-lean-version', [bin_dir / 'lake', 'env', 'lean', '--version'], workspace, env, logs), 'lake Lean identity')
    matching_identity(direct, through_lake)
    prefix = checked_output(commands.run('lean-prefix', [bin_dir / 'lean', '--print-prefix'], workspace, env, logs), 'Lean prefix').strip()
    if Path(prefix).resolve() != bin_dir.parent:
        raise HarnessError('explicit compiler directory disagrees with lean --print-prefix')
    lake = checked_output(commands.run('lake-version', [bin_dir / 'lake', '--version'], workspace, env, logs), 'Lake identity')
    files = [bin_dir / name for name in ('lean', 'lake')]
    files += [bin_dir.parent / name for name in ('lib/lean/Init.olean', 'lib/lean/Lean.olean',
                                                'lib/lean/Init/Data/UInt/Package.olean')]
    return {**identity, 'lean_output': direct, 'lake_lean_output': through_lake, 'lake_output': lake,
            'bin': str(bin_dir), 'prefix': prefix,
            'sha256': {str(path.relative_to(bin_dir.parent)): digest(path.read_bytes()) for path in files}}


def run_case(case, corpus, workspace, role, identity, go, env, commands):
    directory = role / 'cases' / case['name']
    directory.mkdir(parents=True)
    fixture = corpus / case['name']
    source = directory / 'source'
    shutil.copytree(fixture / 'source', source)
    consumer = directory / 'consumer'
    consumer.mkdir()
    command = [sys.executable, workspace / 'scripts/generate.py', '--diagnostics=json',
               '--source-dir', source, '--module', case['module'], '--out', consumer / 'extracted',
               '--ir-output', directory / 'program.ir.json']
    for root in case['roots']:
        command.extend(('--root', root))
    result = commands.run('extract', command, workspace, env, directory)
    outcome = extraction_outcome(result, case.get('reject'))
    if outcome['status'] == 'extracted':
        outcome = check_accepted(case, fixture, directory, consumer, identity, go, env, commands)
    write_json(directory / 'result.json', outcome)
    return outcome


def check_accepted(case, fixture, directory, consumer, identity, go, env, commands):
    try:
        program = parse_json((directory / 'program.ir.json').read_bytes())
        if program['lean_version'] != identity['version'] or program['lean_commit'] != identity['commit']:
            raise HarnessError('successful IR has a different compiler identity')
        canonical = canonical_ir(program)
        missing = ir_shapes(program, case.get('shapes', {}))
    except (OSError, ExportError, HarnessError, KeyError, TypeError, RecursionError) as error:
        return {'status': 'extraction_protocol_failed', 'detail': str(error)}
    (directory / 'program.canonical.json').write_text(canonical)
    outcome = {'status': 'fixture_shape_changed' if missing else 'accepted',
               'canonical_ir_sha256': digest(canonical.encode()), 'missing_shapes': missing}
    # Even a shape change retains independent behavior evidence when emission succeeded.
    shutil.copyfile(fixture / 'consumer_test.go.txt', consumer / 'consumer_test.go')
    (consumer / 'go.mod').write_text('module compatconsumer\n\ngo 1.27.4\n')
    binary = consumer / 'consumer.test'
    built = commands.run('go-consumer-build', [go, 'test', '-c', '-o', binary, '.'], consumer, env, directory)
    if built.returncode:
        outcome['status'] = 'consumer_build_failed'
        return outcome
    runtime_env = dict(env, PATH=str(consumer / 'no-compiler-tools'))
    executed = commands.run('go-consumer-run', [binary, '-test.v'], consumer, runtime_env, directory)
    outcome['behavior'] = 'passed' if executed.returncode == 0 else 'failed'
    if executed.returncode:
        outcome['status'] = 'behavior_mismatch'
    return outcome


def run_compiler(role, bin_dir, snapshot, corpus, cases, go, commands, *, baseline):
    role.mkdir()
    workspace = role / 'producer'
    write_snapshot(workspace, snapshot)
    env = environment(bin_dir, go)
    identity = observe_compiler(bin_dir, workspace / 'producer', env, commands, role)
    if baseline and (identity['version'], identity['commit']) != (PINNED_VERSION, PINNED_COMMIT):
        raise HarnessError('baseline must be exactly the production Lean version and commit')
    adapted, adaptations, patch = adapt_pins(snapshot, identity)
    for name in adapted:
        if adapted[name] != snapshot[name]:
            (workspace / name).write_bytes(adapted[name])
    write_json(role / 'identity.json', identity)
    write_json(role / 'adaptations.json', adaptations)
    (role / 'adaptation.patch').write_text(patch)
    result = {'identity': identity, 'adaptations': adaptations,
              'cases': {case['name']: {'status': 'not_run'} for case in cases}}
    # A focused upstream-name observation is retained even if the producer cannot build.
    probe = corpus.parent / 'reproducers/NumericBoundaryName.lean'
    shutil.copyfile(probe, role / probe.name)
    observed = commands.run('numeric-boundary-probe', [bin_dir / 'lean', role / probe.name],
                            workspace / 'producer', env, role)
    result['numeric_boundary_probe'] = {'returncode': observed.returncode, 'stdout': observed.stdout,
                                       'stderr': observed.stderr}
    build = commands.run('producer-build', [bin_dir / 'lake', 'build', 'LeanGo.Module.Export'],
                         workspace / 'producer', env, role)
    if build.returncode:
        result['status'] = 'producer_build_failed'
    else:
        result['status'] = 'passed'
        for case in cases:
            outcome = run_case(case, corpus, workspace, role, identity, go, env, commands)
            result['cases'][case['name']] = outcome
            if outcome['status'] not in PASS_STATUSES:
                result['status'] = 'fixture_failed'
    write_json(role / 'result.json', result)
    return result


def replay_script(baseline_bin, candidate_bin, case=None, *, timeout):
    arguments = ['--baseline-bin', str(baseline_bin), '--timeout', str(timeout)]
    if candidate_bin:
        arguments += ['--candidate-bin', str(candidate_bin)]
    if case:
        arguments += ['--case', case]
    # Python argv encoding avoids shell quoting and command substitution entirely.
    return ('"""Replay with the same existing tool installations and a NEW output directory."""\n'
            'from pathlib import Path\nimport subprocess\nimport sys\n'
            'if len(sys.argv) != 2:\n    raise SystemExit("usage: python replay.py NEW_ARTIFACT_DIRECTORY")\n'
            'root = Path(__file__).resolve().parent\n'
            'raise SystemExit(subprocess.call([sys.executable, str(root / "source/scripts/check_compatibility.py"),\n'
            f'    *{arguments!r}, "--artifacts", sys.argv[1]]))\n')


def snapshot_inputs(repo, snapshot):
    result = dict(snapshot)
    for name in HARNESS_FILES:
        result[name] = (repo / name).read_bytes()
    for path in sorted((repo / 'compatibility').rglob('*')):
        if path.is_symlink():
            raise HarnessError('compatibility inputs must not contain symlinks')
        if path.is_file():
            result[path.relative_to(repo).as_posix()] = path.read_bytes()
    return result


def retained_evidence(artifacts, case):
    """Enumerate replay evidence without caches and executable build products."""
    for name in ('report.json', 'inputs.json'):
        yield artifacts / name
    for path in sorted((artifacts / 'source').rglob('*')):
        if path.is_file():
            yield path
    for role in ('baseline', 'candidate'):
        for name in ('identity.json', 'adaptations.json', 'adaptation.patch', 'commands.jsonl', 'result.json'):
            path = artifacts / role / name
            if path.is_file():
                yield path
        if case is None:
            continue
        for path in sorted((artifacts / role / 'cases' / case).rglob('*')):
            if path.is_file() and path.name != 'consumer.test':
                yield path


def difference_bundles(artifacts, report, baseline_bin, candidate_bin, *, timeout):
    """Small source-only replays plus relevant logs/IR; never ship build caches."""
    directory = artifacts / 'differences'
    directory.mkdir()
    for difference in report['differences']:
        case = difference.get('case')
        bundle = directory / (case or 'producer')
        bundle.mkdir()
        write_json(bundle / 'difference.json', difference)
        if difference['kind'] == 'canonical_ir_drift':
            before = (artifacts / 'baseline/cases' / case / 'program.canonical.json').read_text()
            after = (artifacts / 'candidate/cases' / case / 'program.canonical.json').read_text()
            (bundle / 'ir.diff').write_text(ir_diff(before, after))
        with zipfile.ZipFile(bundle / 'replay.zip', 'w', compression=zipfile.ZIP_DEFLATED) as archive:
            archive.writestr('replay.py', replay_script(baseline_bin, candidate_bin, case, timeout=timeout))
            for path in retained_evidence(artifacts, case):
                archive.write(path, path.relative_to(artifacts))
            archive.write(bundle / 'difference.json', 'difference.json')
            if (bundle / 'ir.diff').exists():
                archive.write(bundle / 'ir.diff', 'ir.diff')


def validate_output(artifacts, repo, bins):
    requested = artifacts.absolute()
    if requested != requested.resolve() or requested.exists():
        raise HarnessError('artifact directory must be a new path without symlinks')
    for protected in (repo, *[path.parent for path in bins]):
        if requested.is_relative_to(protected) or protected.is_relative_to(requested):
            raise HarnessError('artifact directory must not overlap source or compiler installations')
    return requested


def run(args, repo):
    check_python()
    baseline_bin = compiler_bin(args.baseline_bin)
    candidate_bin = compiler_bin(args.candidate_bin) if args.candidate_bin else None
    artifacts = validate_output(args.artifacts, repo, [p for p in (baseline_bin, candidate_bin) if p])
    go = shutil.which('go')
    if go is None:
        raise HarnessError('Go 1.27.4 must be available on PATH')
    go = str(Path(go).resolve())
    snapshot = source_snapshot(repo)
    inputs = snapshot_inputs(repo, snapshot)
    cases = validate_cases(parse_json(inputs['compatibility/corpus/manifest.json']))
    if args.case:
        cases = [case for case in cases if case['name'] == args.case]
        if not cases:
            raise HarnessError('unknown corpus case')
    artifacts.mkdir(parents=True)
    write_snapshot(artifacts / 'source', inputs)
    write_json(artifacts / 'inputs.json', {name: digest(data) for name, data in inputs.items()})
    (artifacts / 'replay.py').write_text(replay_script(baseline_bin, candidate_bin, args.case, timeout=args.timeout))
    commands = Commands(args.timeout)
    report = {'schema': 'lean-go-compatibility/v1', 'status': 'harness_error', 'differences': [],
              'canonical_ir_exclusions': ['inputs', 'lean_commit', 'lean_version'],
              'scope': 'research_identity_adaptation_only', 'selected_cases': [c['name'] for c in cases],
              'host_tools': {'python': sys.version, 'python_executable': sys.executable,
                             'python_sha256': digest(Path(sys.executable).read_bytes()),
                             'go_executable': go, 'go_sha256': digest(Path(go).read_bytes())}}
    exit_code = 2
    try:
        version = checked_output(commands.run('go-version', [go, 'version'], artifacts,
                                 environment(baseline_bin, go), artifacts), 'Go identity')
        if not version.startswith('go version go1.27.4 '):
            raise HarnessError('Go 1.27.4 is required')
        report['host_tools']['go'] = version
        corpus = artifacts / 'source/compatibility/corpus'
        report['baseline'] = run_compiler(artifacts / 'baseline', baseline_bin, snapshot, corpus,
                                         cases, go, commands, baseline=True)
        if report['baseline']['status'] != 'passed':
            report['status'] = 'baseline_failed'
        elif candidate_bin is None:
            report['status'], exit_code = 'baseline_passed', 0
        else:
            report['candidate'] = run_compiler(artifacts / 'candidate', candidate_bin, snapshot, corpus,
                                              cases, go, commands, baseline=False)
            if report['candidate']['status'] == 'producer_build_failed':
                report['differences'] = [{'kind': 'producer_build_failed'}]
            else:
                report['differences'] = compare_cases(report['baseline']['cases'], report['candidate']['cases'])
            report['status'] = 'different' if report['differences'] else 'compatible'
            exit_code = 1 if report['differences'] else 0
    except (HarnessError, ExportError, OSError, ValueError) as error:
        report['error'] = str(error)
    report['exit_code'] = exit_code
    write_json(artifacts / 'report.json', report)
    if report['differences']:
        difference_bundles(artifacts, report, baseline_bin, candidate_bin, timeout=args.timeout)
    return exit_code, artifacts / 'report.json'
