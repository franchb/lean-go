"""Exercise campaign failures and replay without requiring compiler tools."""
from dataclasses import replace
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import differential
import differential_backend
from differential_backend import BackendError
from differential_cases import (CHANNELS, compare, load_case, reduce_case,
                                save_case, validate_inputs)
from differential_model import (Expr, Program, complexity, evaluate, render_lean,
                                serialize)


def addition(width=32):
    return Program(91, width, Expr('add', (Expr('a'), Expr('b'))), Expr('a'))


def results(program, pairs, broken=()):
    expected = [evaluate(program, a, b) for a, b in pairs]
    return {name: [(value + (name in broken)) % (1 << program.width)
                   for value in expected] for name in CHANNELS}


class CaseTests(unittest.TestCase):
    def test_compare_reports_exact_channels_and_input_indexes(self):
        program = addition(64)
        pairs = [((1 << 64) - 1, 1), (1, 1)]
        output = {'native': [0, 2], 'raw': [0, 3], 'typed': [1, 2]}
        observed = compare(program, pairs, output)
        self.assertEqual(observed.expected, [0, 2])
        self.assertEqual(observed.signature, ('raw', 'typed'))
        self.assertEqual(observed.mismatches, {'native': [], 'raw': [1], 'typed': [0]})

    def test_invalid_inputs_fail_before_evaluation(self):
        for pairs in ([], [(1,)], [(1, 2, 3)], [(True, 0)], [(0, -1)],
                      [(0, 1 << 32)], [(1.0, 2)], '1,2', [(0, 0)] * 4097):
            with self.subTest(pairs=repr(pairs)[:70]), self.assertRaises(ValueError):
                validate_inputs(addition(), pairs)

    def test_backend_protocol_cannot_be_a_semantic_pass(self):
        good = {'native': [3], 'raw': [3], 'typed': [3]}
        bad = [None, {'raw': [3], 'typed': [3]}, dict(good, extra=[])]
        for value in ([], [True], [-1], [1 << 32], [3.0], (3,), '3'):
            bad.append(dict(good, raw=value))
        for output in bad:
            with self.subTest(output=output), self.assertRaises(ValueError):
                compare(addition(), [(1, 2)], output)

    def test_artifact_roundtrip_uses_full_program_and_exact_wide_inputs(self):
        program = replace(addition(64), expr=Expr('mul', (Expr('a'), Expr('b'))))
        pairs = [((1 << 64) - 1, (1 << 53) + 1)]
        observation = compare(program, pairs, results(program, pairs, ('typed',)))
        with tempfile.TemporaryDirectory() as temporary:
            artifact = Path(temporary) / 'case.json'
            save_case(artifact, program, pairs, observation)
            stored = json.loads(artifact.read_text())
            self.assertEqual(stored['program'], serialize(program))
            self.assertEqual(stored['observation']['mismatches']['typed'], [0])
            stored['observation'] = {'expected': 'untrusted historical evidence'}
            artifact.write_text(json.dumps(stored))
            actual_program, actual_pairs = load_case(artifact)
        self.assertEqual((actual_program, actual_pairs), (program, pairs))
        self.assertEqual(render_lean(actual_program), render_lean(program))

    def test_invalid_artifact_schema_fields_and_inputs_are_rejected(self):
        program = serialize(addition())
        valid = {'schema': 'lean-go-differential-case/v1', 'program': program,
                 'inputs': [[1, 2]]}
        malformed = [[], {}, dict(valid, schema='future'), dict(valid, extra=1),
                     dict(valid, program={}), dict(valid, inputs=[[False, 0]]),
                     {key: value for key, value in valid.items() if key != 'inputs'}]
        with tempfile.TemporaryDirectory() as temporary:
            artifact = Path(temporary) / 'case.json'
            for data in malformed:
                artifact.write_text(json.dumps(data))
                with self.subTest(data=data), self.assertRaises(ValueError):
                    load_case(artifact)
            artifact.write_text(' ' * 2_000_001)
            with self.assertRaisesRegex(ValueError, '2 MB'):
                load_case(artifact)


