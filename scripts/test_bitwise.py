"""Exact primitive names for the bitwise feature declaration.

Everything else of this suite is ported to Go (internal/pipeline/
numeric_features_test.go, lean_bitwise_test.go, integration/
bitwise_native_test.go). This test stays because the Go completion step
(internal/pipeline/features.go) declares a family by the operation suffix
alone, so `Int16.complement` declares "bitwise" there; see the port's report.
"""
import json
import unittest

import export_module as producer

OPERATIONS = ('land', 'lor', 'xor', 'complement', 'shiftRight')


class BitwiseFeatureTests(unittest.TestCase):
    def raw(self, body, multi=False):
        program = dict(module='Example', functions=[dict(name='Example.run', body=body)],
                       types=[], claims=[], rewrites=[], provenance=[], api={'records': {}})
        roots = ['Example.run', 'Example.second'] if multi else ['Example.run']
        program['roots'] = roots
        return program, roots

    def complete(self, program, roots):
        return producer.complete_program(json.dumps(program).encode(), 'Example', roots, {}, producer.selected_compiler())

    def bitwise(self, program, roots):
        result = self.complete(program, roots)
        self.assertEqual((result['schema'], result['profile']), (producer.SCHEMA, producer.PROFILE))
        return 'bitwise' in result['features']

    def test_exact_names_and_all_operations(self):
        for width in (8, 16, 32, 64):
            for op in OPERATIONS + ('shiftLeft',):
                body = dict(op='let', value=dict(kind='primitive', name=f'UInt{width}.{op}', args=[]),
                            next=dict(op='return', var=1))
                program, roots = self.raw(body)
                self.assertTrue(self.bitwise(program, roots))
        for name in ('UInt32.land._boxed', 'UInt16.and', 'Int16.complement', 'UInt32.add', 'UInt32.div', 'UInt32.toUInt64'):
            body['value']['name'] = name
            program, roots = self.raw(body)
            self.assertFalse(self.bitwise(program, roots))


if __name__ == '__main__':
    unittest.main()
