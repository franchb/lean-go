"""Ordinary UInt16 source admission and executable bitwise and cast semantics."""
import json
import os
import unittest

import export_module as producer
import test_uint64_admission as wide


LEAVES = {'UInt8.toUInt16', 'UInt16.toUInt8', 'UInt16.lor',
          'UInt16.shiftLeft', 'UInt16.shiftRight'}


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class UInt16AdmissionTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export
    assert_rejected = wide.UInt64AdmissionTests.assert_rejected
    imported_attribute_probe = staticmethod(wide.UInt64AdmissionTests.imported_attribute_probe)

    def test_nested_records_literals_and_exact_operators(self):
        source = '''structure Words where
  first : UInt16
  second : UInt16
structure Packet where
  words : Words
  low : UInt8
  limit : UInt16
@[noinline] def combine (a b : UInt8) := (a.toUInt16 <<< 8) ||| b.toUInt16
def run (flag : Bool) (a b : UInt8) (p : Packet) : Packet :=
  let word := if flag then combine a b else p.words.first
  ⟨⟨word, 65537⟩, (word >>> p.words.second).toUInt8, 65535⟩
'''
        for modern in (False, True):
            with self.subTest(modern=modern):
                program = self.export(source, modern=modern)
                self.assertLessEqual({'bitwise', 'join', 'uint16', 'uint8', 'width_conversion'}, set(program['features']))
                self.assertEqual(producer.emitted_primitives(program), LEAVES)
                fields = {t['name']: t['constructors'][0]['fields'] for t in program['types']}
                self.assertNotIn('UInt16', fields)
                self.assertEqual(fields['Numeric.Words'], ['UInt16', 'UInt16'])
                self.assertEqual(fields['Numeric.Packet'], ['Numeric.Words', 'UInt8', 'UInt16'])
                nodes = list(producer.executable_nodes(program))
                self.assertTrue(any(n['op'] == 'join' and [p['type'] for p in n['join']['params']] == ['UInt16']
                                    for n in nodes))
                literals = {n['value']['name'] for n in nodes if n['op'] == 'let' and n['value']['kind'] == 'uint16'}
                self.assertTrue({'1', '8', '65535'} <= literals)

    def test_scalar_identity_and_dead_provenance(self):
        identity = self.export('def run (w : UInt16) := w\n')
        self.assertEqual(identity['features'], ['uint16'])
        self.assertEqual(identity['functions'][0]['body']['op'], 'return')
        for typ, unused in [('UInt8', 'a.toUInt16'), ('UInt32', 'a.toUInt8.toUInt16')]:
            program = self.export(f'def run (a : {typ}) :=\n  let _unused := {unused}\n  a\n')
            self.assertFalse(producer.emitted_primitives(program))
            self.assertIn('UInt8.toUInt16', {p['name'] for p in program['provenance']})
            self.assertEqual(program['features'], [typ.lower()])
        (self.source / 'Numeric.lean').write_text(
            'namespace Numeric\ndef run (a b : UInt8) := (a + b).toUInt16\ndef second (w : UInt16) := w\nend Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', ['Numeric.run', 'Numeric.second'], self.output)
        multi = json.loads(self.output.read_bytes())
        self.assertEqual(multi['features'], ['uint16', 'uint8', 'width_conversion'])
        self.assertEqual(producer.emitted_primitives(multi), {'UInt8.add', 'UInt8.toUInt16'})

    def test_widening_and_erased_features(self):
        source = 'def run (word : UInt16) : UInt32 := word.toUInt32'
        program = self.export(source)
        self.assertEqual(program['features'], ['uint16', 'uint32', 'width_conversion'])
        function, = program['functions']
        self.assertEqual([p['type'] for p in function['params']], ['UInt16'])
        self.assertEqual(function['result'], 'UInt32')
        self.assertEqual(function['body']['value'], {
            'kind': 'primitive', 'name': 'UInt16.toUInt32', 'args': [function['params'][0]['id']]})
        erased = self.export('def run (word : UInt16) :=\n  let _unused := word.toUInt32\n  word')
        self.assertEqual(erased['features'], ['uint16'])
        self.assertFalse(producer.emitted_primitives(erased))
        self.assertIn('UInt16.toUInt32', {p['name'] for p in erased['provenance']})
        (self.source / 'Numeric.lean').write_text(
            'module\npublic section\nnamespace Numeric\n' + source +
            '\ndef second (word : UInt16) := word.toUInt8\nend Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', ['Numeric.run', 'Numeric.second'], self.output)
        multi = json.loads(self.output.read_bytes())
        self.assertEqual(multi['features'], ['uint16', 'uint32', 'uint8', 'width_conversion'])
        self.assertEqual(producer.emitted_primitives(multi), {'UInt16.toUInt32', 'UInt16.toUInt8'})

    def test_xor_exact_notation_and_erased_features(self):
        for modern, expression, source_name in (
                (False, 'UInt16.xor a b', 'UInt16.xor'),
                (True, 'a ^^^ b', 'instXorOpUInt16')):
            with self.subTest(expression=expression):
                program = self.export(f'def run (a b : UInt16) := {expression}', modern=modern)
                self.assertEqual(program['features'], ['bitwise', 'uint16'])
                function, = program['functions']
                self.assertEqual([p['type'] for p in function['params']], ['UInt16', 'UInt16'])
                self.assertEqual(function['result'], 'UInt16')
                self.assertEqual(function['body']['value'], {
                    'kind': 'primitive', 'name': 'UInt16.xor', 'args': [p['id'] for p in function['params']]})
                self.assertEqual(program['rewrites'], [])
                self.assertIn(source_name, {p['name'] for p in program['provenance']})
                erased = self.export(f'def run (a b : UInt16) :=\n  let _unused := {expression}\n  a', modern=modern)
                self.assertEqual(erased['features'], ['uint16'])
                self.assertFalse(producer.emitted_primitives(erased))
                self.assertIn(source_name, {p['name'] for p in erased['provenance']})

    def test_xor_multi_root_declares_both_families(self):
        (self.source / 'Numeric.lean').write_text(
            'namespace Numeric\ndef run (a b : UInt16) := a ^^^ b\n'
            'def second (a : UInt16) := a.toUInt32\nend Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', ['Numeric.run', 'Numeric.second'], self.output)
        program = json.loads(self.output.read_bytes())
        self.assertEqual(program['features'], ['bitwise', 'uint16', 'uint32', 'width_conversion'])
        self.assertEqual(program['roots'], ['Numeric.run', 'Numeric.second'])
        self.assertEqual(producer.emitted_primitives(program), {'UInt16.xor', 'UInt16.toUInt32'})

    def test_xor_primitive_and_notation_rewrite_history(self):
        wide.check_primitive_history(self, [('UInt16.xor', 'UInt16', False)], 'bitwise')
        prelude = ('def replacement : XorOp UInt16 := instXorOpUInt16\n'
                   'theorem replacement_eq : instXorOpUInt16 = replacement := rfl\n')
        call = 'def run (a b : UInt16) := a ^^^ b\n'
        erased = 'def run (a b : UInt16) :=\n  let _unused := a ^^^ b\n  a\n'
        for source in wide.rewrite_sources(prelude, call, erased):
            self.assert_rejected(source, 'rewrite|replacement|repeated compilation')

    def test_xor_erased_notation_audits_logical_helper(self):
        probe = self.imported_attribute_probe('externAttr', 'BitVec.instXorOp',
                                             '{ entries := [.standard `all "untrusted"] }')
        self.assert_rejected('def run (a b : UInt16) :=\n  let _unused := a ^^^ b\n  a',
                             'Numeric source extern changed', probe=probe)

    def test_widening_rewrite_history(self):
        wide.check_primitive_history(self, [('UInt16.toUInt32', 'UInt16', True)], 'width_conversion')

    def test_reject_unadmitted_widths_operators_and_alias(self):
        sources = [
            'def run (a : UInt16) :=\n  let _unused := a * a\n  a',
            'def run (a : UInt16) := a &&& a', 'def run (a : UInt16) := ~~~a',
            'def run (a : UInt16) := a / a', 'def run (a : UInt16) := a % a',
            'def run (a b : UInt16) := decide (a < b)', 'def run (a : UInt16) := a.toUInt64',
            'def run (a : UInt64) := a.toUInt16', 'def run (a : UInt16) := a.toNat',
            'abbrev Half := UInt16\ndef run (a : Half) := a',
        ]
        for source in sources:
            with self.subTest(source=source):
                self.assert_rejected(source, 'extern|opaque|Unsupported|unsupported|not an inductive type')

    def test_trusted_literal_and_logical_helper_attribute_changes_reject(self):
        for helper, body in [('UInt16.ofNat', 'def run : UInt16 := 65535'),
                             ('UInt16.toNat', 'def run (w : UInt16) := w.toUInt8'),
                             ('UInt16.toNat', 'def run (w : UInt16) := w.toUInt32')]:
            probe = self.imported_attribute_probe('externAttr', helper, '{ entries := [.standard `all "untrusted"] }')
            self.assert_rejected(body, 'Numeric source extern changed', probe=probe)

    def test_scoped_rewrites_of_erased_operations_reject(self):
        source = '''@[noinline] def replacement (w c : UInt16) := UInt16.shiftLeft w c
theorem replacement_eq : UInt16.shiftLeft = replacement := rfl
namespace Scope
attribute [scoped csimp] replacement_eq
end Scope
open scoped Scope
def run (a b : UInt8) :=
  let _unused := a.toUInt16 <<< b.toUInt16
  a
'''
        self.assert_rejected(source, 'rewrite|replacement|repeated compilation')

    def test_native_bitwise_operations_and_byte_casts(self):
        compiler = producer.selected_compiler()
        env = dict(os.environ, ELAN_TOOLCHAIN=compiler.toolchain, CGO_ENABLED='0')
        compiler.check_version(producer.run(['lean', '--version'], self.directory, env))
        (self.directory / 'lean-toolchain').write_bytes(compiler.pin_file)
        (self.directory / 'lakefile.toml').write_text(
            'name = "word16_reference"\nversion = "0.0.0"\n[[lean_exe]]\nname = "reference"\nroot = "Reference"\n')
        (self.directory / 'Reference.lean').write_text('''def main (args : List String) : IO Unit := do
  for arg in args do
    let n := arg.toNat!
    let w := (n / 65536).toUInt16
    let c := n.toUInt16
    IO.println s!"{w <<< c} {w >>> c} {w ||| c} {w ^^^ c} {w.toUInt8} {c.toUInt8.toUInt16}"
''')
        producer.run(['lake', 'build', 'reference'], self.directory, env)
        words = [0, 1, 255, 256, 0x5555, 0x8001, 0xaaaa, 65535]
        counts = [0, 1, 7, 8, 15, 16, 17, 255, 256, 0x5555, 0xaaaa, 65535]
        pairs = [(w, c) for w in words for c in counts]
        exe = self.directory / '.lake/build/bin' / ('reference.exe' if os.name == 'nt' else 'reference')
        output = producer.run([str(exe), *(str((w << 16) + c) for w, c in pairs)], self.directory, env)
        values = [list(map(int, line.split())) for line in output.splitlines()]
        self.assertEqual(values, [[(w << (c % 16)) & 65535, w >> (c % 16), w | c, w ^ c, w & 255, c & 255]
                                  for w, c in pairs])


if __name__ == '__main__':
    unittest.main()
