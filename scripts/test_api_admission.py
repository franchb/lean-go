"""Opt-in fresh Lean checks for source API names and unchanged type admission."""
import os
import unittest

import test_record_admission as records


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1',
                     'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class APIAdmissionTests(unittest.TestCase):
    # Reuse the fresh-source fixture without inheriting its unrelated tests.
    setUp = records.RecordAdmissionTests.setUp
    export = records.RecordAdmissionTests.export

    def test_named_nested_inherited_fields_and_enums(self):
        program = self.export('''structure Base where
  first : UInt32
  second : UInt32
structure Nested where
  base : Base
structure Child extends Base where
  nested : Nested
  enabled : Bool
inductive Mode where
  | ready
  | done
structure Envelope where
  child : Child
  mode : Mode
def run (x : Envelope) : Envelope := x
''')
        self.assertEqual(program['schema'], 'lean-go-ir/v5')
        self.assertEqual(program['profile'], 'pure-module/v3')
        self.assertEqual(program['api'], {'records': {
            'Records.Base': ['first', 'second'],
            'Records.Nested': ['base'],
            'Records.Child': ['toBase', 'nested', 'enabled'],
            'Records.Envelope': ['child', 'mode'],
        }})
        types = {item['name']: item for item in program['types']}
        self.assertEqual(types['Records.Child']['constructors'], [dict(
            name='Records.Child.mk', tag=0,
            fields=['Records.Base', 'Records.Nested', 'Bool'])])
        self.assertEqual(types['Records.Base']['constructors'], [dict(
            name='Records.Base.mk', tag=0, fields=['UInt32', 'UInt32'])])
        self.assertEqual({item['name'] for item in types['Records.Mode']['constructors']},
                         {'Records.Mode.ready', 'Records.Mode.done'})
        self.assertNotIn('Bool', program['api']['records'])

    def test_numeric_boolean_recursive_sum_keeps_original_admission(self):
        program = self.export('''inductive Tree where
  | leaf (value : UInt32) (flag : Bool)
  | branch (left right : Tree)
@[noinline] def run (x : Tree) : Tree :=
  match x with
  | .leaf value flag => .leaf (value + 1) (!flag)
  | .branch left right => .branch (run left) (run right)
''', modern=True)
        self.assertEqual(program['api'], {'records': {}})
        types = {item['name']: item for item in program['types']}
        self.assertEqual(set(types), {'Records.Tree', 'Bool'})
        self.assertEqual(types['Records.Tree']['constructors'], [
            dict(name='Records.Tree.leaf', tag=0, fields=['UInt32', 'Bool']),
            dict(name='Records.Tree.branch', tag=1, fields=['Records.Tree', 'Records.Tree']),
        ])
        root = next(item for item in program['functions'] if item['name'] == 'Records.run')
        self.assertEqual([param['type'] for param in root['params']], ['Records.Tree'])
        self.assertEqual(root['result'], 'Records.Tree')

    def test_single_constructor_inductive_is_not_record_metadata(self):
        program = self.export('''inductive Box where
  | mk (value : UInt32)
def run (x : Box) : Box := x
''')
        self.assertEqual(program['api'], {'records': {}})
        self.assertEqual(program['types'], [dict(name='Records.Box', constructors=[
            dict(name='Records.Box.mk', tag=0, fields=['UInt32'])])])


if __name__ == '__main__':
    unittest.main()