class ReductionTests(unittest.TestCase):
    def test_reduction_keeps_exact_failing_channels(self):
        program, pairs = addition(), [(9, 8), (7, 6)]
        initial = compare(program, pairs, results(program, pairs, ('raw',)))
        def check(candidate, values):
            broken = ('raw', 'typed') if values == [pairs[0]] else ('raw',)
            return compare(candidate, values, results(candidate, values, broken))
        reduced = reduce_case(program, pairs, initial, check, attempts=2)
        self.assertEqual(reduced.inputs, [pairs[1]])
        self.assertEqual(reduced.observation.signature, ('raw',))
        self.assertEqual(reduced.attempts, 2)

    def test_compile_rejection_is_not_a_counterexample(self):
        program, pairs = addition(), [(9, 8), (7, 6)]
        initial = compare(program, pairs, results(program, pairs, ('raw',)))
        def check(candidate, values):
            if values == [pairs[0]]:
                raise BackendError('extract', 'unsupported candidate')
            return compare(candidate, values, results(candidate, values, ('raw',)))
        reduced = reduce_case(program, pairs, initial, check, attempts=2)
        self.assertEqual(reduced.inputs, [pairs[1]])
        self.assertEqual(reduced.rejected_errors, 1)
        self.assertEqual(reduced.observation.signature, ('raw',))

    def test_protocol_errors_and_budget_leave_original_failure(self):
        program, pairs = addition(), [(9, 8), (7, 6)]
        initial = compare(program, pairs, results(program, pairs, ('typed',)))
        check = mock.Mock(side_effect=ValueError('invalid backend output'))
        reduced = reduce_case(program, pairs, initial, check, attempts=3)
        self.assertEqual(check.call_count, 3)
        self.assertEqual(reduced.attempts, 3)
        self.assertEqual(reduced.rejected_errors, 3)
        self.assertEqual((reduced.program, reduced.inputs, reduced.observation),
                         (program, pairs, initial))

    def test_zero_budget_does_not_execute_backend(self):
        program, pairs = addition(), [(9, 8)]
        initial = compare(program, pairs, results(program, pairs, ('raw',)))
        check = mock.Mock()
        reduced = reduce_case(program, pairs, initial, check, attempts=0)
        check.assert_not_called()
        self.assertEqual(reduced.attempts, 0)
        self.assertEqual(reduced.inputs, pairs)

    def test_passing_case_and_invalid_budgets_are_not_shrunk(self):
        program, pairs = addition(), [(0, 0)]
        passed = compare(program, pairs, results(program, pairs))
        check = mock.Mock()
        with self.assertRaisesRegex(ValueError, 'passing'):
            reduce_case(program, pairs, passed, check)
        failed = compare(program, pairs, results(program, pairs, ('raw',)))
        for budget in (-1, 1001, True, 1.0):
            with self.subTest(budget=budget), self.assertRaises(ValueError):
                reduce_case(program, pairs, failed, check, attempts=budget)
        check.assert_not_called()

    def test_injected_semantic_bug_shrinks_inputs_and_program(self):
        program = replace(addition(), expr=Expr('add', (addition().expr, Expr('lit', value=42))))
        pairs = [(255, 31), (7, 8)]
        def check(candidate, values):
            return compare(candidate, values, results(candidate, values, ('raw',)))
        reduced = reduce_case(program, pairs, check(program, pairs), check, attempts=50)
        self.assertEqual(reduced.inputs, [(0, 0)])
        self.assertLess(complexity(reduced.program), complexity(program))
        self.assertEqual(reduced.observation.signature, ('raw',))
        self.assertEqual(reduced.rejected_errors, 0)


