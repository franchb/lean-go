"""Exact byte capability selection and fresh source admission."""
import copy
import os
import unittest
import export_module as producer
import test_bitwise as bitwise
import test_uint64_admission as wide

OPS = ('add', 'sub', 'mul', 'decEq', 'decLt', 'decLe', 'land', 'lor', 'xor', 'complement', 'shiftLeft', 'shiftRight')
CASTS = (('UInt8.toUInt32', 'UInt8', 'UInt32'), ('UInt32.toUInt8', 'UInt32', 'UInt8'))

class ByteSchemaTests(unittest.TestCase):
    raw = bitwise.BitwiseSchemaTests.raw
    complete = bitwise.BitwiseSchemaTests.complete

    def test_executable_byte_types_literals_and_primitives_select_capability(self):
        for multi in (False, True):
            expected = ('lean-go-ir/v13', 'pure-module/v11') if multi else ('lean-go-ir/v12', 'pure-module/v10')
            def check(program, roots):
                result = self.complete(program, roots)
                self.assertEqual((result['schema'], result['profile']), expected)
            for name in (*(f'UInt8.{op}' for op in OPS), *(cast[0] for cast in CASTS)):
                leaf = dict(op='let', bind=dict(type='UInt32'), value=dict(kind='primitive', name=name), next=dict(op='return'))
                for body in (leaf, dict(op='join', join=dict(body=leaf), next=dict(op='jump')), dict(op='cases', alts=[dict(body=leaf)])):
                    program, roots = self.raw(body, multi)
                    check(program, roots)
            for position in ('param', 'result', 'field', 'bind', 'join', 'alt', 'literal'):
                program, roots = self.raw(dict(op='return'), multi)
                f = program['functions'][0]
                if position == 'param': f['params'] = [dict(type='UInt8')]
                if position == 'result': f['result'] = 'UInt8'
                if position == 'field': program['types'] = [dict(name='Packet', constructors=[dict(fields=['UInt8'])])]
                if position == 'bind': f['body'] = dict(op='let', bind=dict(type='UInt8'), value=dict(kind='call', name='helper'), next=dict(op='return'))
                if position == 'join': f['body'] = dict(op='join', join=dict(params=[dict(type='UInt8')], body=dict(op='return')), next=dict(op='jump'))
                if position == 'alt': f['body'] = dict(op='cases', alts=[dict(params=[dict(type='UInt8')], body=dict(op='return'))])
                if position == 'literal': f['body'] = dict(op='let', value=dict(kind='uint8', name='255'), next=dict(op='return'))
                check(program, roots)
            program, roots = self.raw(dict(op='return'), multi)
            program['provenance'] = [dict(name='UInt8.add', dependencies=['UInt8', 'UInt32.toUInt8'])]
            self.assertEqual(self.complete(program, roots)['schema'], 'lean-go-ir/v7' if multi else 'lean-go-ir/v6')
            for name in ('UInt8.add._boxed', 'UInt8.toUInt64', 'UInt64.toUInt8', 'UInt16.add', 'Fake.UInt8.add'):
                program, roots = self.raw(dict(op='let', value=dict(kind='primitive', name=name), next=dict(op='return')), multi)
                self.assertEqual(self.complete(program, roots)['schema'], 'lean-go-ir/v7' if multi else 'lean-go-ir/v6')

