import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import export_module as producer


class ModuleExportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / 'source'
        self.source.mkdir()
        (self.source / 'Example.lean').write_text('def identity (x : UInt32) := x\n')
        for name in producer.PRODUCER_INPUTS:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('trusted:' + name)
        self.output = self.root / 'ir.json'

    def test_python_minor_pin(self):
        producer.check_python((3, 15, 0))
        for version in ((3, 14, 9), (3, 16, 0), (2, 15, 0)):
            with self.subTest(version=version), self.assertRaisesRegex(producer.ExportError, 'Python 3.15'):
                producer.check_python(version)

    def test_ascii_names_only(self):
        producer.validate_name("Example.value'")
        for name in ('../Example', 'A; import X', 'A\nB', 'A..B', 'A/B', 'A"', '', 'Ä', 'A.1'):
            with self.subTest(name=name), self.assertRaises(producer.ExportError):
                producer.validate_name(name)

    def test_snapshot_ignores_stale_build_products(self):
        cache = self.source / '.lake'
        cache.mkdir()
        (cache / 'Stale.lean').write_text('def stale := 0')
        (self.source / 'lakefile.lean').write_text('unsafe script never run')
        (self.source / 'Example.olean').write_bytes(b'stale compiled code')
        snapshot = producer.source_snapshot(self.source)
        self.assertEqual(set(snapshot), {'Example'})
        self.assertEqual(snapshot['Example'][0], 'Example.lean')

    def test_snapshot_rejects_reserved_symlinked_and_ambiguous_modules(self):
        for name in ('Lean.lean', 'Std.lean', 'Init.lean', 'LeanGo.lean', 'Bad-Name.lean'):
            path = self.source / name
            path.write_text('')
            with self.subTest(name=name), self.assertRaises(producer.ExportError):
                producer.source_snapshot(self.source)
            path.unlink()
        link = self.source / 'Link.lean'
        link.symlink_to(self.source / 'Example.lean')
        with self.assertRaisesRegex(producer.ExportError, 'regular'):
            producer.source_snapshot(self.source)
        link.unlink()
        link = self.source / 'linked'
        link.symlink_to(self.source, target_is_directory=True)
        with self.assertRaisesRegex(producer.ExportError, 'symlink'):
            producer.source_snapshot(self.source)
        link.unlink()
        (self.source / 'A').mkdir()
        (self.source / 'A/B.lean').write_text('')
        (self.source / 'A.B.lean').write_text('')
        with self.assertRaisesRegex(producer.ExportError, 'ambiguous'):
            producer.source_snapshot(self.source)

    def test_snapshot_resource_limits(self):
        with mock.patch.object(producer, 'MAX_FILES', 0), self.assertRaisesRegex(producer.ExportError, 'limit'):
            producer.source_snapshot(self.source)
        with mock.patch.object(producer, 'MAX_SOURCE_BYTES', 1), self.assertRaisesRegex(producer.ExportError, 'limit'):
            producer.source_snapshot(self.source)

    def test_instrumentation_preserves_valid_headers(self):
        cases = [
            ('/- license /- nested -/ -/\nimport Std\n-- explanation\ndef f := 1\n', ['Std'], False),
            ('module\nprelude\npublic meta import Lean\npublic import Std\npublic def f := 1\n', ['Lean', 'Std'], True),
            ('-- license\ndef f := 1\n', [], False),
        ]
        for source, imports, modern in cases:
            with self.subTest(source=source):
                result, actual = producer.instrument_source(source.encode())
                self.assertEqual(imports, actual)
                text = result.decode()
                expected = ('public meta import' if modern else 'import') + ' LeanGo.Module.Audit'
                self.assertIn(expected, text)
                self.assertLess(text.index(expected), text.index('def f'))
                if imports:
                    self.assertLess(text.index('import ' + imports[-1]), text.index(expected))
                if modern:
                    self.assertTrue(text.startswith('module\nprelude\n'))
        with self.assertRaisesRegex(producer.ExportError, 'unterminated'):
            producer.instrument_source(b'/- broken')

    def test_header_scan_stops_at_first_source_command(self):
        # The header reader must not tokenize or copy each suffix of a large body.
        body = b'def run := "body"\n/- body parsing belongs to Lean'
        result, imports = producer.instrument_source(body)
        self.assertEqual(imports, [])
        self.assertTrue(result.endswith(body))

    def test_freshness_tracks_original_source_and_trusted_producer(self):
        self.assertIn('producer/LeanGo/Module/Records.lean', producer.PRODUCER_INPUTS)
        self.assertIn('producer/LeanGo/Module/Reduced.lean', producer.PRODUCER_INPUTS)
        for name in ('Local', 'LocalSource', 'LocalObserve'):
            self.assertIn(f'producer/LeanGo/Module/{name}.lean', producer.PRODUCER_INPUTS)
        program = {'inputs': producer.input_digests(producer.source_snapshot(self.source),
                                                   producer.producer_snapshot(self.root))}
        self.assertIn('source/Example.lean', program['inputs'])
        self.assertTrue(all(not Path(key).is_absolute() for key in program['inputs']))
        producer.verify_inputs(self.root, self.source, program)
        for path in [self.source / 'Example.lean', *(self.root / name for name in producer.PRODUCER_INPUTS)]:
            old = path.read_bytes()
            path.write_bytes(old + b'changed')
            with self.assertRaisesRegex(producer.ExportError, 'changed'):
                producer.verify_inputs(self.root, self.source, program)
            path.write_bytes(old)
        (self.source / 'Added.lean').write_text('')
        with self.assertRaisesRegex(producer.ExportError, 'changed'):
            producer.verify_inputs(self.root, self.source, program)

    def fake_run(self, command, workspace, env):
        if command[-1] == '--version':
            (workspace / 'lake-manifest.json').write_text('{"packages":[]}')
            return f'Lean (version 4.35.0, x86_64, commit {producer.COMMIT}, Release)'
        if command[-1] == '--print-prefix':
            return str(self.root / 'stdlib')
        if 'build' in command:
            self.assertEqual(command[1:], ['build', 'LeanGo.Module.Export', 'Example'])
            text = (workspace / 'Example.lean').read_text()
            self.assertIn('import LeanGo.Module.Audit', text)
            self.assertIn('set_option compiler.postponeCompile false', text)
        else:
            self.assertIn('import Example', (workspace / 'LeanGo/Module/Harness.lean').read_text())
            (workspace / 'ir.json').write_text(json.dumps(dict(module='Example', root='identity',
                types=[], functions=[], claims=[], rewrites=[], provenance=[])))
        return ''

    def export(self):
        with mock.patch.object(producer.shutil, 'which', side_effect=lambda name: name):
            producer.export_module(self.root, self.source, 'Example', 'identity', self.output)

    def test_fresh_isolated_build_and_metadata(self):
        with mock.patch.object(producer, 'run', side_effect=self.fake_run):
            self.export()
        program = json.loads(self.output.read_bytes())
        self.assertEqual(program['schema'], 'lean-go-ir/v4')
        self.assertEqual(program['profile'], 'pure-module/v3')
        self.assertIn('producer/LeanGo/Module/Primitives.lean', program['inputs'])
        self.assertEqual(program['root'], 'identity')
        self.assertEqual(program['inputs']['source/Example.lean'],
                         hashlib.sha256((self.source / 'Example.lean').read_bytes()).hexdigest())

    def test_missing_import_fails_before_build_and_preserves_output(self):
        (self.source / 'Example.lean').write_text('import Missing\ndef identity := 1\n')
        self.output.write_text('original')
        with mock.patch.object(producer, 'run', side_effect=self.fake_run) as run:
            with self.assertRaisesRegex(producer.ExportError, 'missing local import'):
                self.export()
        self.assertEqual(run.call_count, 2)
        self.assertEqual(self.output.read_text(), 'original')

    def test_failed_process_and_source_conflict_preserve_outputs(self):
        self.output.write_text('original')
        with mock.patch.object(producer, 'run', side_effect=producer.ExportError('failed')):
            with self.assertRaisesRegex(producer.ExportError, 'failed'):
                self.export()
        self.assertEqual(self.output.read_text(), 'original')
        self.output = self.source / 'Example.lean'
        with mock.patch.object(producer, 'run') as run:
            with self.assertRaisesRegex(producer.ExportError, 'conflicts'):
                self.export()
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
