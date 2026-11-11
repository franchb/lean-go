"""Opt-in fresh Lean module, signature and historical rewrite checks."""
import json
import os
from pathlib import Path
import tempfile
import unittest

import export_module as producer


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean module builds')
class RealModulePositiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='lean-go-positive-')
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.source = self.directory / 'source'
        self.source.mkdir()
        self.root = Path(__file__).resolve().parents[1]

    def write(self, name, source):
        path = self.source / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(source)

    def export(self, module, entry):
        output = self.directory / 'program.json'
        producer.export_module(self.root, self.source, module, entry, output)
        return json.loads(output.read_bytes())

    def modern_sources(self):
        self.write('Modern/Model.lean', '''module
public section
namespace Modern
inductive Signal where
  | yes : Bool → Signal
  | no
@[noinline] def negate (b : Bool) : Bool := !b
end Modern
''')
        self.write('Modern/Transform.lean', '''module
public import Modern.Model
public section
namespace Modern
def choose (flag : Bool) : Signal := .yes (negate flag)
def constant : Signal := .no
def select (left : Signal) (right : Signal) : Signal :=
  match left with
  | .yes _ => right
  | .no => .no
end Modern
''')

    def test_modern_module_local_import_and_bool_rewrite_survive_olean(self):
        self.modern_sources()
        program = self.export('Modern.Transform', 'Modern.choose')
        self.assertEqual(program['module'], 'Modern.Transform')
        self.assertEqual(program['root'], 'Modern.choose')
        self.assertTrue({'Bool', 'Modern.Signal'} <= {item['name'] for item in program['types']})
        self.assertIn('source/Modern/Model.lean', program['inputs'])
        rewrite = dict(source='Bool.not', target='Bool.Internal.not',
                       theorem='Bool.not_eq_internalNot', axioms=['Quot.sound'])
        self.assertIn(rewrite, program['rewrites'])
        records = {item['name']: item for item in program['provenance']}
        self.assertIn('Modern.negate', records)
        self.assertIn(rewrite, records['Modern.negate']['rewrites'])

    def test_nullary_enum_and_two_argument_selector_roots(self):
        self.modern_sources()
        for entry, parameter_count in (('Modern.constant', 0), ('Modern.select', 2)):
            with self.subTest(entry=entry):
                program = self.export('Modern.Transform', entry)
                functions = {item['name']: item for item in program['functions']}
                self.assertEqual(len(functions[entry]['params']), parameter_count)
                self.assertEqual(functions[entry]['result'], 'Modern.Signal')
                self.assertIn('Modern.Signal', {item['name'] for item in program['types']})

    def test_boolean_pattern_match_root(self):
        self.write('Boolean.lean', '''def Boolean.flip (b : Bool) : Bool :=
  match b with
  | true => false
  | false => true
''')
        program = self.export('Boolean', 'Boolean.flip')
        self.assertEqual([item['name'] for item in program['types']], ['Bool'])
        self.assertEqual(program['root'], 'Boolean.flip')

    def test_safe_user_rewrite_rejected_after_local_scope_has_ended(self):
        # This witness is proved by rfl and has no sorry or user axiom. The strict
        # profile still rejects the local rewrite recorded while helper compiled.
        self.write('Historical.lean', '''namespace Historical
@[noinline] def original (b : Bool) : Bool := b
@[noinline] def replacement (b : Bool) : Bool := b
theorem equivalent : original = replacement := rfl
section
attribute [local csimp] equivalent
@[noinline] def helper (b : Bool) : Bool := original b
end
@[noinline] def run (b : Bool) : Bool := helper b
end Historical
''')
        output = self.directory / 'program.json'
        output.write_bytes(b'previous output\n')
        with self.assertRaisesRegex(producer.ExportError,
                                    'Unsupported historical compiler rewrite: Historical.original') as rejected:
            producer.export_module(self.root, self.source, 'Historical', 'Historical.run', output)
        self.assertIn('Historical.equivalent', str(rejected.exception))
        self.assertNotRegex(str(rejected.exception), 'sorry|unknown identifier|unexpected token')
        self.assertEqual(output.read_bytes(), b'previous output\n')

    def test_safe_local_rewrite_in_imported_inline_helper_is_rejected(self):
        self.write('Helper.lean', """namespace Helper
@[noinline] def original (b : Bool) : Bool := b
@[noinline] def replacement (b : Bool) : Bool := b
theorem equivalent : original = replacement := rfl
section
attribute [local csimp] equivalent
@[inline] def helper (b : Bool) : Bool := original b
end
end Helper
""")
        self.write('Caller.lean', """import Helper
namespace Caller
@[noinline] def run (b : Bool) : Bool := Helper.helper b
end Caller
""")
        output = self.directory / 'program.json'
        output.write_bytes(b'previous output\n')
        with self.assertRaisesRegex(producer.ExportError,
                                    'Unsupported historical compiler rewrite: Helper.original') as rejected:
            producer.export_module(self.root, self.source, 'Caller', 'Caller.run', output)
        self.assertIn('Helper.equivalent', str(rejected.exception))
        self.assertNotRegex(str(rejected.exception), 'sorry|unknown identifier|unexpected token')
        self.assertEqual(output.read_bytes(), b'previous output\n')


if __name__ == '__main__':
    unittest.main()
