"""Ordinary left-shift notation uses the audited, existing scalar primitives."""
import json
import os
from pathlib import Path
import random
import tempfile
import unittest
from unittest import mock

import export_module as producer
import test_uint64_admission as wide
from differential_model import Expr, Program, render_lean

WRAPPERS = ('HShiftLeft.hShiftLeft', 'instHShiftLeftOfShiftLeft', 'ShiftLeft.shiftLeft',
            'instShiftLeftUInt32', 'instShiftLeftUInt64')


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class ShiftLeftAdmissionTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export
    nodes = staticmethod(wide.UInt64AdmissionTests.nodes)
    assert_rejected = wide.UInt64AdmissionTests.assert_rejected
    imported_attribute_probe = staticmethod(wide.UInt64AdmissionTests.imported_attribute_probe)

    def test_direct_and_notation_match_at_both_widths_and_module_modes(self):
        body = ('structure Result where\n  small : UInt32\n  large : UInt64\n'
                'def run (a b : UInt32) (c d : UInt64) : Result := ⟨%s, %s⟩\n')
        for modern in (False, True):
            direct = self.export(body % ('UInt32.shiftLeft a b', 'UInt64.shiftLeft c d'), modern=modern)
            notation = self.export(body % ('a <<< b', 'c <<< d'), modern=modern)
            for key in ('types', 'functions', 'root', 'api', 'schema', 'profile', 'claims', 'rewrites', 'stage'):
                self.assertEqual(direct[key], notation[key], key)
            self.assertEqual((notation['schema'], notation['profile']), ('lean-go-ir/v6', 'pure-module/v4'))
            self.assertEqual(producer.emitted_primitives(notation), {'UInt32.shiftLeft', 'UInt64.shiftLeft'})
            self.assertTrue(set(WRAPPERS) - {'ShiftLeft.shiftLeft'} <=
                            {item['name'] for item in notation['provenance']})

    def test_multi_root_helper_and_cases_keep_existing_schema(self):
        (self.source / 'Numeric.lean').write_text('''namespace Numeric
@[noinline] def helper (a b : UInt64) : UInt64 := a <<< b
def run (flag : Bool) (a b : UInt64) := if flag then helper a b else helper b a
def second (a b : UInt32) := a <<< b
end Numeric
''')
        producer.export_module(self.root, self.source, 'Numeric', ['Numeric.second', 'Numeric.run'], self.output)
        program = json.loads(self.output.read_bytes())
        self.assertEqual((program['schema'], program['profile']), ('lean-go-ir/v7', 'pure-module/v5'))
        self.assertEqual(program['roots'], ['Numeric.run', 'Numeric.second'])
        self.assertEqual(producer.emitted_primitives(program), {'UInt32.shiftLeft', 'UInt64.shiftLeft'})
        self.assertTrue(any(node['op'] == 'cases' for fn in program['functions'] for node in self.nodes(fn['body'])))
        self.assertTrue(any(fn['name'] == 'Numeric.helper' for fn in program['functions']))

    def test_erased_and_dead_uses_do_not_activate_a_capability(self):
        for width in (32, 64):
            program = self.export(f'''def dead (a b : UInt{width}) := a <<< b
def run (a b : UInt{width}) :=
  let _unused := a <<< b
  a
''')
            self.assertEqual((program['schema'], program['profile']), ('lean-go-ir/v6', 'pure-module/v4'))
            self.assertFalse(producer.emitted_primitives(program))
            self.assertIn(f'instShiftLeftUInt{width}', {item['name'] for item in program['provenance']})
            self.assertNotIn('Numeric.dead', {fn['name'] for fn in program['functions']})

    def test_custom_instance_follows_addition_instead_of_syntax(self):
        program = self.export('''instance : ShiftLeft UInt32 := ⟨fun a b => UInt32.add a b⟩
def run (a b : UInt32) := a <<< b
''')
        self.assertEqual(producer.emitted_primitives(program), {'UInt32.add'})

    def test_exact_wrapper_and_leaf_extern_mutations_reject(self):
        for name in (*WRAPPERS, 'UInt32.shiftLeft', 'UInt64.shiftLeft'):
            width = 64 if name.endswith('64') or name.startswith('UInt64.') else 32
            probe = self.imported_attribute_probe('externAttr', name, '{ entries := [.standard `all "untrusted"] }')
            with self.subTest(boundary=name):
                self.assert_rejected(f'def run (a b : UInt{width}) := a <<< b\n',
                                     'Numeric source extern changed|numeric primitive', probe=probe)

    def test_implemented_by_and_initialization_reject_after_erasure_too(self):
        for width in (32, 64):
            for name, type_ in ((f'UInt{width}.shiftLeft', f'UInt{width} → UInt{width} → UInt{width}'),
                                (f'instShiftLeftUInt{width}', f'ShiftLeft UInt{width}')):
                prefix = f'def replacement : {type_} := {name}\ndef initializeShift : IO ({type_}) := pure {name}\n'
                for attribute, value in (('Compiler.implementedByAttr', '`Numeric.replacement'),
                                         ('regularInitAttr', '`Numeric.initializeShift')):
                    for erased in (False, True):
                        body = f'def run (a b : UInt{width}) := ' + ('\n  let _unused := a <<< b\n  a\n' if erased else 'a <<< b\n')
                        with self.subTest(boundary=name, attribute=attribute, erased=erased):
                            self.assert_rejected(prefix + body, 'Numeric source .*changed|numeric primitive',
                                                 probe=self.imported_attribute_probe(attribute, name, value))

    def test_exact_wrapper_safety_and_axiom_baselines(self):
        for name in (*WRAPPERS, 'UInt32.shiftLeft', 'UInt64.shiftLeft'):
            for safety, diagnostic in (('unsafe', 'Unsafe numeric boundary'),
                                       ('safe', 'Unexpected numeric boundary axioms')):
                probe = wide.definition_baseline_probe(name, safety)
                with self.subTest(boundary=name, safety=safety):
                    self.assert_rejected('def run (a b : UInt32) := a <<< b\n', diagnostic, probe=probe)

    def test_local_scoped_late_erased_and_closed_scope_rewrite_history(self):
        for width in (32, 64):
            for name, type_ in ((f'UInt{width}.shiftLeft', f'UInt{width} → UInt{width} → UInt{width}'),
                                (f'instShiftLeftUInt{width}', f'ShiftLeft UInt{width}')):
                prelude = (f'@[noinline] def replacement : {type_} := {name}\n'
                           f'theorem replacement_eq : {name} = replacement := rfl\n')
                call = f'def run (a b : UInt{width}) := a <<< b\n'
                erased = f'def run (a b : UInt{width}) :=\n  let _unused := a <<< b\n  a\n'
                cases = wide.rewrite_sources(prelude, call, erased)
                for index, body in enumerate(cases):
                    with self.subTest(boundary=name, context=index):
                        self.assert_rejected(body, 'rewrite|replacement|repeated compilation')
                clean = self.export(prelude + 'section\nattribute [local csimp] replacement_eq\nend\n' + call)
                self.assertEqual(producer.emitted_primitives(clean), {f'UInt{width}.shiftLeft'})

    def test_pristine_closure_and_unsupported_widths_are_not_runtime_admission(self):
        for width in (32, 64):
            probe = self.imported_attribute_probe('externAttr', f'UInt{width}.mod',
                                                 '{ entries := [.standard `all "untrusted"] }')
            self.assert_rejected(f'def run (a b : UInt{width}) := a <<< b\n', 'Numeric source extern changed', probe=probe)
            self.assert_rejected(f'def run (a b : UInt{width}) := UInt{width}.mod a b\n', 'extern|Unsupported|unsupported')
        for width in ('UInt16', 'USize'):
            self.assert_rejected(f'def run (a b : {width}) := a <<< b\n',
                                 'extern|axiom|opaque|Unsupported|unsupported|not supported by code generator')
        self.assert_rejected('axiom forged : UInt32\ndef run (b : UInt32) := forged <<< b\n', 'axiom|not supported by code generator')


