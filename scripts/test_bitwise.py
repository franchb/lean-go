"""Executable schema selection, source admission and independent native bitwise checks."""
import copy
import json
import os
from pathlib import Path
import tempfile
import unittest

import export_module as producer
import generate
from unittest import mock
import test_uint64_admission as wide
from test_proof_contracts import ContractFixture
from differential_model import Expr, Program, deserialize, evaluate, render_lean, serialize

OPERATIONS = ('land', 'lor', 'xor', 'complement', 'shiftRight')


class BitwiseSchemaTests(unittest.TestCase):
    def raw(self, body, multi=False):
        program = dict(module='Example', functions=[dict(name='Example.run', body=body)],
                       types=[], claims=[], rewrites=[], provenance=[], api={'records': {}})
        roots = ['Example.run', 'Example.second'] if multi else ['Example.run']
        program['roots' if multi else 'root'] = roots if multi else roots[0]
        return program, roots

    def complete(self, program, roots):
        return producer.complete_program(json.dumps(program).encode(), 'Example', roots, {})

    def test_only_emitted_primitive_nodes_select_new_schema(self):
        value = dict(kind='primitive', name='UInt64.complement', args=[1])
        leaf = dict(op='let', value=value, next=dict(op='return', var=2))
        bodies = [leaf, dict(op='join', join=dict(body=leaf), next=dict(op='jump')),
                  dict(op='cases', alts=[dict(body=leaf)])]
        for multi in (False, True):
            for body in bodies:
                program, roots = self.raw(body, multi)
                result = self.complete(program, roots)
                self.assertEqual((result['schema'], result['profile']),
                                 ('lean-go-ir/v9', 'pure-module/v7') if multi else
                                 ('lean-go-ir/v8', 'pure-module/v6'))
            # Source evidence and call names do not authorize an executable leaf.
            for kind in ('call', 'constructor'):
                body = copy.deepcopy(leaf)
                body['value']['kind'] = kind
                program, roots = self.raw(body, multi)
                program['provenance'] = [dict(name='UInt64.complement')]
                result = self.complete(program, roots)
                self.assertEqual(result['schema'], 'lean-go-ir/v7' if multi else 'lean-go-ir/v6')
            program, roots = self.raw(dict(op='return', var=1), multi)
            program['functions'].append(dict(name='Example.helper', body=leaf))
            self.assertEqual(self.complete(program, roots)['schema'],
                             'lean-go-ir/v9' if multi else 'lean-go-ir/v8')

    def test_exact_names_and_all_operations(self):
        for width in (32, 64):
            for op in OPERATIONS:
                body = dict(op='let', value=dict(kind='primitive', name=f'UInt{width}.{op}', args=[]),
                            next=dict(op='return', var=1))
                program, roots = self.raw(body)
                self.assertEqual(self.complete(program, roots)['schema'], 'lean-go-ir/v8')
        for name in ('UInt32.land._boxed', 'UInt16.land', 'UInt32.add', 'UInt64.shiftLeft'):
            body['value']['name'] = name
            program, roots = self.raw(body)
            self.assertEqual(self.complete(program, roots)['schema'], 'lean-go-ir/v6')

    def test_new_schemas_keep_module_freshness_and_malformed_error(self):
        for schema in ('lean-go-ir/v8', 'lean-go-ir/v9'):
            with mock.patch.object(generate, 'verify_module_inputs') as verify:
                generate.verify_inputs(Path('/repo'), {'schema': schema}, Path('/source'))
                verify.assert_called_once()
        for body in (None, {}, dict(op='let'), dict(op='join', join=[]), dict(op='cases', alts=None)):
            program, roots = self.raw(body)
            with self.assertRaises(producer.ExportError):
                self.complete(program, roots)


