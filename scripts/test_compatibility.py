"""Fast compatibility protocol, isolation, adaptation and mismatch regressions."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import compatibility_identity as identity
import compatibility_results as results
import compatibility_runner as runner
from export import ExportError
from diagnostics import diagnostic

ROOT = Path(__file__).resolve().parents[1]
BASE = {'version': identity.PINNED_VERSION, 'commit': identity.PINNED_COMMIT}
NEXT = {'version': '4.99.0-rc1', 'commit': '1234567890abcdef1234567890abcdef12345678'}


def version(value=BASE):
    return f"Lean (version {value['version']}, x86_64-unknown-linux-gnu, commit {value['commit']}, Release)\n"


def failure(*, code='unsupported_type', stage='admission', returncode=1, stdout=''):
    report = {'schema': 'lean-go-diagnostics/v1', 'success': False,
              'diagnostics': [diagnostic('fixture', code=code, stage=stage)]}
    return subprocess.CompletedProcess([], returncode, stdout, json.dumps(report))


class IdentityTests(unittest.TestCase):
    def test_release_identities_and_lake_match(self):
        for value in (BASE, NEXT):
            self.assertEqual(identity.parse_identity(version(value)), value)
            self.assertEqual(identity.matching_identity(version(value), version(value)), value)
        with self.assertRaisesRegex(identity.HarnessError, 'disagree'):
            identity.matching_identity(version(), version(NEXT))

    def test_incomplete_or_ambiguous_identities_fail(self):
        for text in ('', 'Lean version 4.35.0', version().replace(identity.PINNED_COMMIT, 'eb6cafb'),
                     version() + version(), version().replace('Release', 'Debug'),
                     version().replace(identity.PINNED_VERSION, identity.PINNED_VERSION + ';echo')):
            with self.subTest(text=text), self.assertRaises(identity.HarnessError):
                identity.parse_identity(text)

    def test_no_shim_or_nonexecutable_selection(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            bin_dir = root / 'bin'
            bin_dir.mkdir()
            for name in ('lean', 'lake'):
                (bin_dir / name).write_text('not a toolchain')
            with self.assertRaises(identity.HarnessError):
                identity.compiler_bin(bin_dir)
            for path in bin_dir.iterdir():
                path.chmod(0o755)
            with self.assertRaisesRegex(identity.HarnessError, 'installed Lean library'):
                identity.compiler_bin(bin_dir)

    def test_bounded_adaptation_and_same_pin_is_empty(self):
        snapshot = identity.source_snapshot(ROOT)
        unchanged = dict(snapshot)
        adapted, evidence, patch_text = identity.adapt_pins(snapshot, BASE)
        self.assertEqual(adapted, snapshot)
        self.assertEqual(evidence['files'], [])
        self.assertEqual(patch_text, '')
        adapted, evidence, patch_text = identity.adapt_pins(snapshot, NEXT)
        self.assertEqual(snapshot, unchanged, 'checkout bytes must never be mutated')
        self.assertEqual({item['path'] for item in evidence['files']}, set(identity.pin_substitutions(NEXT)))
        self.assertEqual(evidence['semantic_adaptations'], [])
        self.assertEqual(evidence['patch_sha256'], identity.digest(patch_text.encode()))
        for name, data in snapshot.items():
            if name.endswith('.lean'):
                self.assertEqual(adapted[name], data, 'no Lean semantic adaptation is authorized')
        for item in evidence['files']:
            self.assertEqual(item['before_sha256'], identity.digest(snapshot[item['path']]))
            self.assertEqual(item['after_sha256'], identity.digest(adapted[item['path']]))
        self.assertIn(f"lean_version='{NEXT['version']}'".encode(), adapted['scripts/export_module.py'])
        self.assertIn(NEXT['commit'].encode(), adapted['internal/moduleir/types.go'])

    def test_missing_duplicate_or_changed_anchor_fails_before_write(self):
        original = identity.source_snapshot(ROOT)
        for value in (b'', original['producer/lean-toolchain'] * 2,
                      b'leanprover/lean4:v4.0.0\n'):
            with self.subTest(value=value), self.assertRaises(identity.HarnessError):
                identity.adapt_pins(dict(original, **{'producer/lean-toolchain': value}), NEXT)

    def test_production_pin_stays_strict(self):
        from export import check_version, ExportError
        check_version(version())
        with self.assertRaises(ExportError):
            check_version(version(NEXT))
        self.assertEqual((ROOT / 'producer/lean-toolchain').read_bytes(), identity.TOOLCHAIN)


class ProtocolTests(unittest.TestCase):
    def test_only_expected_admission_code_matches(self):
        self.assertEqual(results.extraction_outcome(failure(), 'unsupported_type')['status'], 'expected_rejection')
        self.assertEqual(results.extraction_outcome(failure(), 'source_audit')['status'], 'admission_mismatch')
        self.assertEqual(results.extraction_outcome(failure())['status'], 'admission_mismatch')
        for stage in ('toolchain', 'build', 'emission'):
            self.assertEqual(results.extraction_outcome(failure(stage=stage), 'unsupported_type')['status'], 'extraction_failed')

    def test_crashes_noise_and_malformed_json_are_never_rejections(self):
        cases = [failure(returncode=-9), failure(returncode=2), failure(stdout='noise'),
                 failure(code='invented'), subprocess.CompletedProcess([], 1, '', 'Traceback'),
                 subprocess.CompletedProcess([], 1, '', '{}')]
        malformed = json.loads(failure().stderr)
        malformed['success'] = 0
        cases.append(subprocess.CompletedProcess([], 1, '', json.dumps(malformed)))
        cases.append(subprocess.CompletedProcess([], 1, '', failure().stderr.replace('"success": false', '"success": true, "success": false')))
        for result in cases:
            with self.subTest(result=result):
                self.assertEqual(results.extraction_outcome(result, 'unsupported_type')['status'], 'extraction_protocol_failed')
        accepted = subprocess.CompletedProcess([], 0, '', '')
        self.assertEqual(results.extraction_outcome(accepted, 'source_audit')['status'], 'unexpected_acceptance')

    def test_canonical_ir_excludes_only_top_level_identity(self):
        program = {'inputs': {'source': 'hash'}, 'lean_version': '4.35.0', 'lean_commit': 'abc',
                   'functions': [], 'provenance': [{'inputs': 'keep'}], 'claims': [], 'rewrites': []}
        canonical = results.canonical_ir(program)
        self.assertIn('keep', canonical)
        self.assertEqual(canonical, results.canonical_ir(dict(program, inputs={}, lean_version='next', lean_commit='def')))
        for key in ('functions', 'provenance', 'claims', 'rewrites'):
            changed = dict(program, **{key: ['different']})
            self.assertNotEqual(canonical, results.canonical_ir(changed))
        with self.assertRaises(identity.HarnessError):
            results.canonical_ir({})

    def test_shape_contract_requires_called_survivor(self):
        program = {'functions': [{'name': 'A.run._redArg', 'body': {'op': 'return'}}]}
        expected = {'called_function_suffixes': ['._redArg'], 'ops': ['join']}
        self.assertEqual(len(results.ir_shapes(program, expected)), 2)
        program['functions'].append({'name': 'A.run', 'body': {'op': 'join', 'value': {'kind': 'call', 'name': 'A.run._redArg'}}})
        self.assertEqual(results.ir_shapes(program, expected), [])

    def test_ir_drift_and_candidate_failures_are_differences(self):
        baseline = {'a': {'status': 'accepted', 'canonical_ir_sha256': 'a'}}
        for after, expected in [({'status': 'accepted', 'canonical_ir_sha256': 'b'}, 'canonical_ir_drift'),
                                ({'status': 'behavior_mismatch'}, 'behavior_mismatch'),
                                ({'status': 'not_run'}, 'not_run')]:
            self.assertEqual(results.compare_cases(baseline, {'a': after}), [{'case': 'a', 'kind': expected}])
        self.assertEqual(results.compare_cases(baseline, baseline), [])

    def test_malformed_manifest_is_precondition_failure(self):
        valid = {'name': 'example', 'module': 'Example', 'roots': ['run']}
        for mutation in ({'reject': []}, {'reject': 'producer_failed'}, {'roots': 'run'},
                         {'roots': []}, {'shapes': []}, {'shapes': {'ops': 'join'}}, {'name': '../outside'}):
            with self.subTest(mutation=mutation), self.assertRaises((identity.HarnessError, ExportError)):
                results.validate_cases([dict(valid, **mutation)])
        self.assertEqual(results.validate_cases([valid]), [valid])


class RunnerTests(unittest.TestCase):
    def test_reused_overlapping_and_symlink_outputs_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            parent = Path(tmp)
            repo = parent / 'repo'
            repo.mkdir()
            for path in (repo, repo / 'output', parent):
                with self.assertRaises(identity.HarnessError):
                    runner.validate_output(path, repo, [])
            alias = parent / 'alias'
            alias.symlink_to(repo, target_is_directory=True)
            with self.assertRaises(identity.HarnessError):
                runner.validate_output(alias / 'new', repo, [])
            fresh = parent / 'fresh'
            self.assertEqual(runner.validate_output(fresh, repo, []), fresh)

    def test_command_timeout_is_recorded_and_not_admission(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            env = {'CGO_ENABLED': '0', 'SECRET_FOR_TEST': 'do-not-log'}
            with patch.object(runner, 'run_process', side_effect=subprocess.TimeoutExpired(['lean'], 1, output='before', stderr='timeout')):
                result = runner.Commands(1).run('extract', ['lean'], directory, env, directory)
            record = json.loads((directory / 'commands.jsonl').read_text())
            self.assertEqual(record['stdout'], 'before')
            self.assertNotIn('SECRET_FOR_TEST', record['environment'])
            self.assertEqual(results.extraction_outcome(result, 'source_audit')['status'], 'extraction_protocol_failed')

    def test_producer_build_failure_keeps_every_case_not_run(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            corpus = root / 'corpus'
            (root / 'reproducers').mkdir()
            (root / 'reproducers/NumericBoundaryName.lean').write_text('probe')
            cases = [{'name': 'accepted'}, {'name': 'negative', 'reject': 'source_audit'}]
            commands = runner.Commands(1)
            outputs = [subprocess.CompletedProcess([], 0, 'probe', ''),
                       subprocess.CompletedProcess([], 1, '', 'producer API changed')]
            snapshot = identity.source_snapshot(ROOT)
            with patch.object(runner, 'observe_compiler', return_value=NEXT), \
                 patch.object(commands, 'run', side_effect=outputs), \
                 patch.object(runner, 'run_case') as fixture:
                result = runner.run_compiler(root / 'candidate', root / 'bin', snapshot, corpus,
                                             cases, '/tool/go', commands, baseline=False)
            self.assertEqual(result['status'], 'producer_build_failed')
            self.assertEqual(result['cases'], {'accepted': {'status': 'not_run'}, 'negative': {'status': 'not_run'}})
            fixture.assert_not_called()

    def test_malformed_success_and_wrong_behavior_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            consumer = directory / 'consumer'
            consumer.mkdir()
            (directory / 'program.ir.json').write_text('{}')
            commands = runner.Commands(1)
            result = runner.check_accepted({}, directory, directory, consumer, BASE, '/tool/go', {}, commands)
            self.assertEqual(result['status'], 'extraction_protocol_failed')
            program = {'lean_version': BASE['version'], 'lean_commit': BASE['commit'], 'inputs': {}, 'functions': []}
            (directory / 'program.ir.json').write_text(json.dumps(program))
            (directory / 'consumer_test.go.txt').write_text('package consumer')
            with patch.object(commands, 'run', side_effect=[subprocess.CompletedProcess([],0,'',''),
                                                           subprocess.CompletedProcess([],1,'assertion failed','')]):
                result = runner.check_accepted({}, directory, directory, consumer, BASE, '/tool/go', {}, commands)
            self.assertEqual(result['status'], 'behavior_mismatch')
            self.assertEqual(result['behavior'], 'failed')

    def assert_replay_arguments(self, source, *, timeout, case=None):
        with patch.object(sys, 'argv', ['replay.py', '/new/artifacts']), \
             patch.object(subprocess, 'call', return_value=7) as called, \
             self.assertRaises(SystemExit) as stopped:
            exec(compile(source, 'replay.py', 'exec'), {'__file__': '/retained/replay.py'})
        expected = [sys.executable, '/retained/source/scripts/check_compatibility.py',
                    '--baseline-bin', '/baseline/bin', '--timeout', str(timeout),
                    '--candidate-bin', '/candidate/bin']
        if case:
            expected += ['--case', case]
        expected += ['--artifacts', '/new/artifacts']
        called.assert_called_once_with(expected)
        self.assertEqual(stopped.exception.code, 7)

    def test_executable_replay_preserves_nondefault_timeout(self):
        for case in (None, 'numeric'):
            with self.subTest(case=case):
                source = runner.replay_script(Path('/baseline/bin'), Path('/candidate/bin'),
                                              case, timeout=12.75)
                self.assert_replay_arguments(source, timeout=12.75, case=case)

    def test_difference_bundle_is_source_only_and_replays_one_case(self):
        import zipfile
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory / 'source').mkdir()
            (directory / 'source/input.txt').write_text('snapshot')
            runner.write_json(directory / 'inputs.json', {'input.txt': 'hash'})
            report = {'differences': [{'case': 'example', 'kind': 'canonical_ir_drift'}]}
            runner.write_json(directory / 'report.json', report)
            for role, content in (('baseline', '{"body": 1}\n'), ('candidate', '{"body": 2}\n')):
                case_dir = directory / role / 'cases/example'
                case_dir.mkdir(parents=True)
                (case_dir / 'program.canonical.json').write_text(content)
                (case_dir / 'consumer.test').write_text('binary')
                (case_dir / '.lake').mkdir()
            runner.difference_bundles(directory, report, Path('/baseline/bin'), Path('/candidate/bin'), timeout=18.5)
            with zipfile.ZipFile(directory / 'differences/example/replay.zip') as archive:
                self.assertIn('source/input.txt', archive.namelist())
                self.assertIn('ir.diff', archive.namelist())
                self.assertFalse(any(name.endswith('consumer.test') for name in archive.namelist()))
                replay = archive.read('replay.py').decode()
                self.assertIn("'--case', 'example'", replay)
                self.assert_replay_arguments(replay, timeout=18.5, case='example')

    def test_observed_prefix_must_match_selected_installation(self):
        outputs = [subprocess.CompletedProcess([],0,version(),''),
                   subprocess.CompletedProcess([],0,version(),''),
                   subprocess.CompletedProcess([],0,'/different/install\n','')]
        commands = runner.Commands(1)
        with patch.object(commands, 'run', side_effect=outputs), self.assertRaisesRegex(identity.HarnessError, 'print-prefix'):
            runner.observe_compiler(Path('/selected/bin'), Path('/work'), {}, commands, Path('/logs'))


if __name__ == '__main__':
    unittest.main()
