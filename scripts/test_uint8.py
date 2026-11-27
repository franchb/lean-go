"""Exact byte feature declaration.

The fresh source admission tests of this suite are ported to Go
(internal/pipeline/lean_bytes_test.go). This test stays because the Go
completion step (internal/pipeline/features.go) declares "width_conversion"
for `UInt8.toUInt8` and `USize.toUInt8`; see the port's report.
"""
import unittest
import test_bitwise as bitwise

OPS = ('add', 'sub', 'mul', 'decEq', 'decLt', 'decLe', 'land', 'lor', 'xor', 'complement', 'shiftLeft', 'shiftRight')
CASTS = (('UInt8.toUInt32', 'UInt8', 'UInt32'), ('UInt32.toUInt8', 'UInt32', 'UInt8'))

class ByteFeatureTests(unittest.TestCase):
    raw = bitwise.BitwiseFeatureTests.raw
    complete = bitwise.BitwiseFeatureTests.complete

    def features(self, program, roots):
        return self.complete(program, roots)['features']

    def test_executable_byte_types_literals_and_primitives_declare_features(self):
        for multi in (False, True):
            for name in (*(f'UInt8.{op}' for op in OPS), *(cast[0] for cast in CASTS)):
                family = ('width_conversion' if name in {cast[0] for cast in CASTS} else
                          'bitwise' if name.split('.')[1] in bitwise.OPERATIONS + ('shiftLeft',) else None)
                leaf = dict(op='let', bind=dict(type='UInt32'), value=dict(kind='primitive', name=name), next=dict(op='return'))
                for body in (leaf, dict(op='join', join=dict(body=leaf), next=dict(op='jump')), dict(op='cases', alts=[dict(body=leaf)])):
                    program, roots = self.raw(body, multi)
                    features = self.features(program, roots)
                    self.assertIn('uint32', features)
                    self.assertEqual(family in features, family is not None, (name, features))
            for position in ('param', 'result', 'field', 'bind', 'join', 'alt', 'literal'):
                program, roots = self.raw(dict(op='return'), multi)
                f = program['functions'][0]
                if position == 'param': f['params'] = [dict(type='UInt8')]
                if position == 'result': f['result'] = 'UInt8'
                if position == 'field': program['types'] = [dict(name='Packet', constructors=[dict(fields=['UInt8'])])]
                if position == 'bind': f['body'] = dict(op='let', bind=dict(type='UInt8'), value=dict(kind='call', name='helper'), next=dict(op='return'))
                if position == 'join': f['body'] = dict(op='join', join=dict(params=[dict(type='UInt8')], body=dict(op='return')), next=dict(op='jump'))
                if position == 'alt': f['body'] = dict(op='cases', alts=[dict(params=[dict(type='UInt8')], body=dict(op='return'))])
                if position == 'literal': f['body'] = dict(op='let', value=dict(kind='uint8', name='255'), next=dict(op='return'))
                self.assertIn('uint8', self.features(program, roots), position)
            program, roots = self.raw(dict(op='return'), multi)
            program['provenance'] = [dict(name='UInt8.add', dependencies=['UInt8', 'UInt32.toUInt8'])]
            self.assertEqual(self.features(program, roots), [])
            for name in ('UInt8.add._boxed', 'UInt8.toUInt8', 'USize.toUInt8', 'UInt16.add', 'Fake.UInt8.add'):
                program, roots = self.raw(dict(op='let', value=dict(kind='primitive', name=name), next=dict(op='return')), multi)
                self.assertEqual(self.features(program, roots), [])


if __name__ == '__main__':
    unittest.main()