@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class ByteAdmissionTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export
    nodes = staticmethod(wide.UInt64AdmissionTests.nodes)
    assert_rejected = wide.UInt64AdmissionTests.assert_rejected
    imported_attribute_probe = staticmethod(wide.UInt64AdmissionTests.imported_attribute_probe)

    def test_exact_operations_notation_and_conversion_signatures(self):
        body = ("structure Result where\n" + ''.join(f'  x{i} : {"Bool" if op.startswith("dec") else "UInt8"}\n' for i, op in enumerate(OPS)) +
                '  wide : UInt32\n  narrow : UInt8\n  maximum : UInt8\n  wrapped : UInt8\n' +
                'def run (a b : UInt8) (word : UInt32) : Result :=\n  ⟨' +
                ', '.join(f'UInt8.{op} a' + ('' if op == 'complement' else ' b') for op in OPS) +
                ', a.toUInt32, word.toUInt8, 255, 257⟩\n')
        # dec* kernel results are dependent Decidable; ordinary decide supplies Bool.
        for op, prop in [('decEq', 'a = b'), ('decLt', 'a < b'), ('decLe', 'a ≤ b')]:
            body = body.replace(f'UInt8.{op} a b', f'decide ({prop})')
        for modern in (False, True):
            program = self.export(body, modern=modern)
            self.assertEqual((program['schema'], program['profile']), ('lean-go-ir/v12', 'pure-module/v10'))
            self.assertEqual(producer.emitted_primitives(program), {f'UInt8.{op}' for op in OPS} | {c[0] for c in CASTS})
            self.assertNotIn('UInt8', {t['name'] for t in program['types']})
            self.assertEqual([p['type'] for p in program['functions'][0]['params']], ['UInt8', 'UInt8', 'UInt32'])
        program = self.export('def run (a b : UInt8) := ((a + b) - a) * b ^^^ (~~~a &&& b ||| a) <<< b >>> a\n')
        self.assertTrue({'UInt8.' + op for op in ('add', 'sub', 'mul', 'land', 'lor', 'xor', 'complement', 'shiftLeft', 'shiftRight')} <= producer.emitted_primitives(program))

    def test_identity_fields_helpers_joins_and_dead_source_uses(self):
        for source in ('def run (a : UInt8) := a\n',
                       'structure Packet where\n  value : UInt8\n  word : UInt32\ndef run (p : Packet) := p\n',
                       'structure Box where\n  value : UInt8\ndef run (a : UInt8) : Box := ⟨a⟩\n',
                       '@[noinline] def helper (a b : UInt8) := a + b\ndef run (flag : Bool) (a b : UInt8) := if flag then helper a b else helper b a\n',
                       'def run (a : UInt8) :=\n  let _unused := a.toUInt32\n  a\n'):
            self.assertEqual(self.export(source)['schema'], 'lean-go-ir/v12')
        program = self.export('def run (a b : UInt32) :=\n  let _unused : UInt8 := a.toUInt8 + b.toUInt8\n  a\n')
        self.assertEqual(program['schema'], 'lean-go-ir/v6')
        self.assertFalse(producer.emitted_primitives(program))
        self.assertIn('instAddUInt8', {p['name'] for p in program['provenance']})
        (self.source / 'Numeric.lean').write_text('namespace Numeric\ndef run (a : UInt8) := a\ndef second (a : UInt32) := a.toUInt8\nend Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', ['Numeric.run', 'Numeric.second'], self.output)
        import json
        multi = json.loads(self.output.read_bytes())
        self.assertEqual((multi['schema'], multi['profile']), ('lean-go-ir/v13', 'pure-module/v11'))

    def test_unsupported_widths_aliases_and_operators_remain_rejected(self):
        for source in ('def run (a b : UInt8) := a / b\n',
                       'def run (a b : UInt8) :=\n  let _unused := a % b\n  a\n',
                       'def run (a : UInt16) := a\n',
                       'def run (a : UInt8) := a.toUInt64\n',
                       'def run (a : UInt64) := a.toUInt8\n',
                       'def run (a : UInt8) := a.toNat\n',
                       'abbrev Byte := UInt8\ndef run (a : Byte) := a\n',
                       'unsafe def run (a : UInt8) := a\n',
                       'axiom forged : UInt8\ndef run (a : UInt8) := a + forged\n'):
            self.assert_rejected(source, 'Unsupported|unsupported|extern|unsafe|Unsafe|axiom|not an inductive type|not supported by code generator')

    def test_exact_catalog_source_attributes_and_mono_signatures(self):
        # Test every new leaf in one pristine build, restoring the environment
        # around each deliberate corruption so later probes remain independent.
        probe = r'''open Lean LeanGo.Module Lean.Compiler.LCNF
run_cmd Lean.Elab.Command.liftCoreM do
  let pristine ← getEnv
  let leaves := numericPrimitives.filter fun p =>
    p.operand == ``UInt8 || p.result == ``UInt8
  unless leaves.size == 14 do throwError "byte catalog size"
  for primitive in leaves do
    validateNumericPrimitive primitive
    let some decl ← getMonoDecl? primitive.name | throwError "missing byte leaf"
    let mutations := #[{ decl with safe := false }, { decl with params := #[] },
      { decl with levelParams := [`u] }, { decl with type := .const ``UInt16 [] },
      { decl with params := decl.params.map fun p => { p with type := .const ``UInt16 [] } },
      { decl with value := .extern { entries := [.standard `all "untrusted"] } }]
    for changed in mutations do
      withEnv pristine do
        let some index := pristine.getModuleIdxFor? primitive.name | throwError "not imported"
        modifyEnv fun env => monoExt.toEnvExtension.modifyState env fun state =>
          let entries := (state.importedEntries[index]!).map fun entry =>
            if entry.name == primitive.name then changed else entry
          { state with importedEntries := state.importedEntries.set! index entries }
        let rejected ← try
          validateNumericPrimitive primitive
          pure false
        catch _ => pure true
        unless rejected do throwError "accepted mono corruption: {primitive.name}"
    for safety in #[DefinitionSafety.safe, DefinitionSafety.unsafe] do
      withEnv (← mkEmptyEnvironment) do
        addDecl <| .axiomDecl { name := `Forged, levelParams := [], type := .sort .zero, isUnsafe := false }
        addDecl <| .defnDecl { name := primitive.name, levelParams := [], type := .sort .zero, value := .const `Forged [], hints := .opaque, safety := safety }
        let message ← try
          validateNumericDefinition primitive.name
          pure ""
        catch error => error.toMessageData.toString
        let expected := if safety == .safe then "Unexpected numeric boundary axioms" else "Unsafe numeric boundary"
        unless message.startsWith expected do
          throwError "unexpected definition validation result: {primitive.name}: {message}"
  let names := leaves.map (·.name) ++ #[``UInt8.ofNat, ``instAddUInt8,
    ``instSubUInt8, ``instMulUInt8, ``instAndOpUInt8, ``instOrOpUInt8,
    ``instXorOpUInt8, ``instComplementUInt8, ``instShiftLeftUInt8,
    ``instShiftRightUInt8, ``instLinearOrderPackageUInt8, ``instDecidableEqUInt8,
    ``instLTUInt8, ``instLEUInt8]
  for name in names do
    let some index := pristine.getModuleIdxFor? name | throwError "not imported"
    for dimension in [:3] do
      withEnv pristine do
        if dimension == 0 then
          modifyEnv fun env => externAttr.ext.toEnvExtension.modifyState env fun state =>
            let entries := ((state.importedEntries[index]!).filter (·.1 != name)).push
              (name, { entries := [.standard `all "untrusted"] })
            let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
            { state with importedEntries := state.importedEntries.set! index entries }
        else if dimension == 1 then
          modifyEnv fun env => Compiler.implementedByAttr.ext.toEnvExtension.modifyState env fun state =>
            let entries := ((state.importedEntries[index]!).filter (·.1 != name)).push (name, `Untrusted)
            let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
            { state with importedEntries := state.importedEntries.set! index entries }
        else
          modifyEnv fun env => regularInitAttr.ext.toEnvExtension.modifyState env fun state =>
            let entries := ((state.importedEntries[index]!).filter (·.1 != name)).push (name, `Untrusted)
            let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
            { state with importedEntries := state.importedEntries.set! index entries }
        if (numericBoundaryIssues (← getEnv) name).isEmpty then
          throwError "accepted source corruption: {name}"
'''
        self.assertEqual(self.export('def run (a : UInt8) := a.toUInt32\n', probe=probe)['schema'], 'lean-go-ir/v12')

    def test_pristine_logical_closure_helpers_are_not_runtime_leaves(self):
        for helper, primitive, operand in (('UInt8.toNat', 'UInt8.toUInt32', 'UInt8'),
                                            ('Nat.toUInt8', 'UInt32.toUInt8', 'UInt32')):
            probe = self.imported_attribute_probe('externAttr', helper,
                                                 '{ entries := [.standard `all "untrusted"] }')
            self.assert_rejected(f'def run (a : {operand}) := {primitive} a\n',
                                 'Numeric source extern changed', probe=probe)

    def test_local_scoped_late_erased_and_closed_scope_history(self):
        wide.check_primitive_history(self, [('UInt8.toUInt32', 'UInt8', True),
                                            ('UInt32.toUInt8', 'UInt32', True),
                                            ('UInt8.shiftLeft', 'UInt8', False)], 'lean-go-ir/v12')


if __name__ == '__main__':
    unittest.main()
