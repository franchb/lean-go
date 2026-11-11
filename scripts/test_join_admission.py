"""Opt-in fresh Lean checks for lexically scoped local join points."""
import os
import unittest

import test_record_admission as records


PRELUDE = """structure Count where
  value : UInt32
structure State where
  count : Count
  enabled : Bool
@[noinline] def add (x y : UInt32) : UInt32 := x + y
"""


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1',
                     'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class JoinAdmissionTests(unittest.TestCase):
    # Reuse the fresh-source fixture without inheriting the record-only tests.
    setUp = records.RecordAdmissionTests.setUp
    assert_rejected = records.RecordAdmissionTests.assert_rejected

    def export(self, body):
        return records.RecordAdmissionTests.export(self, PRELUDE + body)

    @staticmethod
    def nodes(code):
        yield code
        if code['op'] in ('let', 'join'):
            if code['op'] == 'join':
                yield from JoinAdmissionTests.nodes(code['join']['body'])
            yield from JoinAdmissionTests.nodes(code['next'])
        elif code['op'] == 'cases':
            for alternative in code['alts']:
                yield from JoinAdmissionTests.nodes(alternative['body'])

    def root_function(self, program):
        return next(function for function in program['functions']
                    if function['name'] == 'Records.run')

    def assert_unique_ids(self, function):
        ids = [param['id'] for param in function['params']]
        for node in self.nodes(function['body']):
            if node['op'] == 'let':
                ids.append(node['bind']['id'])
            elif node['op'] == 'join':
                ids.append(node['join']['id'])
                ids.extend(param['id'] for param in node['join']['params'])
            elif node['op'] == 'cases':
                ids.extend(param['id'] for alternative in node['alts']
                           for param in alternative['params'])
        self.assertTrue(all(identifier > 0 for identifier in ids))
        self.assertEqual(len(ids), len(set(ids)), 'join and value IDs share one namespace')

    def test_conditional_record_update_keeps_captured_state(self):
        program = self.export("""def run (b : Bool) (state : State) (delta : Count) : State :=
  let count := if b then delta else state.count
  { state with count := ⟨add count.value state.count.value⟩ }
""")
        function = self.root_function(program)
        nodes = list(self.nodes(function['body']))
        joins = [node['join'] for node in nodes if node['op'] == 'join']
        self.assertEqual(len(joins), 1, 'the example must exercise a compiler join')
        join = joins[0]
        self.assertEqual([param['type'] for param in join['params']], ['UInt32'])
        self.assertEqual(function['result'], 'Records.State')
        captured_state = function['params'][1]['id']
        body = list(self.nodes(join['body']))
        self.assertTrue(any(node['op'] == 'cases' and node['var'] == captured_state
                            for node in body))
        self.assertEqual(sum(node['op'] == 'jump' and node['target'] == join['id']
                             for node in nodes), 2)
        self.assertTrue(any(node['op'] == 'let' and node['value']['kind'] == 'call'
                            and node['value']['name'] == 'Records.add' for node in body))
        self.assertIn('Records.add', [item['name'] for item in program['functions']])
        self.assertIn('Records.add', [item['name'] for item in program['provenance']])
        self.assert_unique_ids(function)

    def test_conditional_and_match_results_rebuild_source_wrapper(self):
        selectors = {
            'conditional': ('(b : Bool) (x y bias : Count)', 'if b then x else y'),
            'match': ('(choice : Selection) (bias : Count)',
                      'match choice with | .left value => value | .right value => value'),
        }
        for name, (input_param, selection) in selectors.items():
            with self.subTest(selection=name):
                program = self.export(f"""inductive Selection where
  | left (value : Count)
  | right (value : Count)
def run {input_param} : Count :=
  let selected := {selection}
  let first := add selected.value bias.value
  let second := add first bias.value
  let third := add second bias.value
  ⟨add third bias.value⟩
""")
                function = self.root_function(program)
                nodes = list(self.nodes(function['body']))
                joins = [node['join'] for node in nodes if node['op'] == 'join']
                self.assertEqual(len(joins), 1)
                self.assertEqual([param['type'] for param in joins[0]['params']], ['UInt32'])
                self.assertEqual(function['result'], 'Records.Count')
                self.assertTrue(all(node['result'] == 'Records.Count' for node in nodes
                                    if node['op'] == 'cases'))
                body = list(self.nodes(joins[0]['body']))
                wrappers = [node for node in body if node['op'] == 'let'
                            and node['value']['name'] == 'Records.Count.mk']
                self.assertEqual(len(wrappers), 1)
                self.assertEqual(body[-1], dict(op='return', var=wrappers[0]['bind']['id']))
                self.assert_unique_ids(function)

    def test_nested_joins_can_jump_to_prior_targets_and_capture_values(self):
        program = self.export("""def run (a b : Bool) (x y z bias : UInt32) : UInt32 :=
  let first := if a then x else y
  let second := if b then first else z
  let one := add second bias
  let two := add one bias
  let three := add two bias
  add three bias
""")
        function = self.root_function(program)
        joins = [node['join'] for node in self.nodes(function['body']) if node['op'] == 'join']
        self.assertEqual(len(joins), 2)
        outer, inner = joins
        self.assertTrue(any(node['op'] == 'jump' and node['target'] == outer['id']
                            for node in self.nodes(inner['body'])))
        captured_bias = function['params'][-1]['id']
        calls = [node['value'] for node in self.nodes(outer['body']) if node['op'] == 'let']
        self.assertEqual(len(calls), 4)
        self.assertTrue(all(call['name'] == 'Records.add' and captured_bias in call['args']
                            for call in calls))
        self.assert_unique_ids(function)

    def test_foreign_axiom_and_rewrite_inside_continuation_preserve_output(self):
        continuation = """def run (b : Bool) (x y bias : UInt32) : UInt32 :=
  let selected := if b then x else y
  let first := add selected bias
  let second := add first bias
  let third := add second bias
  helper third
"""
        helper = '@[noinline] def helper (x : UInt32) : UInt32 := x\n'
        baseline = self.root_function(self.export(helper + continuation))
        join = next(node['join'] for node in self.nodes(baseline['body']) if node['op'] == 'join')
        self.assertTrue(any(node['op'] == 'let' and node['value']['name'] == 'Records.helper'
                            for node in self.nodes(join['body'])))
        cases = {
            'foreign': ('@[extern "lean_go_untrusted"] opaque foreign : UInt32 → UInt32\n'
                        '@[noinline] def helper (x : UInt32) : UInt32 := foreign x\n'
                        + continuation, 'extern|External function|opaque'),
            'axiom': ('axiom forged : True\n'
                      '@[noinline] def helper (x : UInt32) : UInt32 :=\n'
                      '  let _proof : True := forged\n  x\n' + continuation,
                      '[Aa]xiom|provenance'),
            'local_rewrite': (helper
                              + '@[noinline] def replacement (x : UInt32) : UInt32 := helper x\n'
                              + 'theorem replacement_eq : helper = replacement := rfl\n'
                              + 'section\nattribute [local csimp] replacement_eq\n'
                              + continuation + 'end\n', 'rewrite|replacement|repeated compilation'),
            'late_foreign': (helper + continuation
                             + 'attribute [extern "lean_go_untrusted"] helper\n', 'extern'),
        }
        for name, (body, diagnostic) in cases.items():
            with self.subTest(boundary=name):
                self.assert_rejected(body, diagnostic)

    def test_captured_closure_remains_rejected(self):
        # The pinned compiler retains a partially applied lifted lambda here;
        # a directly invoked local lambda would be optimized into ordinary code.
        self.assert_rejected("""@[noinline] def apply (f : UInt32 → UInt32) (value : UInt32) : UInt32 :=
  f value
def run (bias value : UInt32) : UInt32 :=
  let captured := fun x => x + bias
  apply captured value
""", 'Unsupported first-order monomorphic type')


if __name__ == '__main__':
    unittest.main()
