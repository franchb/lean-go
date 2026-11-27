"""Exact mixed-width feature declaration and adversarial producer admission."""
import copy
import json
import os
import unittest

import export_module as producer
import test_bitwise as bitwise
import test_uint64_admission as wide

CASTS = (('UInt32.toUInt64', 'UInt32', 'UInt64'), ('UInt64.toUInt32', 'UInt64', 'UInt32'))


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


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class WidthAdmissionTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export
    nodes = staticmethod(wide.UInt64AdmissionTests.nodes)
    assert_rejected = wide.UInt64AdmissionTests.assert_rejected
    imported_attribute_probe = staticmethod(wide.UInt64AdmissionTests.imported_attribute_probe)

    def test_exact_unary_leaves_and_erased_selection(self):
        for name, operand, result in CASTS:
            for modern in (False, True):
                program = self.export(f'def run (a : {operand}) : {result} := {name} a\n', modern=modern)
                self.assertEqual(program['features'], ['uint32', 'uint64', 'width_conversion'])
                function = program['functions'][0]
                self.assertEqual([p['type'] for p in function['params']], [operand])
                self.assertEqual(function['result'], result)
                values = [node['value'] for node in self.nodes(function['body']) if node['op'] == 'let']
                self.assertEqual([(v['kind'], v['name'], len(v['args'])) for v in values], [('primitive', name, 1)])
            program = self.export(f'def run (a : {operand}) :=\n  let _unused := {name} a\n  a\n')
            self.assertEqual(program['features'], [operand.lower()])
            self.assertIn(name, {item['name'] for item in program['provenance']})
            self.assertFalse(producer.emitted_primitives(program) & producer.WIDTH_CONVERSIONS)

    def test_pristine_closure_is_audited_without_runtime_helper_admission(self):
        cases = [('UInt32.toNat', 'UInt32.toUInt64', 'UInt32'),
                 ('Nat.toUInt32', 'UInt64.toUInt32', 'UInt64'),
                 ('UInt64.toNat', 'UInt64.toUInt32', 'UInt64')]
        for helper, cast, operand in cases:
            probe = self.imported_attribute_probe('externAttr', helper, '{ entries := [.standard `all "untrusted"] }')
            self.assert_rejected(f'def run (a : {operand}) := {cast} a\n', 'Numeric source extern changed', probe=probe)
        for body in ('def run (a : UInt64) := a.toNat\n', 'def run (a : UInt32) := a.toUSize\n',
                     'def run (a : UInt16) := a.toInt16\n', 'axiom forged : UInt64\ndef run := forged.toUInt32\n',
                     'unsafe def run (a : UInt64) := a.toUInt32\n'):
            self.assert_rejected(body, 'extern|axiom|unsafe|Unsafe|Unsupported|unsupported|not supported by code generator|provenance')

    def test_imported_attributes_and_mono_signatures_reject(self):
        for name, operand, result in CASTS:
            prefix = (f'@[noinline] def replacement (a : {operand}) := {name} a\n'
                      f'def initializeCast : IO ({operand} → {result}) := pure {name}\n')
            attributes = [('externAttr', '{ entries := [.standard `all "untrusted"] }'),
                          ('Compiler.implementedByAttr', '`Numeric.replacement'),
                          ('regularInitAttr', '`Numeric.initializeCast')]
            for attribute, value in attributes:
                for erased in (False, True):
                    body = f'def run (a : {operand}) := ' + (f'\n  let _unused := {name} a\n  a\n' if erased else f'{name} a\n')
                    self.assert_rejected(prefix + body, 'Numeric source .*changed|numeric primitive',
                                         probe=self.imported_attribute_probe(attribute, name, value))
            for fields in ('safe := false', 'params := #[]', 'levelParams := [`u]',
                           f'type := .forallE `a (.const ``{result} []) (.const ``{result} []) .default',
                           f'type := .forallE `a (.const ``{operand} []) (.const ``{operand} []) .default',
                           'value := .extern { entries := [.standard `all "untrusted"] }'):
                probe = f'''open Lean LeanGo.Module Lean.Compiler.LCNF
run_cmd Lean.Elab.Command.liftCoreM do
  let some decl ← getMonoDecl? ``{name} | throwError "missing test primitive"
  let changed := {{ decl with {fields} }}
  let some index := (← getEnv).getModuleIdxFor? ``{name}
    | throwError "test requires imported primitive"
  modifyEnv fun env => monoExt.toEnvExtension.modifyState env fun state =>
    let entries := (state.importedEntries[index]!).map fun entry =>
      if entry.name == ``{name} then changed else entry
    {{ state with importedEntries := state.importedEntries.set! index entries }}
  let some observed ← getMonoDecl? ``{name} | throwError "missing mutated primitive"
  unless observed == changed do throwError "test mutation was not observed"
'''
                with self.subTest(primitive=name, fields=fields):
                    self.assert_rejected(f'def run (a : {operand}) := {name} a\n',
                                         'Invalid numeric primitive|Numeric primitive signature/safety mismatch|Numeric mono extern mismatch', probe=probe)

    def test_exact_definition_safety_and_axiom_checks(self):
        # Build isolated exact-name definitions, so no imported constant or
        # cached axiom result can hide these deliberate baseline corruptions.
        for name, operand, _ in CASTS:
            for safety, diagnostic in (('unsafe', 'Unsafe numeric boundary'),
                                       ('safe', 'Unexpected numeric boundary axioms')):
                probe = wide.definition_baseline_probe(name, safety)
                with self.subTest(primitive=name, safety=safety):
                    self.assert_rejected(f'def run (a : {operand}) := {name} a\n', diagnostic, probe=probe)

    def test_local_scoped_late_erased_and_closed_scope_history(self):
        wide.check_primitive_history(self, [(name, operand, True) for name, operand, _ in CASTS], 'width_conversion')


if __name__ == '__main__':
    unittest.main()
