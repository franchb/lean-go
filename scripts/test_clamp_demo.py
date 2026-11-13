#!/usr/bin/env python3
"""Reject stale demo artifacts and failed or incorrect Go-only consumers."""
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import check_clamp_demo as demo
from export import ExportError


class DemoVerificationTests(unittest.TestCase):
    demo = demo

    def test_stale_missing_and_extra_artifacts_are_rejected(self):
        tracked = self.demo.ROOT / self.demo.DEMO
        package = self.demo.DEMO.name
        names = [package + '/' + name for name in
                 ['api.go', 'control.go', 'runtime.go', 'schema.go', 'manifest.json']]
        names.extend([package + '.ir.json', 'proof-evidence.json'])
        self.assertEqual(set(self.demo.artifacts(tracked)), set(names))
        for name in names:
            for operation in ['change', 'remove']:
                with self.subTest(name=name, operation=operation), tempfile.TemporaryDirectory() as directory:
                    candidate = Path(directory).resolve() / 'candidate'
                    shutil.copytree(tracked, candidate)
                    target = candidate / name
                    if operation == 'change':
                        target.write_bytes(target.read_bytes() + b'stale')
                    else:
                        target.unlink()
                    with self.assertRaises((ExportError, OSError)):
                        self.demo.compare_artifacts(tracked, candidate)
        with tempfile.TemporaryDirectory() as directory:
            candidate = Path(directory).resolve() / 'candidate'
            shutil.copytree(tracked, candidate)
            self.demo.compare_artifacts(tracked, candidate)
            (candidate / package / 'unexpected.go').write_bytes(b'package extracted')
            with self.assertRaisesRegex(ExportError, 'unexpected.go'):
                self.demo.compare_artifacts(tracked, candidate)

    def test_failed_go_tests_stop_execution(self):
        with patch.object(self.demo, 'run', side_effect=ExportError('test failure')) as run:
            with self.assertRaisesRegex(ExportError, 'test failure'):
                self.demo.run_go_demo(Path('project'), Path('/go/bin/go'), {}, Path('workspace'))
            self.assertEqual(run.call_count, 1)

    def test_wrong_output_is_rejected(self):
        with patch.object(self.demo, 'run', side_effect=['ok', 'wrong output']):
            with self.assertRaisesRegex(ExportError, 'unexpected output'):
                self.demo.run_go_demo(Path('project'), Path('/go/bin/go'), {}, Path('workspace'))

    def test_failed_generation_stops_before_proof_or_go_checks(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(self.demo, 'pinned_go', return_value=Path('/go/bin/go')), \
                    patch.object(self.demo, 'run', side_effect=ExportError('generation failure')) as run, \
                    patch.object(self.demo, 'run_go_demo') as consumer:
                with self.assertRaisesRegex(ExportError, 'generation failure'):
                    self.demo.verify_demo(Path('/tools/lean-go'), Path(directory).resolve(), {})
                self.assertEqual(run.call_count, 1)
                consumer.assert_not_called()


if __name__ == '__main__':
    unittest.main()
