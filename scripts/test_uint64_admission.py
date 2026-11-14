"""Fresh-source checks for the exact UInt64 primitive and source boundary."""
import json
import os
import unittest
from unittest import mock

import export_module as producer
import test_join_admission as joins
import test_local_admission as local
import test_numeric_admission as numeric
import test_reduced_admission as reduced


def definition_baseline_probe(name, safety):
    # Isolate the exact-name definition from imported constants and axiom caches.
    return f'''open Lean LeanGo.Module
run_cmd Lean.Elab.Command.liftCoreM do
  withEnv (← mkEmptyEnvironment) do
    addDecl <| .axiomDecl {{ name := `Forged, levelParams := [], type := .sort .zero, isUnsafe := false }}
    addDecl <| .defnDecl {{ name := ``{name}, levelParams := [], type := .sort .zero, value := .const `Forged [], hints := .opaque, safety := .{safety} }}
    validateNumericDefinition ``{name}
'''


def rewrite_sources(prelude, call, erased):
    """Place the same valid replacement at each relevant compilation boundary."""
    return [prelude + 'section\nattribute [local csimp] replacement_eq\n' + call + 'end\n',
            prelude + 'namespace Scope\nattribute [scoped csimp] replacement_eq\nend Scope\nopen scoped Scope\n' + call,
            prelude + call + 'attribute [csimp] replacement_eq\n',
            prelude + erased + 'attribute [csimp] replacement_eq\n']


