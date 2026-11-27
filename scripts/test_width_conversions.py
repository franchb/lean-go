"""Exact mixed-width feature declaration.

The adversarial producer admission tests of this suite are ported to Go
(internal/pipeline/lean_widths_test.go). This test stays because the Go
completion step (internal/pipeline/features.go) declares "width_conversion"
for names such as `UInt32.toUInt32`, `USize.toUInt32` and
`UInt32.toUInt64._boxed`; see the port's report.
"""
import copy
import unittest

import test_bitwise as bitwise


class WidthFeatureTests(unittest.TestCase):
    raw = bitwise.BitwiseFeatureTests.raw
    complete = bitwise.BitwiseFeatureTests.complete

    def converts(self, program, roots):
        return 'width_conversion' in self.complete(program, roots)['features']

    def test_executable_casts_declare_feature_in_every_container(self):
        for name in ('UInt32.toUInt64', 'UInt64.toUInt32', 'UInt16.toUInt32'):
            leaf = dict(op='let', value=dict(kind='primitive', name=name, args=[1]), next=dict(op='return', var=2))
            bodies = [leaf, dict(op='join', join=dict(body=leaf), next=dict(op='jump')),
                      dict(op='join', join=dict(body=dict(op='return')), next=leaf),
                      dict(op='cases', alts=[dict(body=leaf)]),
                      dict(op='let', value=dict(kind='primitive', name='UInt64.land'), next=leaf)]
            for multi in (False, True):
                for body in bodies:
                    program, roots = self.raw(body, multi)
                    self.assertTrue(self.converts(program, roots))
                program, roots = self.raw(dict(op='return'), multi)
                program['functions'].append(dict(name='Example.helper', body=leaf))
                self.assertTrue(self.converts(program, roots))
                for kind in ('call', 'constructor'):
                    body = copy.deepcopy(leaf)
                    body['value']['kind'] = kind
                    program, roots = self.raw(body, multi)
                    program['provenance'] = [dict(name=name, dependencies=[name])]
                    self.assertFalse(self.converts(program, roots))
        for name in ('UInt32.toUInt32', 'UInt64.toUInt64', 'UInt16.toUInt16', 'USize.toUInt32',
                     'UInt32.toUInt64._boxed', 'UInt64.toUInt32.injected', 'Fake.UInt32.toUInt64',
                     'UInt16.toUInt32._boxed', 'Fake.UInt16.toUInt32'):
            leaf['value']['name'] = name
            program, roots = self.raw(leaf)
            self.assertFalse(self.converts(program, roots))


if __name__ == '__main__':
    unittest.main()
