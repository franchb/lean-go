"""Admission tests for a source-witnessed specialization of List.foldl on bytes."""
import os
import unittest

import export_module
import test_uint64_admission as wide


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'requires fresh Lean builds')
class ByteFoldTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export

    def test_direct_fold(self):
        source = """def tick (s : UInt32) (b : UInt8) := s + b.toUInt32
def run (s : UInt32) (bytes : List UInt8) := bytes.foldl tick s
"""
        for modern in (False, True):
            with self.subTest(modern=modern):
                ir = self.export(source, modern=modern)
                self.assertEqual(ir['schema'], 'lean-go-ir/v20')
                self.assertIn('Numeric.tick', [f['name'] for f in ir['functions']])
                self.assertEqual(next(t for t in ir['types'] if t['name'] == 'List UInt8')['constructors'][1]['fields'],
                                 ['UInt8', 'List UInt8'])

    def test_reject_different_recurrences(self):
        for source in (
            'def run (s : UInt32) (bs : List UInt32) := bs.foldl UInt32.add s',
            'def run (s : UInt32) (bs : List UInt8) := bs.foldl (fun n b => n + b.toUInt32 + s) s',
            'def tick (s : UInt32) (b : UInt8) := s + b.toUInt32\ndef run (s : UInt32) (bs : List UInt8) := bs.foldl tick (s + 1)',
        ):
            with self.subTest(source=source), self.assertRaises(export_module.ExportError):
                self.export(source + '\n')
