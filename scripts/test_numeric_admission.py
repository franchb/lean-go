"""Opt-in fresh compiler checks for the narrowly audited UInt32 boundary."""
import json
import os
from pathlib import Path
import tempfile
import unittest

import export_module as producer
from test_record_admission import ELABORATION_FAILURE


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class NumericAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.directory = Path(self.enterContext(
            tempfile.TemporaryDirectory(prefix='lean-go-numeric-admission-')))
        self.source = self.directory / 'source'
        self.source.mkdir()
        self.output = self.directory / 'program.json'
        self.root = Path(__file__).resolve().parents[1]

    def export(self, body, *, modern=False):
        prefix = 'module\npublic section\n' if modern else ''
        (self.source / 'Numeric.lean').write_text(prefix + 'namespace Numeric\n' + body + '\nend Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', 'Numeric.run', self.output)
        return json.loads(self.output.read_bytes())

    def test_literals_primitives_and_decidable_bool_lowering(self):
        program = self.export('''inductive Result where
  | mk (arithmetic : UInt32) (eq lt le : Bool)
def run (a b : UInt32) : Result :=
  .mk (((a + 4294967297) - b) * 2) (decide (a = b)) (decide (a < b)) (decide (a ≤ b))
''', modern=True)
        values = []
        for function in program['functions']:
            code = function['body']
            while code['op'] == 'let':
                values.append(code['value'])
                code = code['next']
        self.assertIn(dict(kind='uint32', name='1', args=[]), values)
        names = {value['name'] for value in values if value['kind'] == 'primitive'}
        self.assertEqual(names, {'UInt32.add', 'UInt32.sub', 'UInt32.shiftLeft',
                                 'UInt32.decEq', 'UInt32.decLt', 'UInt32.decLe'})
        functions = {item['name'] for item in program['functions']}
        self.assertFalse(names & functions)
        bool_type = next(item for item in program['types'] if item['name'] == 'Bool')
        self.assertEqual([(item['name'], item['tag']) for item in bool_type['constructors']],
                         [('Bool.false', 0), ('Bool.true', 1)])

    def test_numeric_condition_and_order_package_equality(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                program = self.export('''def run (a b : UInt32) : UInt32 :=
  if a == b then 4294967295 else if a < b then a + b else a - b
''', modern=modern)
                self.assertEqual(program['functions'][0]['result'], 'UInt32')
                self.assertIn('Bool', {item['name'] for item in program['types']})
                records = {item['name'] for item in program['provenance']}
                self.assertIn('instLinearOrderPackageUInt32', records)
                self.assertFalse(any(name.startswith('_private.Init.Data.UInt.Package.')
                                     for name in records))

    def test_custom_operator_instance_follows_its_audited_implementation(self):
        program = self.export('''instance : Add UInt32 := ⟨fun a b => UInt32.sub a b⟩
def run (a b : UInt32) : UInt32 := a + b
''')
        function = next(item for item in program['functions'] if item['name'] == 'Numeric.run')
        self.assertEqual(function['body']['value']['kind'], 'primitive')
        self.assertEqual(function['body']['value']['name'], 'UInt32.sub')

    def test_unsupported_numeric_sources_preserve_output_even_when_folded(self):
        cases = {
            'other_width': 'def run (a : UInt16) : UInt16 := a\n',
            'custom_foreign_instance':
                '@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32 → UInt32\n'
                'instance : Add UInt32 := ⟨foreign⟩\n'
                'def run (a b : UInt32) : UInt32 := a + b\n',
            'division': 'def run (a b : UInt32) : UInt32 := a / b\n',
            'folded_division': 'def run (a : UInt32) : UInt32 := a / 1\n',
            'remainder': 'def run (a b : UInt32) : UInt32 := a % b\n',
            'foreign': '@[extern "lean_go_untrusted"] opaque foreign : UInt32 → UInt32\n'
                       'def run (a : UInt32) : UInt32 := foreign a\n',
            'axiom': 'axiom forged : UInt32\ndef run : UInt32 := forged\n',
        }
        for name, body in cases.items():
            with self.subTest(case=name):
                self.output.write_bytes(b'previous output\n')
                with self.assertRaisesRegex(producer.ExportError, 'extern|axiom|opaque|provenance|Unsupported|not supported by code generator') as rejected:
                    self.export(body)
                self.assertNotRegex(str(rejected.exception), ELABORATION_FAILURE)
                self.assertEqual(self.output.read_bytes(), b'previous output\n')

    def test_context_sensitive_primitive_and_wrapper_replacements_are_rejected(self):
        addition = '''@[noinline] def replacement (a b : UInt32) := UInt32.add a b
theorem replacement_eq : UInt32.add = replacement := rfl
'''
        equality = '''@[noinline] def replacement (a b : UInt32) : Decidable (a = b) := UInt32.decEq a b
theorem replacement_eq : UInt32.decEq = replacement := rfl
'''
        wrapper = '''def replacement : Add UInt32 := instAddUInt32
theorem replacement_eq : instAddUInt32 = replacement := rfl
'''
        cases = {
            'local_primitive': addition + 'section\nattribute [local csimp] replacement_eq\n'
                               'def run (a b : UInt32) := a + b\nend\n',
            'scoped_primitive': addition + 'namespace Scope\nattribute [scoped csimp] replacement_eq\nend Scope\n'
                                'open scoped Scope\ndef run (a b : UInt32) := a + b\n',
            'late_primitive': addition + 'def run (a b : UInt32) := a + b\n'
                              'attribute [csimp] replacement_eq\n',
            'local_order_package_dependency': equality + 'section\nattribute [local csimp] replacement_eq\n'
                                              'def run (a b : UInt32) := a == b\nend\n',
            'late_order_package_dependency': equality + 'def run (a b : UInt32) := a == b\n'
                                             'attribute [csimp] replacement_eq\n',
            'local_notation_wrapper': wrapper + 'section\nattribute [local csimp] replacement_eq\n'
                                     'def run (a b : UInt32) := a + b\nend\n',
            'late_pristine_rewrite_dependency':
                '@[noinline] def replacement := @Nat.recCompiled\n'
                'theorem replacement_eq : @Nat.recCompiled = @replacement := rfl\n'
                'def run (a b : UInt32) := a == b\n'
                'attribute [csimp] replacement_eq\n',
            'late_extern': 'def run (a b : UInt32) := a + b\n'
                           'attribute [extern "lean_go_untrusted"] UInt32.add\n',
            'late_implemented_by': addition + 'def run (a b : UInt32) := a + b\n'
                                   'attribute [implemented_by replacement] UInt32.add\n',
        }
        for name, body in cases.items():
            with self.subTest(case=name):
                self.output.write_bytes(b'previous output\n')
                with self.assertRaisesRegex(producer.ExportError, 'rewrite|extern|implemented_by|replacement|repeated compilation') as rejected:
                    self.export(body)
                self.assertNotRegex(str(rejected.exception), ELABORATION_FAILURE)
                self.assertEqual(self.output.read_bytes(), b'previous output\n')


if __name__ == '__main__':
    unittest.main()
