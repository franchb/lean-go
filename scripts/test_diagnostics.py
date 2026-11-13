"""Protocol and exact source mapping tests, independent of the Lean toolchain."""
from contextlib import redirect_stderr
import io
import json
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch

import diagnostics
from source_locations import SourceMap
import export_module


class DiagnosticTests(unittest.TestCase):
    def test_unknown_context_is_null_and_json_is_one_report(self):
        stream = io.StringIO()
        with redirect_stderr(stream):
            diagnostics.emit(ValueError('failed\nwith detail'), 'json')
        report = json.loads(stream.getvalue())
        self.assertEqual(report['schema'], 'lean-go-diagnostics/v1')
        self.assertFalse(report['success'])
        item, = report['diagnostics']
        self.assertEqual(item['stage'], 'input')
        self.assertEqual(item['message'], 'failed\nwith detail')
        for field in ('declaration', 'compiler_form', 'location'):
            self.assertIsNone(item[field])
        self.assertEqual(len(stream.getvalue().splitlines()), 1)

    def test_context_preserves_exception_and_reason(self):
        error = ValueError('old exact reason')
        with self.assertRaisesRegex(ValueError, '^old exact reason$'):
            with diagnostics.stage('toolchain', 'toolchain_mismatch'):
                raise error
        self.assertEqual(diagnostics.report(error)['diagnostics'][0]['stage'], 'toolchain')
        stream = io.StringIO()
        with redirect_stderr(stream):
            diagnostics.emit(error, 'text')
        self.assertIn('toolchain_mismatch', stream.getvalue())
        self.assertIn('old exact reason', stream.getvalue())

    def test_readable_error_includes_known_compiler_form(self):
        error = ValueError('unsupported control')
        error.diagnostic = diagnostics.diagnostic(error, stage='admission',
            code='unsupported_lcnf_control', compiler_form='LCNF.Code.fun')
        stream = io.StringIO()
        with redirect_stderr(stream):
            diagnostics.emit(error)
        self.assertIn('[unsupported_lcnf_control; LCNF.Code.fun]', stream.getvalue())

    def test_child_protocol_checks_types_codes_and_location_context(self):
        valid = diagnostics.diagnostic('rejected', stage='admission', code='unsupported_type', hint='fix source')
        raw_location = {'module': 'Example', 'kind': 'declaration', 'declaration': 123,
                        'start': {'line': 1, 'column': 0}, 'end': {'line': 1, 'column': 1}}
        with self.assertRaises(ValueError):
            diagnostics.producer_diagnostic({'schema': diagnostics.SCHEMA,
                                              'diagnostic': dict(valid, location=raw_location)})
        self.assertIsNone(diagnostics.map_location(raw_location, {'Example': None}))
        for changed in (dict(valid, code='fake_rejection'), dict(valid, message=[]),
                        dict(valid, stage=[]), dict(valid, location=raw_location)):
            result = subprocess.CompletedProcess([], 1, '', json.dumps(
                {'schema': diagnostics.SCHEMA, 'success': False, 'diagnostics': [changed]}))
            stream = io.StringIO()
            with self.subTest(changed=changed), redirect_stderr(stream):
                self.assertEqual(diagnostics.forward_process(result), 1)
            item, = json.loads(stream.getvalue())['diagnostics']
            self.assertEqual((item['stage'], item['code']), ('build', 'pipeline_failed'))

    def test_invalid_producer_protocol_never_becomes_admission(self):
        for value in ({}, {'code': 'source_audit'}, {'schema': 'future'}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                diagnostics.producer_diagnostic(value)


class SourceMappingTests(unittest.TestCase):
    def mapped(self, original):
        result, _ = export_module.instrument_source(original, with_mapping=True)
        self.assertIsInstance(result, SourceMap)
        return result

    def test_unicode_crlf_and_same_line_import(self):
        original = '/- 😀 header -/ module\r\npublic import Init /- 😀 inline -/ public def «😀α» (x : UInt32) : UInt32 := x\r\n'.encode()
        mapped = self.mapped(original)
        self.assertEqual(mapped.position(5, 27),
                         {'line': 2, 'column': 45, 'utf16_column': 46,
                          'byte_offset': original.index('«'.encode())})
        self.assertEqual(mapped.position(1, 3)['byte_offset'], 3)
        self.assertIsNone(mapped.position(3, 0))
        self.assertIsNone(mapped.position(0, 0))
        self.assertIsNone(mapped.position(999, 0))
        self.assertIsNone(mapped.position(1, 999))
        self.assertEqual(mapped.original, original)

    def test_unchanged_prefix_and_end_exclusive(self):
        mapped = self.mapped(b'import Init\ndef f := 0\n')
        self.assertEqual(mapped.position(1, 0)['byte_offset'], 0)
        self.assertEqual(mapped.position(5, 4)['byte_offset'], 16)
        self.assertEqual(mapped.position(6, 0)['byte_offset'], len(mapped.original))

    def test_location_only_names_snapshotted_sources(self):
        mapped = self.mapped(b'def f := 0\n')
        sources = {'Example': ('Example.lean', mapped)}
        raw = {'module': 'Example', 'kind': 'declaration', 'declaration': 'f',
               'start': {'line': 4, 'column': 4}, 'end': {'line': 4, 'column': 5}}
        location = diagnostics.map_location(raw, sources)
        self.assertEqual(location['path'], 'Example.lean')
        self.assertEqual(location['start']['line'], 1)
        self.assertEqual(location['end']['column'], 5)
        self.assertIsNone(diagnostics.map_location(dict(raw, module='Lean'), sources))
        self.assertIsNone(diagnostics.map_location(dict(raw, start={'line': 2, 'column': 0}), sources))

class CompilerProtocolTests(unittest.TestCase):
    def test_both_compiler_location_formats_and_workspace_identity(self):
        mapped, _ = export_module.instrument_source(b'def f := broken\n', with_mapping=True)
        sources = {'Example': ('Example.lean', mapped)}
        for message in ('Example.lean:4:9: error: unknown identifier',
                        'error: Example.lean:4:9: unknown identifier',
                        'error: /fresh/Example.lean:4:9: unexpected token'):
            location = diagnostics.compiler_location(message, sources, Path('/fresh'))
            self.assertEqual(location['start']['line'], 1)
            self.assertEqual(location['start']['column'], 9)
        location = diagnostics.compiler_location('error: C:\\fresh\\Example.lean:4:9: unexpected token', sources, 'C:\\fresh')
        self.assertEqual(location['start']['line'], 1)
        self.assertIsNone(diagnostics.compiler_location(
            'error: /unrelated/Example.lean:4:9: unexpected token', sources, Path('/fresh')))

    def test_malformed_context_types_are_not_interpreted(self):
        valid = diagnostics.diagnostic('rejected', stage='admission', code='source_audit', hint='fix source')
        for field, value in [('code', []), ('code', {}), ('declaration', 4), ('hint', None)]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                diagnostics.producer_diagnostic({'schema': diagnostics.SCHEMA,
                    'diagnostic': dict(valid, **{field: value})})
        self.assertIsNone(diagnostics.map_location({'module': []}, {}))
        self.assertIsNone(diagnostics.map_location({'module': 'Example', 'kind': []}, {'Example': None}))

    def test_unknown_failure_and_invalid_protocol_are_build_failures(self):
        from export import ExportError
        from module_diagnostics import DiagnosticError, run_compiler
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory)
            def fail(command, root, env):
                raise ExportError('internal compiler crash')
            with self.assertRaises(DiagnosticError) as caught:
                run_compiler(fail, [], workspace, {}, {}, admission=True)
            self.assertEqual(caught.exception.diagnostic['stage'], 'build')
            self.assertEqual(caught.exception.diagnostic['code'], 'producer_failed')
            def malformed(command, root, env):
                (root / 'lean-go-diagnostic.json').write_text('{"schema":1,"schema":2}')
                raise ExportError('failed')
            with self.assertRaises(DiagnosticError) as caught:
                run_compiler(malformed, [], workspace, {}, {}, admission=True)
            self.assertEqual(caught.exception.diagnostic['code'], 'producer_protocol')