class CampaignTests(unittest.TestCase):
    def run_campaign(self, arguments, check):
        backend = mock.Mock()
        backend.check.side_effect = check
        with mock.patch.object(differential, 'Backend', return_value=backend), \
                mock.patch('sys.stdout', new_callable=io.StringIO):
            status = differential.main(arguments)
        return status, backend

    def report(self, directory):
        paths = list(Path(directory).glob('campaign-*/report.json'))
        self.assertEqual(len(paths), 1)
        return paths[0].parent, json.loads(paths[0].read_text())

    def test_passing_campaign_records_exact_source_and_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            status, backend = self.run_campaign(
                ['--seed', '4', '--width', '64', '--cases', '16', '--artifacts', temporary],
                results)
            campaign, report = self.report(temporary)
            self.assertEqual(status, 0)
            self.assertEqual(report['planned_programs'], 1)
            self.assertEqual(report['completed_programs'], 1)
            self.assertEqual(report['results'][0]['status'], 'passed')
            case = campaign / 'case-0000'
            program, pairs = load_case(case / 'case.json')
            self.assertEqual(case.joinpath('Generated.lean').read_text(), render_lean(program))
            self.assertEqual(len(pairs), 16)
            self.assertEqual(backend.check.call_count, 1)
            self.assertFalse((case / 'failure.json').exists())

    def test_mismatch_stops_campaign_and_persists_original_and_reduction(self):
        def mismatch(program, pairs):
            return results(program, pairs, ('raw',))
        with tempfile.TemporaryDirectory() as temporary:
            status, _ = self.run_campaign(
                ['--seed', '4', '--seed', '5', '--width', '32', '--cases', '3',
                 '--shrink', '--shrink-attempts', '6', '--artifacts', temporary], mismatch)
            campaign, report = self.report(temporary)
            self.assertEqual(status, 1)
            self.assertEqual(report['planned_programs'], 2)
            self.assertEqual(report['completed_programs'], 1)
            result = report['results'][0]
            self.assertEqual(result['status'], 'mismatch')
            self.assertEqual(result['channels'], ['raw'])
            case = campaign / 'case-0000'
            original, original_inputs = load_case(case / 'failure.json')
            minimized, minimized_inputs = load_case(case / 'minimized.json')
            self.assertEqual((original, original_inputs), load_case(case / 'case.json'))
            self.assertEqual(case.joinpath('Generated.lean').read_text(), render_lean(original))
            self.assertEqual(case.joinpath('Minimized.lean').read_text(), render_lean(minimized))
            self.assertLessEqual(len(minimized_inputs), len(original_inputs))
            self.assertEqual(compare(minimized, minimized_inputs,
                                     mismatch(minimized, minimized_inputs)).signature, ('raw',))

    def test_execution_error_is_saved_and_never_shrunk(self):
        with tempfile.TemporaryDirectory() as temporary:
            status, backend = self.run_campaign(
                ['--seed', '0', '--width', '32', '--shrink', '--artifacts', temporary],
                mock.Mock(side_effect=BackendError('native-build', 'compile failed')))
            campaign, report = self.report(temporary)
            self.assertEqual(status, 1)
            self.assertEqual(report['results'][0]['status'], 'error')
            failure = json.loads((campaign / 'case-0000/failure.json').read_text())
            self.assertIn('native-build', failure['error'])
            self.assertNotIn('observation', failure)
            self.assertFalse((campaign / 'case-0000/minimized.json').exists())
            self.assertEqual(backend.check.call_count, 1)

    def test_replay_does_not_call_generator_or_replace_stored_inputs(self):
        program = replace(addition(64), expr=Expr('sub', (Expr('a'), Expr('b'))))
        pairs = [((1 << 64) - 1, (1 << 53) + 3)]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            artifact = root / 'saved.json'
            save_case(artifact, program, pairs)
            with mock.patch.object(differential, 'generate', side_effect=AssertionError('generated')), \
                    mock.patch.object(differential, 'inputs', side_effect=AssertionError('inputs')):
                status, backend = self.run_campaign(
                    ['--replay', str(artifact), '--artifacts', str(root / 'replay')], results)
            self.assertEqual(status, 0)
            backend.check.assert_called_once_with(program, pairs)

    def test_replay_rejects_generation_selection_arguments(self):
        for argument in (['--seed', '1'], ['--width', '32']):
            args = differential.parser().parse_args(['--replay', 'unused.json', *argument])
            with self.subTest(argument=argument), self.assertRaisesRegex(ValueError, 'combined'):
                list(differential.selected_cases(args))


@unittest.skipUnless(os.environ.get('LEAN_GO_DIFFERENTIAL') == '1',
                     'set LEAN_GO_DIFFERENTIAL=1 for compiled fault-injection replay')
class CompiledFaultInjectionTests(unittest.TestCase):
    def test_compiled_mismatch_is_reduced_and_replayed_with_clean_backend(self):
        from differential_model import generate

        original_consumer = differential_backend.consumer_source
        def faulty_consumer(width):
            source = original_consumer(width)
            self.assertEqual(source.count('result.Raw[i] = value'), 1)
            return source.replace('result.Raw[i] = value', 'result.Raw[i] = value + 1')

        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix='lean-go-compiled-fault-') as temporary:
            directory = Path(temporary)
            backend = differential_backend.Backend(repo, directory / 'faulty')
            program = generate(0, 32, depth=1)
            pairs = [(0, 0), ((1 << 32) - 1, 65)]
            def check(candidate, values):
                return compare(candidate, values, backend.check(candidate, values))

            with mock.patch.object(differential_backend, 'consumer_source', faulty_consumer):
                original = check(program, pairs)
                self.assertEqual(original.signature, ('raw',))
                reduced = reduce_case(program, pairs, original, check, attempts=2)
                self.assertEqual(reduced.observation.signature, ('raw',))
                self.assertEqual(reduced.attempts, 2)
                self.assertEqual(reduced.rejected_errors, 0)
                self.assertEqual(reduced.inputs, [(0, 0)])
                self.assertLess(complexity(reduced.program), complexity(program))
                artifact = directory / 'minimized.json'
                save_case(artifact, reduced.program, reduced.inputs, reduced.observation)

            replay_program, replay_inputs = load_case(artifact)
            self.assertEqual((replay_program, replay_inputs), (reduced.program, reduced.inputs))
            clean = differential_backend.Backend(repo, directory / 'clean')
            replayed = compare(replay_program, replay_inputs, clean.check(replay_program, replay_inputs))
            self.assertEqual(replayed.signature, ())
            self.assertEqual(replayed.outputs['native'], replayed.expected)
            self.assertEqual(replayed.outputs['raw'], replayed.expected)
            self.assertEqual(replayed.outputs['typed'], replayed.expected)


if __name__ == '__main__':
    unittest.main()
