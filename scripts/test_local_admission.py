"""Opt-in fresh Lean checks for source-owned local helpers and data captures."""
import json
import os
import unittest

import export_module as producer
import test_join_admission as joins
import test_record_admission as records
import test_reduced_admission as reduced


ADD = '@[noinline] def add (x y : UInt32) : UInt32 := x + y\n'
CHAIN = ''.join('    let value := add ' + ('a' if step == 0 else 'value') +
                ' bias\n' for step in range(8)) + '    value\n'
SCALAR = ('@[noinline] def run (bias x y : UInt32) : UInt32 :=\n'
          '  let bump := fun (a : UInt32) =>\n' + CHAIN +
          '  add (bump x) (bump y)\n')
BOOL_SCALAR = ('@[noinline] def run (flag : Bool) (bias x y : UInt32) : UInt32 :=\n'
               '  let bump := fun (a : UInt32) =>\n'
               '    let bias := if flag then bias else a\n' + CHAIN +
               '  add (bump x) (bump y)\n')


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1',
                     'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class LocalAdmissionTests(unittest.TestCase):
    # Compose the fresh-source fixture without inheriting unrelated test methods.
    setUp = records.RecordAdmissionTests.setUp
    nodes = staticmethod(joins.JoinAdmissionTests.nodes)
    assert_rejected = reduced.ReducedAdmissionTests.assert_rejected
    functions = reduced.ReducedAdmissionTests.functions
    calls = reduced.ReducedAdmissionTests.calls

    def export(self, body, *, modern=False, entry='Local.run', probe=None):
        prefix = 'module\npublic section\n' if modern else ''
        if probe:
            body += ('\nopen Lean LeanGo.Module Lean.Compiler.LCNF\n'
                     'run_cmd do\n  Lean.Elab.Command.liftCoreM do\n' + probe + '\n')
        (self.source / 'Local.lean').write_text(
            prefix + 'namespace Local\n' + body + '\nend Local\n')
        producer.export_module(self.root, self.source, 'Local', entry, self.output)
        return json.loads(self.output.read_bytes())

    def assert_helper(self, program, owner, params, *, helper=None):
        # This is the pinned compiler's observed survivor, never an admission rule.
        helper = helper or owner + '._lam_0'
        function = self.functions(program)[helper]
        self.assertEqual([param['type'] for param in function['params']], params)
        self.assertEqual(function['result'], 'UInt32')
        provenance = {record['name']: record for record in program['provenance']}
        self.assertEqual(provenance[helper],
                         dict(name=helper, dependencies=[owner], rewrites=[]))
        self.assertIn(owner, provenance)
        self.assertEqual(program['schema'], 'lean-go-ir/v6')
        self.assertEqual(program['profile'], 'pure-module/v4')
        self.assertTrue(any(call['name'] == helper for caller in program['functions']
                            for call in self.calls(caller)),
                        'the lifted function must have an emitted direct call')
        return function

    def test_scalar_capture_exports_actual_helper_and_source_owner(self):
        program = self.export(ADD + SCALAR)
        root = self.functions(program)['Local.run']
        self.assertEqual([param['type'] for param in root['params']], ['UInt32'] * 3)
        self.assertEqual(root['result'], 'UInt32')
        helper = self.assert_helper(program, 'Local.run', ['UInt32', 'UInt32'])
        calls = [call for call in self.calls(root) if call['name'] == helper['name']]
        self.assertEqual([call['args'] for call in calls],
                         [[root['params'][0]['id'], root['params'][i]['id']] for i in (1, 2)])
        owner = next(item for item in program['provenance'] if item['name'] == 'Local.run')
        self.assertIn('Local.add', owner['dependencies'])

    def test_bool_and_scalar_capture_order_matches_both_calls(self):
        program = self.export(ADD + BOOL_SCALAR)
        root = self.functions(program)['Local.run']
        helper = self.assert_helper(program, 'Local.run', ['Bool', 'UInt32', 'UInt32'])
        calls = [call for call in self.calls(root) if call['name'] == helper['name']]
        self.assertEqual([call['args'] for call in calls],
                         [[root['params'][i]['id'] for i in (0, 1, argument)]
                          for argument in (2, 3)])

    def test_nested_record_captures_preserve_source_root_signature(self):
        program = self.export(ADD + '''structure Count where
  value : UInt32
structure Nested where
  count : Count
structure Config where
  nested : Nested
  flag : Bool
@[noinline] def run (config : Config) (x y : Count) : Count :=
  let bump := fun (a : UInt32) =>
    let bias := if config.flag then config.nested.count.value else a
''' + CHAIN + '  ⟨add (bump x.value) (bump y.value)⟩\n')
        root = self.functions(program)['Local.run']
        self.assertEqual([param['type'] for param in root['params']],
                         ['Local.Config', 'Local.Count', 'Local.Count'])
        self.assertEqual(root['result'], 'Local.Count')
        self.assert_helper(program, 'Local.run', ['Local.Config', 'UInt32'])
        types = {item['name']: item for item in program['types']}
        for name, fields in {'Local.Config': ['Local.Nested', 'Bool'],
                             'Local.Nested': ['Local.Count'],
                             'Local.Count': ['UInt32']}.items():
            self.assertEqual(types[name]['constructors'],
                             [dict(name=name + '.mk', tag=0, fields=fields)])

    def test_shadowed_capture_is_the_same_value_at_multiple_calls(self):
        body = SCALAR.replace('  let bump :=', '  let bias := add bias 1\n  let bump :=')
        program = self.export(ADD + body)
        root = self.functions(program)['Local.run']
        helper = self.assert_helper(program, 'Local.run', ['UInt32', 'UInt32'])
        capture = next(node['bind']['id'] for node in self.nodes(root['body'])
                       if node['op'] == 'let' and node['value']['kind'] == 'call'
                       and node['value']['name'] == 'Local.add')
        calls = [call for call in self.calls(root) if call['name'] == helper['name']]
        self.assertEqual(len(calls), 2)
        self.assertEqual([call['args'][0] for call in calls], [capture, capture])
        self.assertNotEqual(capture, root['params'][0]['id'])

    def test_classic_and_modern_imports_retain_helper_ownership(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                prefix = 'module\npublic section\n' if modern else ''
                (self.source / 'Model.lean').write_text(
                    prefix + 'namespace Model\n' + ADD + SCALAR + 'end Model\n')
                header = 'module\npublic import Model\npublic section\n' if modern else 'import Model\n'
                (self.source / 'Caller.lean').write_text(header + '''namespace Caller
def run (bias x y : UInt32) : UInt32 := Model.run bias x y
end Caller
''')
                producer.export_module(self.root, self.source, 'Caller', 'Caller.run', self.output)
                program = json.loads(self.output.read_bytes())
                self.assert_helper(program, 'Model.run', ['UInt32', 'UInt32'])
                self.assertTrue({'source/Model.lean', 'source/Caller.lean'} <= set(program['inputs']))

    def test_authored_suffix_and_unrelated_unsupported_local_are_source_scoped(self):
        program = self.export('''@[noinline] def safe_lam_0 (x : UInt32) : UInt32 := x + 1
@[noinline] def ordinary._lam_0 (x : UInt32) : UInt32 := safe_lam_0 x
def unrelated (x : UInt32) : UInt32 :=
  let localProof := fun (a : UInt32) (_proof : True) => a
  localProof x True.intro
def run (x : UInt32) : UInt32 := ordinary._lam_0 x
''')
        self.assertIn('Local.ordinary._lam_0', self.functions(program))
        provenance = {item['name']: item for item in program['provenance']}
        self.assertIn('Local.safe_lam_0', provenance['Local.ordinary._lam_0']['dependencies'])
        self.assertNotIn('Local.ordinary', provenance['Local.ordinary._lam_0']['dependencies'])
        self.assertNotIn('Local.unrelated', provenance)

    def test_erased_unsupported_local_parameters_preserve_output(self):
        for name, typ, argument in [('proof', 'True', 'True.intro'),
                                    ('wide', 'UInt16', '0'),
                                    ('higher_order', 'UInt32 → UInt32', '(fun z => z)')]:
            with self.subTest(parameter=name):
                body = SCALAR.replace('fun (a : UInt32) =>', f'fun (a : UInt32) (_unused : {typ}) =>')
                body = body.replace('(bump x)', f'(bump x {argument})').replace(
                    '(bump y)', f'(bump y {argument})')
                self.assert_rejected(ADD + body,
                                     '[Ll]ocal.*(type|parameter|signature)|Unsupported|unsupported|proof')
        for alias in (False, True):
            with self.subTest(conditional_function_type_alias=alias):
                body = 'structure Bad where\n  value : UInt32\n  proof : True\n'
                if alias:
                    body += 'abbrev BadFunction := Bad → UInt32\n'
                signature = 'BadFunction' if alias else 'Bad → UInt32'
                body += ('def run (flag : Bool) (x : UInt32) : UInt32 :=\n'
                         f'  let ignored : {signature} := if flag then (fun _ => 1) else (fun _ => 2)\n'
                         '  x\n')
                self.assert_rejected(body, 'Unsupported|unsupported|proof|not an inductive type')

    def test_erased_unsupported_captures_preserve_output(self):
        for name, typ, value in [('proof', 'True', 'True.intro'),
                                 ('wide', 'UInt16', '0'),
                                 ('closure', 'UInt32 → UInt32', '(fun z => z)')]:
            with self.subTest(field=name):
                record = f'structure Settings where\n  bias : UInt32\n  hidden : {typ}\n'
                body = SCALAR.replace('  let bump :=',
                                      f'  let settings : Settings := ⟨bias, {value}⟩\n  let bump :=')
                body = body.replace('    let value := add a bias',
                                    '    let bias := settings.bias\n    let value := add a bias')
                self.assert_rejected(ADD + record + body,
                                     '[Ll]ocal.*(capture|field|type)|Unsupported|unsupported|proof')
        body = SCALAR.replace('  let bump :=',
                              '  let transform := fun (z : UInt32) => z\n  let bump :=')
        body = body.replace('    let value := add a bias',
                            '    let bias := transform bias\n    let value := add a bias')
        self.assert_rejected(ADD + body, 'Unsupported first-order monomorphic type|function-valued capture')

    def test_returned_function_eta_expands_to_existing_data_signature(self):
        body = ('def run (bias : UInt32) : UInt32 → UInt32 :=\n'
                '  let bump := fun (a : UInt32) =>\n' + CHAIN + '  bump\n')
        program = self.export(ADD + body)
        root = self.functions(program)['Local.run']
        self.assertEqual([param['type'] for param in root['params']], ['UInt32', 'UInt32'])
        self.assertEqual(root['result'], 'UInt32')
        calls = self.calls(root)
        self.assertEqual(len(calls), 8)
        self.assertTrue(all(call['name'] == 'Local.add' for call in calls))
        self.assertEqual(calls[0]['args'], [root['params'][1]['id'], root['params'][0]['id']])
        self.assertTrue(all(call['args'][1] == root['params'][0]['id'] for call in calls))

    def test_retained_closure_preserves_output(self):
        body = ('@[noinline] def apply (f : UInt32 → UInt32) (x y : UInt32) : UInt32 :=\n'
                '  add (f x) (f y)\n' +
                SCALAR.replace('  add (bump x) (bump y)', '  apply bump x y'))
        self.assert_rejected(ADD + body,
                             '[Ee]scap|closure|Local helper.*(call|function)|Unsupported first-order')
        # An anonymous argument has no authenticated source local-let witness.
        # Inlining its caller must not authorize the surviving lifted function.
        anonymous = ('@[inline] def twice (f : UInt32 → UInt32) (x y : UInt32) : UInt32 :=\n'
                     '  add (f x) (f y)\n'
                     'def run (bias x y : UInt32) : UInt32 :=\n'
                     '  twice (fun (a : UInt32) =>\n' + CHAIN + '  ) x y\n')
        self.assert_rejected(ADD + anonymous,
                             r'Runtime function lacks compilation provenance: Local\.run\._lam_0')
        # Compiler-cache reuse cannot turn an anonymous lambda into an approved
        # local let merely because an earlier owner authenticated the same body.
        prior = ADD + SCALAR.replace('def run ', 'def first ')
        program = self.export(prior + anonymous, entry='Local.first')
        self.assert_helper(program, 'Local.first', ['UInt32', 'UInt32'])
        for reduced_owner in (False, True):
            with self.subTest(anonymous_cache_reduced_owner=reduced_owner):
                owner = anonymous
                if reduced_owner:
                    owner = owner.replace('(bias x y : UInt32)', '(_unused : UInt32) (bias x y : UInt32)')
                self.assert_rejected(prior + owner,
                                     'anonymous|unowned|untraced|source identity|ambiguous cache reuse')
        # The inner anonymous lambda can be lifted into an outer helper before
        # its cached target is visible in the source owner's final declaration.
        nested_chain = ''.join('  ' + line for line in CHAIN.splitlines(keepends=True))
        nested_first = ('@[noinline] def first (z x y : UInt32) : UInt32 :=\n'
                        '  let bump := fun (a : UInt32) =>\n' +
                        nested_chain.replace(' bias', ' z') + '  add (bump x) (bump y)\n')
        nested_anonymous = (
            'set_option compiler.small 4\n'
            '@[noinline] def add (a b : UInt32) : UInt32 := UInt32.add a b\n' + nested_first +
            '@[inline] def twice (f : UInt32 → UInt32) (x y : UInt32) := add (f x) (f y)\n'
            'def run (bias x y : UInt32) : UInt32 :=\n'
            '  twice (fun (z : UInt32) =>\n'
            '    twice (fun (a : UInt32) =>\n' + nested_chain.replace(' bias', ' z') +
            '    ) z y\n  ) x y\n')
        program = self.export(nested_anonymous, entry='Local.first')
        self.assert_helper(program, 'Local.first', ['UInt32', 'UInt32'])
        for reduced_owner in (True, False):
            with self.subTest(nested_anonymous_reduced_owner=reduced_owner):
                owner = nested_anonymous
                if not reduced_owner:
                    owner = owner.replace('  ) x y\n', '  ) (add bias x) y\n')
                self.assert_rejected(owner,
                                     'ambiguous cache reuse|Nested local helper lacks original source identity')

    def test_nested_locals_reject_without_rejecting_existing_kernel_recursion(self):
        nested = SCALAR.replace('    let value := add a bias',
                                '    let inner := fun (b : UInt32) => add b bias\n'
                                '    let value := inner a')
        self.assert_rejected(ADD + nested, 'Local helper.*nested|[Nn]ested local')
        # Existing safe kernel auxiliary definitions are ordinary source functions;
        # they are not recursively lifted closures admitted by this extension.
        program = self.export(ADD + '''inductive Steps where
  | done
  | more (tail : Steps)
def run (steps : Steps) (bias : UInt32) : UInt32 :=
  let rec bump (remaining : Steps) : UInt32 :=
    match remaining with
    | .done => bias
    | .more tail => add (bump tail) bias
  add (bump steps) bias
''')
        helper = self.functions(program)['Local.run.bump']
        self.assertEqual([param['type'] for param in helper['params']], ['UInt32', 'Local.Steps'])
        self.assertTrue(any(call['name'] == 'Local.run.bump' for call in self.calls(helper)))
        self.assertIn('Local.run.bump', [item['name'] for item in program['provenance']])

    def test_custom_pass_and_erased_foreign_or_axiomatic_dependency_preserve_output(self):
        cases = {
            'custom_pass': ('open Lean.Compiler.LCNF\n'
                            '@[cpass] meta def noOpPass : PassInstaller where\n'
                            '  phase := .base\n  install passes := pure passes\n' + ADD + SCALAR,
                            '[Cc]ustom compiler passes'),
            'foreign': ('@[extern "lean_go_untrusted"] opaque foreign : UInt32 → UInt32\n' +
                        ADD + SCALAR.replace('  let bump :=',
                                             '  let _erased := foreign bias\n  let bump :='),
                        'extern|External function|opaque'),
            'axiom': ('axiom forged : True\n' + ADD +
                      SCALAR.replace('  let bump :=', '  let _proof : True := forged\n  let bump :='),
                      '[Aa]xiom|proof'),
        }
        for boundary, (body, diagnostic) in cases.items():
            with self.subTest(boundary=boundary):
                self.assert_rejected(body, diagnostic)

    def test_original_owner_history_survives_erased_dependency(self):
        definitions = (ADD + '@[noinline] def original (x : UInt32) : UInt32 := x + 1\n'
                       '@[noinline] def replacement (x : UInt32) : UInt32 := original x\n'
                       'theorem replacement_eq : original = replacement := rfl\n')
        owner = SCALAR.replace('  let bump :=', '  let _erased := original bias\n  let bump :=')
        contexts = {
            'local': 'section\nattribute [local csimp] replacement_eq\n' + owner + 'end\n',
            'scoped': ('namespace RewriteScope\nattribute [scoped csimp] replacement_eq\n'
                       'end RewriteScope\nopen scoped RewriteScope\n' + owner),
            'late_csimp': owner + 'attribute [csimp] replacement_eq\n',
            'late_extern': owner + 'attribute [extern "lean_go_untrusted"] original\n',
            'late_implemented_by': owner + 'attribute [implemented_by replacement] original\n',
        }
        for context, body in contexts.items():
            with self.subTest(context=context):
                self.assert_rejected(definitions + body,
                                     'rewrite|replacement|repeated compilation|extern|implemented_by')

    def test_preexisting_name_is_avoided_but_late_kernel_collision_rejects(self):
        collision = 'def run._lam_0 (bias x : UInt32) : UInt32 := add bias x\n'
        program = self.export(ADD + collision + SCALAR)
        self.assert_helper(program, 'Local.run', ['UInt32', 'UInt32'], helper='Local.run._lam_1')
        self.assertNotIn('Local.run._lam_0', self.functions(program))
        self.assert_rejected(ADD + SCALAR + collision,
                             '[Cc]ollision|Local helper.*(identity|invalid|rejected)')

    def test_auxiliary_cache_reuse_rejects_only_the_ambiguous_owner(self):
        body = ADD + SCALAR.replace('def run ', 'def first ') + SCALAR.replace('def run ', 'def second ')
        program = self.export(body, entry='Local.first')
        self.assert_helper(program, 'Local.first', ['UInt32', 'UInt32'])
        self.assert_rejected(body, '[Aa]mbiguous|[Aa]uxiliary|[Ll]ocal.*reuse', entry='Local.second')
        # Two distinct computed captures share a binder name. The pinned compiler
        # reuses the first lifted declaration within the same lambda-lifting pass.
        same_pass = (ADD + '@[noinline] def run (initial other x y : UInt32) : UInt32 :=\n'
                     '  let bias := add initial 1\n'
                     '  let first := fun (a : UInt32) =>\n' + CHAIN +
                     '  let bias := add other 1\n'
                     '  let second := fun (a : UInt32) =>\n' + CHAIN +
                     '  add (add (first x) (first y)) (add (second x) (second y))\n')
        self.assert_rejected(same_pass, 'ambiguous cache reuse',
                             probe=self.evidence_fixture('    pure ()'))

    @staticmethod
    def evidence_fixture(mutation):
        return '''    let name := `Local.run._lam_0
    let some entry := (localExt.getState (← getEnv)).find? name
      | throwError "Test fixture requires compiler evidence"
    let some evidence := entry.evidence?
      | throwError "Test fixture requires successful compiler evidence"
''' + mutation + '\n'

    def corrupt_evidence(self, fields):
        return self.evidence_fixture('    modifyEnv (localExt.addEntry · { entry with\n'
                                     f'      evidence? := some {{ evidence with {fields} }} }})')

    def test_stale_final_and_pre_lifting_snapshots_preserve_output(self):
        cases = {
            'container': ('containerDecl := { evidence.containerDecl with recursive := !evidence.containerDecl.recursive }',
                          'declaration snapshot mismatch'),
            'helper': ('helperDecl := { evidence.helperDecl with recursive := !evidence.helperDecl.recursive }',
                       'declaration snapshot mismatch'),
            'pre_lifting': ('beforeDecl := { evidence.beforeDecl with value := .code '
                            '(.return evidence.beforeDecl.params[0]!.fvarId) }',
                            'declaration or direct call mapping mismatch|pre-lifting local missing'),
            'source_capture_link': ('sourceCaptures := #[]', 'source snapshot mapping mismatch'),
            'typed_capture_link': ('typedCaptures := #[]', 'typed source mapping mismatch'),
        }
        for snapshot, (fields, diagnostic) in cases.items():
            with self.subTest(snapshot=snapshot):
                self.assert_rejected(ADD + SCALAR, diagnostic, probe=self.corrupt_evidence(fields))

    def test_capture_order_and_type_corruption_preserve_output(self):
        cases = {
            'capture_order': ('captures := evidence.captures.reverse',
                              'capture mapping mismatch|declaration or direct call mapping mismatch'),
            'capture_type': ('captures := evidence.captures.modify 0 '
                             '(fun param => { param with type := mkConst ``UInt32 })',
                             'lifted body mismatch|declaration or direct call mapping mismatch'),
        }
        for corruption, (fields, diagnostic) in cases.items():
            with self.subTest(corruption=corruption):
                self.assert_rejected(ADD + BOOL_SCALAR, diagnostic, probe=self.corrupt_evidence(fields))

    def test_missing_evidence_wrong_owner_and_module_preserve_output(self):
        cases = {
            'missing': (self.evidence_fixture(
                        '    modifyEnv (localExt.addEntry · { entry with evidence? := none })'),
                        'Missing local helper evidence'),
            'owner': (self.corrupt_evidence('sourceOwner := `Local.add'),
                      'owner derivation|owner mismatch|source owner'),
            'module': (self.corrupt_evidence('sourceModule := `OtherModule'), 'module mismatch'),
        }
        for corruption, (probe, diagnostic) in cases.items():
            with self.subTest(corruption=corruption):
                self.assert_rejected(ADD + SCALAR, diagnostic, probe=probe)

    def test_changed_owner_cannot_hide_evidence_by_erasing_helper_calls(self):
        erase_calls = '''    let container := { evidence.containerDecl with
      value := .code (.return evidence.containerDecl.params[0]!.fvarId) }
    modifyEnv (saveMonoDeclCore · container)
'''
        cases = {
            'stale_snapshot': ('', 'declaration snapshot mismatch'),
            'missing_evidence': (
                '    modifyEnv (localExt.addEntry · { entry with evidence? := none })',
                'Missing local helper evidence'),
            'wrong_owner': (
                '    modifyEnv (localExt.addEntry · { entry with evidence? := some '
                '{ evidence with sourceOwner := `Local.add } })',
                'owner derivation|owner mismatch|owner association|source owner'),
        }
        for corruption, (mutation, diagnostic) in cases.items():
            with self.subTest(corruption=corruption):
                self.assert_rejected(ADD + SCALAR, diagnostic,
                                     probe=self.evidence_fixture(erase_calls + mutation))

    def test_reduced_owner_composition_is_authenticated_or_explicitly_unsupported(self):
        body = ADD + SCALAR.replace('(bias x y : UInt32)', '(_unused : UInt32) (bias x y : UInt32)')
        self.output.write_bytes(b'previous output\n')
        try:
            program = self.export(body)
        except producer.ExportError as rejected:
            self.assertRegex(str(rejected), 'Unsupported local helper.*(owner|derivation|reduced)|'
                                           'unsupported.*local.*reduced|unsupported.*reduced.*local')
            self.assertEqual(self.output.read_bytes(), b'previous output\n')
            return
        root = self.functions(program)['Local.run']
        self.assertEqual([param['type'] for param in root['params']], ['UInt32'] * 4)
        derived = [item['name'] for item in program['provenance']
                   if item['dependencies'] == ['Local.run'] and '._lam_' in item['name']]
        self.assertEqual(len(derived), 1, 'composition must retain one authenticated local helper')
        self.assert_helper(program, 'Local.run', ['UInt32', 'UInt32'], helper=derived[0])


if __name__ == '__main__':
    unittest.main()
