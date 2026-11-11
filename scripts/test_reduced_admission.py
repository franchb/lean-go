"""Opt-in fresh Lean checks for authenticated reduced-argument helpers."""
from functools import partial
import json
import os
import unittest
from unittest import mock

import export_module as producer
import test_join_admission as joins
import test_record_admission as records


ADD = '@[noinline] def add (x y : UInt32) : UInt32 := x + y\n'
WORK = """  let selected := if flag then x else y
  let first := add selected bias
  let second := add first bias
  let third := add second bias
  add third bias
"""


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1',
                     'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class ReducedAdmissionTests(unittest.TestCase):
    # Compose the fresh-source harness without inheriting its unrelated tests.
    setUp = records.RecordAdmissionTests.setUp
    nodes = staticmethod(joins.JoinAdmissionTests.nodes)

    def export(self, body, *, modern=False, entry='Reduced.run', probe=None):
        prefix = 'module\npublic section\n' if modern else ''
        if probe:
            # Explicit adversarial metadata fixture, executed after the source
            # declarations compile while their mono bodies are still local.
            body += ('\nopen Lean LeanGo.Module Lean.Compiler.LCNF\n'
                     'run_cmd do\n  Lean.Elab.Command.liftCoreM do\n' + probe + '\n')
        (self.source / 'Reduced.lean').write_text(
            prefix + 'namespace Reduced\n' + body + '\nend Reduced\n')
        producer.export_module(self.root, self.source, 'Reduced', entry, self.output)
        return json.loads(self.output.read_bytes())

    def assert_rejected(self, body, diagnostic, **options):
        with mock.patch.object(self, 'export', partial(self.export, **options)):
            records.RecordAdmissionTests.assert_rejected(self, body, diagnostic)

    def functions(self, program):
        return {function['name']: function for function in program['functions']}

    def assert_helper(self, program, owner, params, result='UInt32'):
        helper = owner + '._redArg'
        function = self.functions(program)[helper]
        self.assertEqual([param['type'] for param in function['params']], params)
        self.assertEqual(function['result'], result)
        provenance = {record['name']: record for record in program['provenance']}
        self.assertEqual(provenance[helper],
                         dict(name=helper, dependencies=[owner], rewrites=[]))
        self.assertIn(owner, provenance)
        self.assertEqual(program['schema'], 'lean-go-ir/v6')
        self.assertEqual(program['profile'], 'pure-module/v4')
        return function

    def calls(self, function):
        return [node['value'] for node in self.nodes(function['body'])
                if node['op'] == 'let' and node['value']['kind'] == 'call']

    def test_fresh_root_retains_full_signature_and_exports_actual_helper(self):
        program = self.export(ADD +
            'def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n'
            + WORK)
        root = self.functions(program)['Reduced.run']
        self.assertEqual([param['type'] for param in root['params']],
                         ['Bool', 'UInt32', 'UInt32', 'UInt32', 'UInt32'])
        helper = self.assert_helper(program, 'Reduced.run',
                                    ['Bool', 'UInt32', 'UInt32', 'UInt32'])
        self.assertIn(helper['name'], [call['name'] for call in self.calls(root)])
        provenance = {item['name']: item for item in program['provenance']}
        self.assertIn('Reduced.add', provenance['Reduced.run']['dependencies'])

    def test_unused_positions_and_multiple_drops_keep_source_order(self):
        cases = {
            'first': '(_a : UInt32) (flag : Bool) (x y bias : UInt32)',
            'middle': '(flag : Bool) (x : UInt32) (_a : UInt32) (y bias : UInt32)',
            'last': '(flag : Bool) (x y bias : UInt32) (_a : UInt32)',
            'several': '(_a : UInt32) (flag : Bool) (_b : Bool) (x y bias : UInt32) (_c : UInt32)',
        }
        for position, params in cases.items():
            with self.subTest(position=position):
                program = self.export(ADD + f'def run {params} : UInt32 :=\n' + WORK)
                helper = self.assert_helper(program, 'Reduced.run',
                                            ['Bool', 'UInt32', 'UInt32', 'UInt32'])
                root = self.functions(program)['Reduced.run']
                call = next(call for call in self.calls(root) if call['name'] == helper['name'])
                retained = {'first': [1, 2, 3, 4], 'middle': [0, 1, 3, 4],
                            'last': [0, 1, 2, 3], 'several': [1, 3, 4, 5]}[position]
                self.assertEqual(call['args'], [root['params'][index]['id'] for index in retained])
                self.assertEqual(len(root['params']), 7 if position == 'several' else 5)

    def test_all_unused_uses_zero_runtime_parameters_and_nullary_owner_works(self):
        work = """  let first := add 1 2
  let second := add first 3
  let third := add second 4
  add third 5
"""
        for params in ('(_a : UInt32) (_b : Bool)', ''):
            with self.subTest(params=params):
                program = self.export(ADD + f'def run {params} : UInt32 :=\n' + work)
                root = self.functions(program)['Reduced.run']
                self.assertEqual([param['type'] for param in root['params']],
                                 ['UInt32', 'Bool'] if params else [])
                if params:
                    helper = self.assert_helper(program, 'Reduced.run', [])
                    self.assertEqual(next(call['args'] for call in self.calls(root)
                                          if call['name'] == helper['name']), [])

    def test_small_owner_without_forwarding_wrapper_still_works(self):
        program = self.export('def run (_unused : UInt32) (value : UInt32) : UInt32 := value\n')
        functions = self.functions(program)
        self.assertEqual(set(functions), {'Reduced.run'})
        self.assertEqual(len(functions['Reduced.run']['params']), 2)
        self.assertEqual(functions['Reduced.run']['body'],
                         dict(op='return', var=functions['Reduced.run']['params'][1]['id']))

    def test_direct_noinline_call_emits_helper_and_unchanged_owner_provenance(self):
        program = self.export(ADD +
            '@[noinline] def choose (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n'
            + WORK + 'def run (flag : Bool) (x y bias : UInt32) : UInt32 := choose flag x x y bias\n')
        helper = self.assert_helper(program, 'Reduced.choose', ['Bool', 'UInt32', 'UInt32', 'UInt32'])
        functions = self.functions(program)
        self.assertIn(helper['name'], [call['name'] for call in self.calls(functions['Reduced.run'])])
        owner = next(item for item in program['provenance'] if item['name'] == 'Reduced.choose')
        self.assertIn('Reduced.add', owner['dependencies'])
        self.assertNotIn(helper['name'], owner['dependencies'])

    def test_recursive_helper_calls_itself(self):
        program = self.export(ADD + """inductive Chain where
  | done
  | next (rest : Chain)
@[noinline] def descend (chain : Chain) (_unused : UInt32) (value : UInt32) : UInt32 :=
  match chain with
  | .done => add value 1
  | .next rest =>
    let first := add value 2
    let second := add first 3
    let third := add second 4
    descend rest _unused (add third 5)
def run (chain : Chain) (value : UInt32) : UInt32 := descend chain value value
""")
        helper = self.assert_helper(program, 'Reduced.descend', ['Reduced.Chain', 'UInt32'])
        self.assertIn(helper['name'], [call['name'] for call in self.calls(helper)])

    def test_retained_and_discarded_wrappers_keep_source_types_and_result(self):
        program = self.export(ADD + """structure Count where
  value : UInt32
structure Flag where
  value : Bool
structure Discarded where
  value : UInt32
@[noinline] def choose (flag : Flag) (_unused : Discarded) (x y bias : Count) : Count :=
  let selected := if flag.value then x.value else y.value
  let first := add selected bias.value
  let second := add first bias.value
  let third := add second bias.value
  ⟨add third bias.value⟩
def run (flag : Flag) (x y bias : Count) : Count := choose flag ⟨x.value⟩ x y bias
""")
        helper = self.assert_helper(program, 'Reduced.choose',
                                    ['Reduced.Flag', 'Reduced.Count', 'Reduced.Count', 'Reduced.Count'],
                                    'Reduced.Count')
        self.assertNotIn('Reduced.Discarded', [item['name'] for item in program['types']])
        self.assertTrue(any(node['op'] == 'let' and node['value']['kind'] == 'constructor'
                            and node['value']['name'] == 'Reduced.Count.mk'
                            for node in self.nodes(helper['body'])))

    def test_classic_and_modern_imports_retain_compiler_evidence(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                prefix = 'module\npublic section\n' if modern else ''
                (self.source / 'Model.lean').write_text(prefix + 'namespace Model\n' + ADD +
                    '@[noinline] def choose (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n'
                    + WORK + 'end Model\n')
                header = 'module\npublic import Model\npublic section\n' if modern else 'import Model\n'
                (self.source / 'Caller.lean').write_text(header + """namespace Caller
def run (flag : Bool) (x y bias : UInt32) : UInt32 := Model.choose flag x x y bias
end Caller
""")
                producer.export_module(self.root, self.source, 'Caller', 'Caller.run', self.output)
                program = json.loads(self.output.read_bytes())
                helper = self.assert_helper(program, 'Model.choose', ['Bool', 'UInt32', 'UInt32', 'UInt32'])
                self.assertIn(helper['name'], [call['name'] for call in self.calls(
                    self.functions(program)['Caller.run'])])
                self.assertTrue({'source/Model.lean', 'source/Caller.lean'} <= set(program['inputs']))

    def test_ordinary_source_name_containing_redarg_has_no_special_privilege(self):
        program = self.export('@[noinline] def safe_redArg (x : UInt32) : UInt32 := x + 1\n'
                              '@[noinline] def ordinary._redArg (x : UInt32) : UInt32 := safe_redArg x\n'
                              'def run (x : UInt32) : UInt32 := ordinary._redArg x\n')
        self.assertIn('Reduced.safe_redArg', self.functions(program))
        self.assertIn('Reduced.ordinary._redArg', self.functions(program))
        record = next(item for item in program['provenance'] if item['name'] == 'Reduced.safe_redArg')
        self.assertNotEqual(record['dependencies'], ['Reduced.safe'])
        ordinary = next(item for item in program['provenance']
                        if item['name'] == 'Reduced.ordinary._redArg')
        self.assertIn('Reduced.safe_redArg', ordinary['dependencies'])
        self.assertNotIn('Reduced.ordinary', ordinary['dependencies'])

    def test_kernel_collisions_before_and_after_owner_preserve_output(self):
        owner = (ADD + '@[noinline] def choose (flag : Bool) (_unused : UInt32) '
                 '(x y bias : UInt32) : UInt32 :=\n' + WORK)
        collision = 'def choose._redArg (flag : Bool) (x y bias : UInt32) : UInt32 := x\n'
        caller = 'def run (flag : Bool) (x y bias : UInt32) : UInt32 := choose flag x x y bias\n'
        for order, body in {'before': collision + owner + caller,
                            'after': owner + collision + caller}.items():
            with self.subTest(order=order):
                self.assert_rejected(body, '[Cc]ollision|reduced[- ]argument|[Ii]nvalid reduced')

    def test_overwritten_authored_helper_selected_as_root_is_rejected(self):
        # The tiny owner's forwarding wrapper disappears. A collision event must
        # still prevent the selected authored name from using ordinary admission.
        self.assert_rejected('def choose._redArg (value : UInt32) : UInt32 := value + 7\n'
                             'def choose (_unused : UInt32) (value : UInt32) : UInt32 := value\n',
                             '[Cc]ollision|reduced[- ]argument|[Ii]nvalid reduced',
                             entry='Reduced.choose._redArg')

    def test_constructor_name_collision_cannot_bypass_call_resolution(self):
        self.assert_rejected("""inductive Foo where
  | run._redArg (value : UInt32)
  | other (value : UInt32)
namespace Foo
@[noinline] def run (value _unused : UInt32) : Foo := .other (value + value)
end Foo
def run (value unused : UInt32) : Foo := Foo.run value unused
""", '[Cc]ollision')

    def test_erased_source_proof_and_foreign_dependencies_preserve_output(self):
        owner = ('@[noinline] def choose (flag : Bool) (_unused : UInt32) '
                 '(x y bias : UInt32) : UInt32 :=\n')
        caller = 'def run (flag : Bool) (x y bias : UInt32) : UInt32 := choose flag x x y bias\n'
        cases = {
            'proof': ('axiom forged : True\n' + ADD + owner +
                      '  let _proof : True := forged\n' + WORK + caller, '[Aa]xiom|proof|provenance'),
            'foreign': ('@[extern "lean_go_untrusted"] opaque foreign : UInt32 → UInt32\n' +
                        ADD + owner + '  let _erased := foreign x\n' + WORK + caller,
                        'extern|External function|opaque'),
        }
        for boundary, (body, diagnostic) in cases.items():
            with self.subTest(boundary=boundary):
                self.assert_rejected(body, diagnostic)

    def test_contextual_and_late_source_changes_preserve_output(self):
        definitions = (ADD + '@[noinline] def original (x : UInt32) : UInt32 := x + 1\n'
                       '@[noinline] def replacement (x : UInt32) : UInt32 := original x\n'
                       'theorem replacement_eq : original = replacement := rfl\n')
        owner = ('@[noinline] def choose (flag : Bool) (_unused : UInt32) '
                 '(x y bias : UInt32) : UInt32 :=\n'
                 '  let _erased := original x\n' + WORK)
        caller = 'def run (flag : Bool) (x y bias : UInt32) : UInt32 := choose flag x x y bias\n'
        contexts = {
            'local': 'section\nattribute [local csimp] replacement_eq\n' + owner + 'end\n' + caller,
            'scoped': ('namespace RewriteScope\nattribute [scoped csimp] replacement_eq\n'
                       'end RewriteScope\nopen scoped RewriteScope\n' + owner + caller),
            'late_csimp': owner + caller + 'attribute [csimp] replacement_eq\n',
            'late_extern': owner + caller + 'attribute [extern "lean_go_untrusted"] original\n',
            'implemented_by': 'attribute [implemented_by replacement] original\n' + owner + caller,
            'late_implemented_by': owner + caller + 'attribute [implemented_by replacement] original\n',
        }
        for context, body in contexts.items():
            with self.subTest(context=context):
                self.assert_rejected(definitions + body, 'rewrite|replacement|repeated compilation|extern|implemented_by')

    def test_discarded_unsupported_source_fields_preserve_output(self):
        fields = {'wide': 'UInt16', 'closure': 'UInt32 → UInt32',
                  'proof': 'True', 'dependent': 'Fin 7'}
        for field, typ in fields.items():
            with self.subTest(field=field):
                body = (ADD + 'inductive Discarded where\n  | empty\n'
                        f'  | boxed (value : {typ})\n'
                        '@[noinline] def choose (flag : Bool) (_unused : Discarded) '
                        '(x y bias : UInt32) : UInt32 :=\n' + WORK +
                        'def run (flag : Bool) (x y bias : UInt32) : UInt32 :=\n'
                        '  choose flag .empty x y bias\n')
                self.assert_rejected(body, 'Unsupported|unsupported|monomorphic|proof|source field|wrapper')

    def test_retained_closure_remains_rejected(self):
        self.assert_rejected("""@[noinline] def apply (f : UInt32 → UInt32) (_unused : UInt32)
    (value : UInt32) : UInt32 := f value
def run (bias value : UInt32) : UInt32 := apply (fun x => x + bias) bias value
""", 'Unsupported first-order monomorphic type')

    def evidence_fixture(self, mutation, *, helper='Reduced.run._redArg'):
        return f'''    let name := `{helper}
    let some entry := (reducedExt.getState (← getEnv)).find? name
      | throwError "Test fixture requires compiler evidence"
    let some evidence := entry.evidence?
      | throwError "Test fixture requires successful compiler evidence"
{mutation}
'''

    def test_malformed_evidence_snapshots_identity_and_mapping_preserve_output(self):
        body = (ADD + 'def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n'
                + WORK)
        cases = {
            'owner_snapshot': ('ownerDecl := { evidence.ownerDecl with recursive := !evidence.ownerDecl.recursive }',
                               'declaration snapshot mismatch'),
            'helper_snapshot': ('helperDecl := { evidence.helperDecl with recursive := !evidence.helperDecl.recursive }',
                                'declaration snapshot mismatch'),
            'wrong_module': ('sourceModule := `OtherModule', 'module mismatch'),
            'wrong_owner': ('owner := `Reduced.add', 'identity or kernel collision'),
            'wrong_helper': ('helper := `Reduced.absent', 'identity or kernel collision'),
            'nonordered': ('retained := #[2, 0, 3, 4]', 'forwarding mapping mismatch'),
            'duplicate': ('retained := #[0, 2, 2, 4]', 'forwarding mapping mismatch'),
            'out_of_range': ('retained := #[0, 2, 3, 5]', 'forwarding mapping mismatch'),
            'false_all_unused': ('retained := #[], allUnused := true', 'forwarding mapping mismatch'),
        }
        for corruption, (fields, diagnostic) in cases.items():
            with self.subTest(corruption=corruption):
                probe = self.evidence_fixture(
                    '    modifyEnv (reducedExt.addEntry · { entry with\n'
                    f'      evidence? := some {{ evidence with {fields} }} }})')
                self.assert_rejected(body, diagnostic, probe=probe)

    def test_missing_evidence_and_uncompiled_or_unsafe_source_owner_preserve_output(self):
        body = (ADD + '@[noinline] def choose (flag : Bool) (_unused : UInt32) '
                '(x y bias : UInt32) : UInt32 :=\n' + WORK +
                'def run (flag : Bool) (x y bias : UInt32) : UInt32 := choose flag x x y bias\n')
        mutations = {
            'missing_evidence': ('    modifyEnv (reducedExt.addEntry · { entry with evidence? := none })',
                                 'Missing reduced helper evidence'),
            'uncompiled_owner': ('    let some source := (provenanceExt.getState (← getEnv)).find? evidence.owner\n'
                                 '      | throwError "Test fixture requires source owner"\n'
                                 '    modifyEnv (provenanceExt.addEntry · { source with compiled := false })',
                                 'source owner rejected|fresh source provenance'),
            'unsafe_owner': ('    let some source := (provenanceExt.getState (← getEnv)).find? evidence.owner\n'
                             '      | throwError "Test fixture requires source owner"\n'
                             '    modifyEnv (provenanceExt.addEntry · { source with safeDefinition := false })',
                             'source owner rejected|source provenance|safe source'),
            'wrong_source_module': ('    let some source := (provenanceExt.getState (← getEnv)).find? evidence.owner\n'
                                    '      | throwError "Test fixture requires source owner"\n'
                                    '    modifyEnv (provenanceExt.addEntry · { source with sourceModule := `OtherModule })',
                                    'source owner rejected|source provenance|safe source'),
        }
        for corruption, (mutation, diagnostic) in mutations.items():
            with self.subTest(corruption=corruption):
                self.assert_rejected(body, diagnostic, probe=self.evidence_fixture(
                    mutation, helper='Reduced.choose._redArg'))

    def test_changed_owner_body_cannot_erase_evidence_audit(self):
        body = (ADD + 'def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n'
                + WORK)
        mutation = '''    let owner := { evidence.ownerDecl with
      value := .code (.return evidence.ownerDecl.params[2]!.fvarId) }
    modifyEnv (saveMonoDeclCore · owner)
'''
        cases = {
            'stale_snapshot': (mutation, 'declaration snapshot mismatch'),
            'nonforwarding_wrapper': (mutation +
                '    modifyEnv (reducedExt.addEntry · { entry with evidence? := some { evidence with ownerDecl := owner } })',
                'forwarding mapping mismatch'),
        }
        for corruption, (probe, diagnostic) in cases.items():
            with self.subTest(corruption=corruption):
                self.assert_rejected(body, diagnostic, probe=self.evidence_fixture(probe))

    def test_all_unused_rejects_non_erased_call_and_dummy_data_use(self):
        body = ADD + """def run (_unused : UInt32) : UInt32 :=
  let first := add 1 2
  let second := add first 3
  let third := add second 4
  add third 5
"""
        cases = {
            'non_erased_argument': ('''    let .code (.let call continuation) := evidence.ownerDecl.value
      | throwError "Test fixture requires owner forwarding wrapper"
    let .const name levels _ _ := call.value
      | throwError "Test fixture requires helper call"
    let call := { call with value := .const name levels #[.fvar evidence.ownerDecl.params[0]!.fvarId] }
    let owner := { evidence.ownerDecl with value := .code (.let call continuation) }
    modifyEnv (saveMonoDeclCore · owner)
    modifyEnv (reducedExt.addEntry · { entry with evidence? := some { evidence with ownerDecl := owner } })''',
                                   'forwarding mapping mismatch'),
            'dummy_data_use': ('''    let helper := { evidence.helperDecl with
      value := .code (.return evidence.helperDecl.params[0]!.fvarId) }
    modifyEnv (saveMonoDeclCore · helper)
    modifyEnv (reducedExt.addEntry · { entry with evidence? := some { evidence with helperDecl := helper } })''',
                               'Unbound variable|dummy|Dummy'),
        }
        for corruption, (mutation, diagnostic) in cases.items():
            with self.subTest(corruption=corruption):
                self.assert_rejected(body, diagnostic, probe=self.evidence_fixture(mutation))


if __name__ == '__main__':
    unittest.main()
