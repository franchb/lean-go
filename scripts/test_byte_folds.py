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
                self.assertEqual(ir['features'], ['byte_fold', 'uint32', 'uint8', 'width_conversion'])
                self.assertIn('Numeric.tick', [f['name'] for f in ir['functions']])
                self.assertEqual(next(t for t in ir['types'] if t['name'] == 'List UInt8')['constructors'][1]['fields'],
                                 ['UInt8', 'List UInt8'])

    def test_record_fold(self):
        source = """structure Pair where
  count : UInt32
  total : UInt32
def see (s : Pair) (b : UInt8) : Pair := ⟨s.count + 1, s.total + b.toUInt32⟩
def run (s : Pair) (bs : List UInt8) := bs.foldl see s
"""
        ir = self.export(source)
        self.assertEqual(ir['features'], ['byte_fold', 'record_fold', 'uint32', 'uint8', 'width_conversion'])
        self.assertEqual(ir['api']['records']['Numeric.Pair'], ['count', 'total'])

    def test_reject_other_accumulators(self):
        for declaration, result in (
            ('structure S where\n  a : UInt32\n  b : UInt16', '⟨s.a + b.toUInt32, s.b⟩'),
            ('structure S where\n  a : UInt32\n  b : UInt32\n  c : UInt32', '⟨s.a + b.toUInt32, s.b, s.c⟩'),
            ('inductive S where | mk (a b : UInt32)', 'match s with | .mk a c => .mk (a + b.toUInt32) c'),
        ):
            source = (declaration + '\ndef step (s : S) (b : UInt8) : S := ' + result
                      + '\ndef run (s : S) (bs : List UInt8) := bs.foldl step s\n')
            with self.subTest(declaration=declaration), self.assertRaises(export_module.ExportError):
                self.export(source)

    def test_reject_different_recurrences(self):
        for source in (
            'def run (s : UInt32) (bs : List UInt32) := bs.foldl UInt32.add s',
            'def run (s : UInt32) (bs : List UInt8) := bs.foldl (fun n b => n + b.toUInt32 + s) s',
            'def tick (s : UInt32) (b : UInt8) := s + b.toUInt32\ndef run (s : UInt32) (bs : List UInt8) := bs.foldl tick (s + 1)',
        ):
            with self.subTest(source=source), self.assertRaises(export_module.ExportError):
                self.export(source + '\n')


if __name__ == '__main__':
    unittest.main()