def check_primitive_history(case, primitives, schema):
    for name, operand, unary in primitives:
        arguments = 'a' if unary else 'a b'
        prelude = (f'@[noinline] def replacement ({arguments} : {operand}) := {name} {arguments}\n'
                   f'theorem replacement_eq : {name} = replacement := rfl\n')
        call = f'def run ({arguments} : {operand}) := {name} {arguments}\n'
        erased = f'def run ({arguments} : {operand}) :=\n  let _unused := {name} {arguments}\n  a\n'
        for source in rewrite_sources(prelude, call, erased):
            case.assert_rejected(source, 'rewrite|replacement|repeated compilation')
        clean = case.export(prelude + 'section\nattribute [local csimp] replacement_eq\nend\n' + call)
        case.assertEqual(clean['schema'], schema)


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1',
                     'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class UInt64AdmissionTests(unittest.TestCase):
    setUp = numeric.NumericAdmissionTests.setUp
    nodes = staticmethod(joins.JoinAdmissionTests.nodes)
    functions = reduced.ReducedAdmissionTests.functions
    calls = reduced.ReducedAdmissionTests.calls
    assert_rejected = reduced.ReducedAdmissionTests.assert_rejected

    def export(self, body, *, modern=False, probe=None):
        original_run = producer.run

        def run(command, workspace, env):
            if probe and command[-1] == 'LeanGo/Module/Harness.lean':
                harness = workspace / command[-1]
                harness.write_text(harness.read_text().replace('#lean_go_export', probe + '\n#lean_go_export'))
            return original_run(command, workspace, env)

        with mock.patch.object(producer, 'run', side_effect=run):
            return numeric.NumericAdmissionTests.export(self, body, modern=modern)

    @staticmethod
    def imported_attribute_probe(attribute, name, value):
        # Lean forbids source attributes on imported declarations. Change the
        # actual post-import environment to test the producer's own checks.
        return f'''open Lean
run_cmd do
  Lean.Elab.Command.liftCoreM do
    let name := "{name}".toName
    let some index := (← getEnv).getModuleIdxFor? name
      | throwError "Test requires an imported numeric boundary"
    modifyEnv fun env => {attribute}.ext.toEnvExtension.modifyState env fun state =>
      let entries := (state.importedEntries[index]!).filter (·.1 != name)
      let entries := entries.push (name, {value})
      let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
      {{ state with importedEntries := state.importedEntries.set! index entries }}
'''

    def test_exact_literals_and_seven_primitive_signatures(self):
        program = self.export('''structure Result where
  sum : UInt64
  difference : UInt64
  product : UInt64
  shifted : UInt64
  aboveExact : UInt64
  maximum : UInt64
  wrapped : UInt64
  eq : Bool
  lt : Bool
  le : Bool
def run (a b : UInt64) : Result :=
  ⟨a + b, a - b, a * b, a * 8, 9007199254740993,
   18446744073709551615, 18446744073709551617,
   decide (a = b), decide (a < b), decide (a ≤ b)⟩
''', modern=True)
        self.assertEqual(program['schema'], 'lean-go-ir/v6')
        self.assertEqual(program['profile'], 'pure-module/v4')
        self.assertIn('records', program['api'])
        values = [node['value'] for function in program['functions']
                  for node in self.nodes(function['body']) if node['op'] == 'let']
        literals = {value['name'] for value in values if value['kind'] == 'uint64'}
        self.assertTrue({'9007199254740993', '18446744073709551615', '1'} <= literals)
        names = {value['name'] for value in values if value['kind'] == 'primitive'}
        self.assertEqual(names, {'UInt64.' + suffix for suffix in
                                ('add', 'sub', 'mul', 'shiftLeft', 'decEq', 'decLt', 'decLe')})
        self.assertFalse(names & set(self.functions(program)))
        self.assertNotIn('UInt64', {item['name'] for item in program['types']})
        root = self.functions(program)['Numeric.run']
        self.assertEqual([param['type'] for param in root['params']], ['UInt64', 'UInt64'])
        for node in self.nodes(root['body']):
            if node['op'] == 'let' and node['value']['kind'] == 'primitive':
                expected = 'Bool' if '.dec' in node['value']['name'] else 'UInt64'
                self.assertEqual(node['bind']['type'], expected)

    def test_mixed_width_records_wrappers_and_join_keep_source_types(self):
        program = self.export('''structure Narrow where
  value : UInt32
structure Wide where
  value : UInt64
structure State where
  small : Narrow
  large : Wide
@[noinline] def add (a b : UInt64) : UInt64 := a + b
def run (flag : Bool) (state : State) (delta : Wide) : State :=
  let selected := if flag then delta else state.large
  { small := ⟨state.small.value + 1⟩,
    large := ⟨add selected.value state.large.value⟩ }
''')
        types = {item['name']: item for item in program['types']}
        for name, fields in {'Narrow': ['UInt32'], 'Wide': ['UInt64'],
                             'State': ['Numeric.Narrow', 'Numeric.Wide']}.items():
            self.assertEqual(types['Numeric.' + name]['constructors'][0]['fields'], fields)
        root = self.functions(program)['Numeric.run']
        self.assertEqual([param['type'] for param in root['params']],
                         ['Bool', 'Numeric.State', 'Numeric.Wide'])
        self.assertEqual(root['result'], 'Numeric.State')
        joins = [node['join'] for node in self.nodes(root['body']) if node['op'] == 'join']
        self.assertEqual(len(joins), 1, 'the source must retain a compiler join')
        self.assertEqual([param['type'] for param in joins[0]['params']], ['UInt64'])
        self.assertEqual(set(program['api']['records']),
                         {'Numeric.Narrow', 'Numeric.Wide', 'Numeric.State'})

    def test_captured_and_reduced_helpers_survive_with_wide_types(self):
        fixtures = {
            'capture': (local.ADD + local.SCALAR, '._lam_0', ['UInt64', 'UInt64']),
            'capture_in_reduced_owner':
                (local.ADD + local.SCALAR.replace('(bias x y : UInt32)',
                                                 '(_unused : UInt32) (bias x y : UInt32)'),
                 '._redArg._lam_0', ['UInt64', 'UInt64']),
            'reduced': (reduced.ADD + 'def run (flag : Bool) (_unused : UInt32) '
                        '(x y bias : UInt32) : UInt32 :=\n' + reduced.WORK,
                        '._redArg', ['Bool', 'UInt64', 'UInt64', 'UInt64']),
        }
        for name, (body, suffix, params) in fixtures.items():
            with self.subTest(helper=name):
                program = self.export(body.replace('UInt32', 'UInt64'))
                helper = self.functions(program)['Numeric.run' + suffix]
                self.assertEqual([param['type'] for param in helper['params']], params)
                self.assertEqual(helper['result'], 'UInt64')
                provenance = {item['name']: item for item in program['provenance']}
                self.assertEqual(provenance[helper['name']],
                                 dict(name=helper['name'], dependencies=['Numeric.run'], rewrites=[]))
                self.assertTrue(any(call['name'] == helper['name']
                                    for function in program['functions'] for call in self.calls(function)),
                                'the compiler helper must have a surviving emitted call')

    def test_classic_and_modern_imports_preserve_mixed_source_records(self):
        model = '''namespace Model
structure Packet where
  narrow : UInt32
  wide : UInt64
@[noinline] def update (packet : Packet) : Packet :=
  ⟨packet.narrow + 1, packet.wide + 9007199254740993⟩
end Model
'''
        caller = 'def run (packet : Model.Packet) : Model.Packet := Model.update packet\n'
        for modern in (False, True):
            with self.subTest(modern=modern):
                prefix = 'module\npublic section\n' if modern else ''
                (self.source / 'Model.lean').write_text(prefix + model)
                header = 'module\npublic import Model\npublic section\n' if modern else 'import Model\n'
                (self.source / 'Caller.lean').write_text(header + caller)
                producer.export_module(self.root, self.source, 'Caller', 'run', self.output)
                exported = json.loads(self.output.read_bytes())
                self.assertEqual(exported['api']['records'], {'Model.Packet': ['narrow', 'wide']})
                packet = next(item for item in exported['types'] if item['name'] == 'Model.Packet')
                self.assertEqual(packet['constructors'][0]['fields'], ['UInt32', 'UInt64'])
                self.assertIn('Model.update', self.functions(exported))
                self.assertIn('Model.update', {item['name'] for item in exported['provenance']})

    def test_mixed_record_capture_retains_complete_source_field_closure(self):
        body = '''structure Settings where
  narrow : UInt32
  wide : UInt64
@[noinline] def run (settings : Settings) (x y : UInt64) : UInt64 :=
  let bump := fun (a : UInt64) =>
    let bias := if settings.narrow == 0 then settings.wide else a
'''
        body += local.CHAIN.replace('UInt32', 'UInt64') + '  add (bump x) (bump y)\n'
        exported = self.export(local.ADD.replace('UInt32', 'UInt64') + body)
        helper = self.functions(exported)['Numeric.run._lam_0']
        self.assertEqual([param['type'] for param in helper['params']], ['Numeric.Settings', 'UInt64'])
        settings = next(item for item in exported['types'] if item['name'] == 'Numeric.Settings')
        self.assertEqual(settings['constructors'][0]['fields'], ['UInt32', 'UInt64'])
        self.assertTrue(any(call['name'] == helper['name']
                            for call in self.calls(self.functions(exported)['Numeric.run'])))

    def test_custom_add_instance_follows_its_actual_implementation(self):
        program = self.export('''structure Result where
  custom : UInt64
  direct : UInt64
instance : Add UInt64 := ⟨fun a b => UInt64.sub a b⟩
def run (a b : UInt64) : Result := ⟨a + b, UInt64.add a b⟩
''')
        operations = [node['value']['name'] for node in self.nodes(self.functions(program)['Numeric.run']['body'])
                      if node['op'] == 'let' and node['value']['kind'] == 'primitive']
        self.assertEqual(operations, ['UInt64.sub', 'UInt64.add'])

    def test_order_package_and_direct_shift_are_admitted(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                program = self.export('''def run (a b : UInt64) : UInt64 :=
  if a == b then UInt64.shiftLeft a b else if a < b then a + b else a - b
''', modern=modern)
                provenance = {item['name'] for item in program['provenance']}
                self.assertIn('instLinearOrderPackageUInt64', provenance)
                self.assertFalse(any(name.startswith('_private.Init.Data.UInt.Package.')
                                     for name in provenance))
                self.assertEqual(self.functions(program)['Numeric.run']['result'], 'UInt64')

    def test_unsupported_numeric_dependencies_reject_even_after_erasure(self):
        cases = {
            'division': 'def run (a b : UInt64) : UInt64 := a / b\n',
            'remainder': 'def run (a b : UInt64) : UInt64 := a % b\n',
            'folded_division': 'def run (a : UInt64) : UInt64 := a / 1\n',
            'erased_remainder': 'def run (a b : UInt64) : UInt64 :=\n  let _unused := a % b\n  a\n',
            'conversion': 'def run (a : UInt64) : UInt16 := a.toUInt16\n',
            'custom_foreign_instance': '@[extern "lean_go_untrusted"] opaque foreign : UInt64 → UInt64 → UInt64\n'
                                       'instance : Add UInt64 := ⟨foreign⟩\n'
                                       'def run (a b : UInt64) : UInt64 := a + b\n',
        }
        for name, body in cases.items():
            with self.subTest(case=name):
                self.assert_rejected(body, 'extern|axiom|opaque|provenance|Unsupported|unsupported')

    def test_contextual_and_late_primitive_and_support_mutations_reject(self):
        addition = '''@[noinline] def replacement (a b : UInt64) := UInt64.add a b
theorem replacement_eq : UInt64.add = replacement := rfl
'''
        equality = '''@[noinline] def replacement (a b : UInt64) : Decidable (a = b) := UInt64.decEq a b
theorem replacement_eq : UInt64.decEq = replacement := rfl
'''
        wrapper = '''def replacement : Add UInt64 := instAddUInt64
theorem replacement_eq : instAddUInt64 = replacement := rfl
'''
        call = 'def run (a b : UInt64) := a + b\n'
        erased = 'def run (a b : UInt64) :=\n  let _unused := a + b\n  a\n'
        cases = {
            'local_primitive': addition + 'section\nattribute [local csimp] replacement_eq\n' + call + 'end\n',
            'scoped_primitive': addition + 'namespace Scope\nattribute [scoped csimp] replacement_eq\n'
                                'end Scope\nopen scoped Scope\n' + call,
            'late_primitive': addition + call + 'attribute [csimp] replacement_eq\n',
            'erased_late_primitive': addition + erased + 'attribute [csimp] replacement_eq\n',
            'local_wrapper': wrapper + 'section\nattribute [local csimp] replacement_eq\n' + call + 'end\n',
            'late_order_dependency': equality + 'def run (a b : UInt64) := a == b\n'
                                     'attribute [csimp] replacement_eq\n',
            'local_order_dependency': equality + 'section\nattribute [local csimp] replacement_eq\n'
                                      'def run (a b : UInt64) := a == b\nend\n',
            'late_pristine_rewrite_dependency':
                '@[noinline] def replacement := @Nat.recCompiled\n'
                'theorem replacement_eq : @Nat.recCompiled = @replacement := rfl\n'
                'def run (a b : UInt64) := a == b\nattribute [csimp] replacement_eq\n',
        }
        for name, body in cases.items():
            with self.subTest(case=name):
                self.assert_rejected(body, 'rewrite|extern|implemented_by|replacement|initializ|repeated compilation')

    def test_imported_primitive_attributes_reject_even_after_erasure(self):
        prefix = ('@[noinline] def replacement (a b : UInt64) := UInt64.add a b\n'
                  'def initializeAdd : IO (UInt64 → UInt64 → UInt64) := pure UInt64.add\n')
        cases = {
            'extern': ('externAttr', '{ entries := [.standard `all "lean_go_untrusted"] }'),
            'implemented_by': ('Compiler.implementedByAttr', '`Numeric.replacement'),
            'initializer': ('regularInitAttr', '`Numeric.initializeAdd'),
        }
        for name, (attribute, value) in cases.items():
            for erased in (False, True):
                with self.subTest(attribute=name, erased=erased):
                    body = 'def run (a b : UInt64) := ' + (
                        '\n  let _unused := a + b\n  a\n' if erased else 'a + b\n')
                    probe = self.imported_attribute_probe(attribute, 'UInt64.add', value)
                    self.assert_rejected(prefix + body, 'Numeric source .*changed|numeric primitive', probe=probe)

    def test_literal_and_public_order_package_mutations_reject(self):
        cases = {
            'UInt64.ofNat': 'def run : UInt64 := 9007199254740993\n',
            'instLinearOrderPackageUInt64':
                'def run (a b : UInt64) : Bool := a == b\n',
            'instLinearOrderPackageUInt32':
                'def run (a b : UInt32) : Bool := a == b\n',
        }
        for name, body in cases.items():
            with self.subTest(boundary=name):
                probe = self.imported_attribute_probe(
                    'externAttr', name, '{ entries := [.standard `all "lean_go_untrusted"] }')
                self.assert_rejected(body, 'Numeric source extern changed', probe=probe)


if __name__ == '__main__':
    unittest.main()