class BitwiseManifestTests(ContractFixture):
    def test_exact_manifest_ir_profile_triples(self):
        for multi in (False, True):
            self.document['roots'] = ['Example.identity', 'Example.second'] if multi else ['Example.identity']
            self.write_contract()
            self.write_manifest()
            accepted = ((7, 5), (9, 7), (11, 9)) if multi else ((6, 4), (8, 6), (10, 8))
            for manifest_version in range(6, 12):
                for ir_version in range(6, 12):
                    for profile in range(4, 10):
                        self.manifest['schema'] = f'lean-go-manifest/v{manifest_version}'
                        self.manifest['program'].update(schema=f'lean-go-ir/v{ir_version}',
                                                        profile=f'pure-module/v{profile}')
                        self.save_manifest()
                        if manifest_version == ir_version and (ir_version, profile) in accepted:
                            self.verify_manifest()
                        else:
                            with self.assertRaises(producer.ExportError):
                                self.verify_manifest()
            for schema in (None, [], {}, 8, True):
                self.manifest['schema'] = schema
                self.save_manifest()
                with self.assertRaises(producer.ExportError):
                    self.verify_manifest()


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class BitwiseAdmissionTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export
    nodes = staticmethod(wide.UInt64AdmissionTests.nodes)
    assert_rejected = wide.UInt64AdmissionTests.assert_rejected
    imported_attribute_probe = staticmethod(wide.UInt64AdmissionTests.imported_attribute_probe)

    def test_notation_and_direct_calls_have_exact_signatures(self):
        for direct in (False, True):
            fields, values = [], []
            for width in (32, 64):
                a, b = f'a{width}', f'b{width}'
                expressions = [f'{a} &&& {b}', f'{a} ||| {b}', f'{a} ^^^ {b}', f'~~~{a}', f'{a} >>> {b}']
                for op, notation in zip(OPERATIONS, expressions):
                    fields.append(f'  {op}{width} : UInt{width}')
                    values.append(f'UInt{width}.{op} {a}' + ('' if op == 'complement' else f' {b}') if direct else notation)
            source = ('structure Result where\n' + '\n'.join(fields) + '\n'
                      'def run (a32 b32 : UInt32) (a64 b64 : UInt64) : Result :=\n  ⟨' + ', '.join(values) + '⟩\n')
            program = self.export(source, modern=direct)
            self.assertEqual((program['schema'], program['profile']), ('lean-go-ir/v8', 'pure-module/v6'))
            leaves = [node['value'] for function in program['functions'] for node in self.nodes(function['body'])
                      if node['op'] == 'let' and node['value']['kind'] == 'primitive']
            self.assertEqual({leaf['name'] for leaf in leaves},
                             {f'UInt{width}.{op}' for width in (32, 64) for op in OPERATIONS})
            for leaf in leaves:
                self.assertEqual(len(leaf['args']), 1 if leaf['name'].endswith('.complement') else 2)

    def test_source_only_bitwise_and_multi_root_selection(self):
        program = self.export('def run (a b : UInt32) : UInt32 :=\n  let _unused := a &&& b\n  a\n')
        self.assertEqual(program['schema'], 'lean-go-ir/v6')
        # Provenance stops at the trusted wrapper; its pristine closure audits land.
        self.assertIn('instAndOpUInt32', {item['name'] for item in program['provenance']})
        self.assertFalse(producer.emitted_primitives(program) & producer.BITWISE_PRIMITIVES)
        (self.source / 'Numeric.lean').write_text('namespace Numeric\ndef run (a : UInt32) := ~~~a\ndef second (a : UInt64) := a >>> 64\nend Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', ['Numeric.second', 'Numeric.run'], self.output)
        program = json.loads(self.output.read_bytes())
        self.assertEqual((program['schema'], program['profile']), ('lean-go-ir/v9', 'pure-module/v7'))
        self.assertEqual(program['roots'], ['Numeric.run', 'Numeric.second'])

    def test_exact_primitive_and_wrapper_mutations_reject(self):
        for width in (32, 64):
            for name, expression in ((f'UInt{width}.land', 'a &&& b'),
                                     (f'UInt{width}.complement', '~~~a'),
                                     (f'UInt{width}.shiftRight', 'a >>> b'),
                                     (f'instAndOpUInt{width}', 'a &&& b'),
                                     (f'instComplementUInt{width}', '~~~a'),
                                     (f'instShiftRightUInt{width}', 'a >>> b')):
                with self.subTest(name=name):
                    source = f'def run (a b : UInt{width}) := {expression}\n'
                    probe = self.imported_attribute_probe('externAttr', name,
                                                         '{ entries := [.standard `all "untrusted"] }')
                    self.assert_rejected(source, 'Numeric source extern changed|numeric primitive', probe=probe)

    def test_local_scoped_late_and_erased_replacements_reject(self):
        for width in (32, 64):
            primitive = (f'@[noinline] def replacement (a : UInt{width}) := UInt{width}.complement a\n'
                         f'theorem replacement_eq : UInt{width}.complement = replacement := rfl\n')
            wrapper = (f'def replacement : AndOp UInt{width} := instAndOpUInt{width}\n'
                       f'theorem replacement_eq : instAndOpUInt{width} = replacement := rfl\n')
            for prelude, expression in ((primitive, '~~~a'), (wrapper, 'a &&& b')):
                call = f'def run (a b : UInt{width}) := {expression}\n'
                erased = f'def run (a b : UInt{width}) :=\n  let _unused := {expression}\n  a\n'
                sources = [prelude + 'section\nattribute [local csimp] replacement_eq\n' + call + 'end\n',
                           prelude + 'namespace Scope\nattribute [scoped csimp] replacement_eq\nend Scope\nopen scoped Scope\n' + call,
                           prelude + call + 'attribute [csimp] replacement_eq\n',
                           prelude + erased + 'attribute [csimp] replacement_eq\n']
                for source in sources:
                    self.assert_rejected(source, 'rewrite|replacement|repeated compilation')

    def test_unsupported_width_conversions_division_remainder_and_forged_axiom(self):
        for source in ('def run (a b : UInt16) := a &&& b\n',
                       'def run (a : UInt64) := (~~~a).toUInt16\n',
                       'def run (a b : UInt32) := (a &&& b) / b\n',
                       'def run (a b : UInt64) := (a ||| b) % b\n',
                       'axiom forged : UInt32\ndef run := ~~~forged\n'):
            self.assert_rejected(source, 'extern|axiom|opaque|Unsupported|unsupported|not supported by code generator')


