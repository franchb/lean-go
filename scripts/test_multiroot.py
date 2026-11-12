"""Canonical multi-root requests and fresh union-closure admission."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import export_module as producer
import generate


class RootSelectionTests(unittest.TestCase):
    def test_string_api_and_canonical_lists(self):
        self.assertEqual(producer.select_roots('Example.run'), ['Example.run'])
        self.assertEqual(producer.select_roots(('Example.z', 'Example.a')), ['Example.a', 'Example.z'])
        names = [f'Example.root{i}' for i in range(64)]
        self.assertEqual(producer.select_roots(names), sorted(names))

    def test_malformed_and_duplicate_requests_fail_before_processes(self):
        invalid = (None, [], {}, ['A', 'A'], ['A', 1], ['A', 'A; import Lean'],
                   ['A', 'A.λ'], [f'A.root{i}' for i in range(65)])
        for selection in invalid:
            with self.subTest(selection=selection), mock.patch.object(producer, 'run') as run:
                with self.assertRaises(producer.ExportError):
                    producer.export_module(Path('/unused'), Path('/unused'), 'Example',
                                           selection, Path('/unused/result.json'))
                run.assert_not_called()

    def test_producer_identity_and_exact_selection_fields(self):
        common = dict(module='Example', types=[], functions=[], claims=[], rewrites=[],
                      provenance=[], api={'records': {}})
        single = producer.complete_program(json.dumps(dict(common, root='Example.a')).encode(),
                                           'Example', ['Example.a'], {})
        self.assertEqual((single['schema'], single['profile']), ('lean-go-ir/v6', 'pure-module/v4'))
        self.assertNotIn('roots', single)
        roots = ['Example.a', 'Example.b']
        multi = producer.complete_program(json.dumps(dict(common, roots=roots)).encode(),
                                          'Example', roots, {})
        self.assertEqual((multi['schema'], multi['profile']), ('lean-go-ir/v7', 'pure-module/v5'))
        self.assertNotIn('root', multi)
        for changed in (dict(common, roots=list(reversed(roots))), dict(common, roots=['Example.a']),
                        dict(common, roots=roots, root='Example.a'), dict(common, root='Example.a'),
                        dict(common, roots=roots, module='Other')):
            with self.subTest(changed=changed), self.assertRaises(producer.ExportError):
                producer.complete_program(json.dumps(changed).encode(), 'Example', roots, {})

    def test_multi_root_schema_uses_module_freshness(self):
        program = {'schema': 'lean-go-ir/v7'}
        with mock.patch.object(generate, 'verify_module_inputs') as verify:
            generate.verify_inputs(Path('/project'), program, Path('/source'))
            verify.assert_called_once_with(Path('/project'), Path('/source'), program)
        with self.assertRaisesRegex(ValueError, 'source directory'):
            generate.verify_inputs(Path('/project'), program)

    def test_cli_repeats_canonical_roots_and_keeps_single_output_workflow(self):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            source = work / 'source'
            source.mkdir()
            (source / 'Example.lean').write_text('def run (x : UInt32) := x\n')
            arguments = ['generate.py', '--source-dir', str(source), '--module', 'Example',
                         '--root', 'Example.z', '--root', 'Example.a',
                         '--out', str(work / 'package'), '--ir-output', str(work / 'program.json')]
            commands = []
            def process(command, **kwargs):
                commands.append(command)
                if command[-1] == 'version':
                    return subprocess.CompletedProcess(command, 0, 'go version go1.27.4 linux/amd64\n')
                if '--output' in command:
                    Path(command[command.index('--output') + 1]).write_text('{"schema":"lean-go-ir/v7"}')
                return subprocess.CompletedProcess(command, 0)
            with mock.patch.object(generate.sys, 'argv', arguments), \
                    mock.patch.object(generate.shutil, 'which', return_value='go'), \
                    mock.patch.object(generate.subprocess, 'run', side_effect=process), \
                    mock.patch.object(generate, 'verify_inputs'), mock.patch('builtins.print'):
                generate.main()
            export = commands[1]
            self.assertEqual(export[-4:], ['--root', 'Example.a', '--root', 'Example.z'])
            self.assertEqual(json.loads((work / 'program.json').read_text())['schema'], 'lean-go-ir/v7')


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class MultiRootAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.directory = Path(self.enterContext(tempfile.TemporaryDirectory(prefix='lean-go-multi-')))
        self.source = self.directory / 'source'
        self.source.mkdir()
        self.repo = Path(__file__).resolve().parents[1]
        self.output = self.directory / 'program.json'

    def export(self, entries):
        producer.export_module(self.repo, self.source, 'Example', entries, self.output)
        return json.loads(self.output.read_bytes())

    def shared_sources(self):
        (self.source / 'Shared.lean').write_text('''namespace Shared
structure Box where
  value : UInt64
@[noinline] def bump (n : UInt64) : UInt64 := n + 1
def advance (s : Box) (delta : UInt64) : Box := { value := bump (s.value + delta) }
def read (s : Box) : UInt64 := bump s.value
def enabled (flag : Bool) : Bool := !flag
end Shared
''')
        (self.source / 'Example.lean').write_text('import Shared\n')

    def test_union_closure_imports_and_request_order_are_deterministic(self):
        self.shared_sources()
        roots = ['Shared.read', 'Shared.enabled', 'Shared.advance']
        program = self.export(roots)
        original = self.output.read_bytes()
        self.export(list(reversed(roots)))
        self.assertEqual(self.output.read_bytes(), original)
        self.assertEqual(program['roots'], sorted(roots))
        self.assertNotIn('root', program)
        self.assertEqual(program['schema'], 'lean-go-ir/v7')
        functions = {item['name']: item for item in program['functions']}
        self.assertEqual(sum(item['name'] == 'Shared.bump' for item in program['functions']), 1)
        self.assertEqual(sum(item['name'] == 'Shared.Box' for item in program['types']), 1)
        self.assertEqual([p['type'] for p in functions['Shared.advance']['params']], ['Shared.Box', 'UInt64'])
        self.assertEqual(functions['Shared.advance']['result'], 'Shared.Box')
        self.assertEqual(functions['Shared.read']['params'][0]['type'], 'Shared.Box')
        self.assertEqual(functions['Shared.enabled']['result'], 'Bool')
        self.assertEqual(program['api']['records']['Shared.Box'], ['value'])
        provenance = {item['name'] for item in program['provenance']}
        self.assertTrue(set(roots) | {'Shared.bump'} <= provenance)
        self.assertIn('source/Shared.lean', program['inputs'])

    def test_one_root_list_retains_single_root_v6_contract(self):
        self.shared_sources()
        program = self.export(['Shared.read'])
        original = self.output.read_bytes()
        self.export('Shared.read')
        self.assertEqual(self.output.read_bytes(), original)
        self.assertEqual(program['root'], 'Shared.read')
        self.assertNotIn('roots', program)
        self.assertEqual((program['schema'], program['profile']), ('lean-go-ir/v6', 'pure-module/v4'))

    def test_bad_second_root_never_publishes_partial_output(self):
        (self.source / 'Example.lean').write_text('''namespace Example
@[extern "unavailable_external"] opaque externalValue (n : UInt64) : UInt64
def aGood (n : UInt64) : UInt64 := n
def zBad (n : UInt64) : UInt64 := externalValue n
end Example
''')
        for roots, message in ((['Example.aGood', 'Example.zBad'], 'extern source dependency'),
                               (['Example.aGood', 'Example.zMissing'], 'Unknown constant')):
            with self.subTest(roots=roots):
                self.output.write_text('existing output')
                with self.assertRaisesRegex(producer.ExportError, message):
                    self.export(roots)
                self.assertEqual(self.output.read_text(), 'existing output')


if __name__ == '__main__':
    unittest.main()