@unittest.skipUnless(os.environ.get('LEAN_GO_DIFFERENTIAL') == '1', 'set LEAN_GO_DIFFERENTIAL=1 for native Lean comparisons')
class ShiftLeftNativeTests(unittest.TestCase):
    def test_notation_and_direct_calls_match_native_raw_typed_and_arithmetic(self):
        from differential_backend import Backend
        from differential_cases import compare, save_case
        with tempfile.TemporaryDirectory(prefix='lean-go-shift-left-native-') as directory:
            runner = Backend(Path(__file__).resolve().parents[1], Path(os.environ.get('LEAN_GO_SHIFT_ARTIFACTS', directory)))
            for width in (32, 64):
                maximum = 2**width - 1
                alternating = sum(2**i for i in range(0, width, 2))
                values = (0, 1, maximum, 2**(width-1), alternating, maximum-alternating)
                pairs = [(a, b) for a in values for b in (0, width-1, width, width+1, maximum)]
                rng = random.Random(3072 + width)
                pairs.extend((rng.randrange(maximum+1), rng.randrange(maximum+1)) for _ in range(32))
                program = Program(0, width, Expr('shl', (Expr('a'), Expr('b'))), Expr('a'))
                source = render_lean(program)
                results = []
                for direct in (False, True):
                    rendered = source.replace('(a <<< b)', f'(UInt{width}.shiftLeft a b)') if direct else source
                    with mock.patch('differential_model.render_lean', return_value=rendered):
                        result = runner.check(program, pairs)
                    observation = compare(program, pairs, result)
                    save_case(runner.last_work / 'case.json', program, pairs, observation)
                    self.assertEqual(observation.signature, ())
                    ir = json.loads((runner.last_work / 'program.ir.json').read_bytes())
                    self.assertEqual(producer.emitted_primitives(ir), {f'UInt{width}.shiftLeft'})
                    self.assertEqual((ir['schema'], ir['profile']), ('lean-go-ir/v6', 'pure-module/v4'))
                    results.append(result)
                self.assertEqual(*results)


if __name__ == '__main__':
    unittest.main()