class BitwiseModelTests(unittest.TestCase):
    def test_arithmetic_oracle_and_saved_ast_roundtrip(self):
        for width in (32, 64):
            maximum = 2**width - 1
            wants = {'land': 0, 'lor': maximum, 'xor': maximum,
                     'complement': maximum - 0xaaaaaaaa, 'shr': 0x55555555}
            for operation, want in wants.items():
                args = (Expr('a'),) if operation == 'complement' else (Expr('a'), Expr('b'))
                program = Program(0, width, Expr(operation, args), Expr('a'))
                b = 1 if operation == 'shr' else maximum - 0xaaaaaaaa
                self.assertEqual(evaluate(program, 0xaaaaaaaa, b), want)
                self.assertEqual(deserialize(serialize(program)), program)
                self.assertIn('def run', render_lean(program))


@unittest.skipUnless(os.environ.get('LEAN_GO_DIFFERENTIAL') == '1', 'set LEAN_GO_DIFFERENTIAL=1 for native Lean comparisons')
class BitwiseNativeTests(unittest.TestCase):
    def test_all_primitives_at_width_boundaries(self):
        from differential_backend import Backend
        from differential_cases import compare, save_case
        with tempfile.TemporaryDirectory(prefix='lean-go-bitwise-native-') as directory:
            artifacts = Path(os.environ.get('LEAN_GO_BITWISE_ARTIFACTS', directory))
            runner = Backend(Path(__file__).resolve().parents[1], artifacts)
            for width in (32, 64):
                maximum = 2**width - 1
                alternating = sum(2**i for i in range(0, width, 2))
                values = (0, maximum, 2**(width-1), alternating, maximum-alternating)
                pairs = [(a, b) for a in values for b in (0, width-1, width, width+1, maximum)]
                pairs.extend((a, b) for a in values for b in values)
                for op in ('land', 'lor', 'xor', 'complement', 'shr'):
                    with self.subTest(width=width, op=op):
                        args = (Expr('a'),) if op == 'complement' else (Expr('a'), Expr('b'))
                        program = Program(0, width, Expr(op, args), Expr('a'))
                        try:
                            observation = compare(program, pairs, runner.check(program, pairs))
                        except Exception as error:
                            if runner.last_work is not None:
                                save_case(runner.last_work / 'case.json', program, pairs, error=error)
                            raise
                        save_case(runner.last_work / 'case.json', program, pairs, observation)
                        self.assertEqual(observation.signature, ())


if __name__ == '__main__':
    unittest.main()