class ScriptBoundaryTests(unittest.TestCase):
    def test_repeated_modes_follow_argparse_last_value_for_both_spellings(self):
        import generate
        for first, last in [('json', 'text'), ('text', 'json')]:
            combinations = [
                [f'--diagnostics={first}', f'--diagnostics={last}'],
                [f'--diagnostics={first}', '--diagnostics', last],
                ['--diagnostics', first, f'--diagnostics={last}'],
                ['--diagnostics', first, '--diagnostics', last],
            ]
            for flags in combinations:
                arguments = ['generate.py', '--module', 'Example', *flags]
                output = io.StringIO()
                with self.subTest(flags=flags), patch.object(generate.sys, 'argv', arguments), redirect_stderr(output):
                    self.assertEqual(generate.cli(), 1)
                    if last == 'json':
                        item, = json.loads(output.getvalue())['diagnostics']
                        self.assertEqual(item['code'], 'invalid_input')
                    else:
                        self.assertTrue(output.getvalue().startswith('input [invalid_input]'), output.getvalue())
                    self.assertEqual(diagnostics.requested_mode(arguments[1:]), last)

    def test_unexpected_script_errors_are_json_build_failures(self):
        import generate
        cases = [(generate, generate.cli, ['generate.py', '--diagnostics=json'], 'main'),
                 (export_module, export_module.main,
                  ['export_module.py', '--source-dir', 'source', '--module', 'Example',
                   '--root', 'run', '--output', 'output', '--diagnostics', 'json'], 'export_module')]
        for module, boundary, arguments, action in cases:
            output = io.StringIO()
            with self.subTest(module=module.__name__), patch.object(module.sys, 'argv', arguments), \
                    patch.object(module, action, side_effect=TypeError('unexpected test error')), redirect_stderr(output):
                self.assertEqual(boundary(), 1)
            item, = json.loads(output.getvalue())['diagnostics']
            self.assertEqual((item['stage'], item['code']), ('build', 'pipeline_failed'))


if __name__ == '__main__':
    unittest.main()
