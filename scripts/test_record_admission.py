"""Opt-in fresh Lean checks for source-shaped record representation adapters."""
import json
import os
from pathlib import Path
import tempfile
import unittest

import export_module as producer


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1',
                     'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class RecordAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.directory = Path(self.enterContext(
            tempfile.TemporaryDirectory(prefix='lean-go-record-admission-')))
        self.source = self.directory / 'source'
        self.source.mkdir()
        self.output = self.directory / 'program.json'
        self.root = Path(__file__).resolve().parents[1]

    def export(self, body, *, modern=False):
        prefix = 'module\npublic section\n' if modern else ''
        (self.source / 'Records.lean').write_text(
            prefix + 'namespace Records\n' + body + '\nend Records\n')
        producer.export_module(self.root, self.source, 'Records', 'Records.run', self.output)
        return json.loads(self.output.read_bytes())

    @staticmethod
    def nodes(code):
        yield code
        if code['op'] == 'let':
            yield from RecordAdmissionTests.nodes(code['next'])
        elif code['op'] == 'cases':
            for alternative in code['alts']:
                yield from RecordAdmissionTests.nodes(alternative['body'])

    def assert_source_boundary(self, function, source_type):
        self.assertEqual([param['type'] for param in function['params']], [source_type])
        self.assertEqual(function['result'], source_type)
        nodes = list(self.nodes(function['body']))
        self.assertTrue(any(node['op'] == 'cases' and node['type'] == source_type
                            for node in nodes), source_type + ' must be unwrapped')
        self.assertTrue(any(node['op'] == 'let' and
                            node['value']['kind'] == 'constructor' and
                            node['value']['name'] == source_type + '.mk' and
                            node['bind']['type'] == source_type for node in nodes),
                        source_type + ' must be reconstructed')
        self.assertTrue(all(node['result'] == source_type for node in nodes
                            if node['op'] == 'cases'))

    def assert_typed_boundaries(self, program):
        constructors = {constructor['name']: constructor['fields']
                        for declaration in program['types']
                        for constructor in declaration['constructors']}
        functions = {function['name']: function for function in program['functions']}
        for function in functions.values():
            nodes = list(self.nodes(function['body']))
            binders = list(function['params'])
            for node in nodes:
                if node['op'] == 'let':
                    binders.append(node['bind'])
                elif node['op'] == 'cases':
                    for alternative in node['alts']:
                        binders.extend(alternative['params'])
                        self.assertEqual([p['type'] for p in alternative['params']],
                                         constructors[alternative['constructor']])
            types = {binder['id']: binder['type'] for binder in binders}
            self.assertEqual(len(types), len(binders), 'adapter binders must be fresh')
            for node in nodes:
                if node['op'] != 'let':
                    continue
                value = node['value']
                if value['kind'] == 'constructor':
                    expected = constructors[value['name']]
                elif value['kind'] == 'call':
                    target = functions[value['name']]
                    expected = [param['type'] for param in target['params']]
                    self.assertEqual(node['bind']['type'], target['result'])
                else:
                    continue
                self.assertEqual([types[arg] for arg in value['args']], expected)

    def assert_source_provenance(self, program, names):
        records = {item['name'] for item in program['provenance']}
        self.assertTrue(set(names) <= records, set(names) - records)
        self.assertTrue({item['name'] for item in program['functions']} <= records)
        self.assertEqual(program['schema'], 'lean-go-ir/v3')
        self.assertEqual(program['profile'], 'pure-module/v2')
        self.assert_typed_boundaries(program)

    def assert_rejected(self, body, diagnostic):
        self.output.write_bytes(b'previous output\n')
        with self.assertRaisesRegex(producer.ExportError, diagnostic) as rejected:
            self.export(body)
        self.assertNotRegex(str(rejected.exception),
                            'unexpected token|unknown identifier|unknown attribute|'
                            'unsolved goals|type mismatch|maximum recursion depth|'
                            'stack overflow|RecursionError')
        self.assertEqual(self.output.read_bytes(), b'previous output\n')

    def test_single_field_parameter_return_and_noinline_call_keep_source_types(self):
        program = self.export('''structure Count where
  value : UInt32
@[noinline] def increment (x : Count) : Count := ⟨x.value + 1⟩
def run (x : Count) : Count := increment x
''')
        functions = {item['name']: item for item in program['functions']}
        self.assertEqual(set(functions), {'Records.run', 'Records.increment'})
        for function in functions.values():
            self.assert_source_boundary(function, 'Records.Count')
        self.assert_source_provenance(program, ['Records.run', 'Records.increment', 'Records.Count.value'])
        count = next(item for item in program['types'] if item['name'] == 'Records.Count')
        self.assertEqual(count['constructors'], [
            dict(name='Records.Count.mk', tag=0, fields=['UInt32'])])

    def test_nested_nominal_bool_fields_and_record_update_keep_original_schema(self):
        program = self.export("""structure Count where
  value : UInt32
structure Other where
  value : UInt32
structure Nested where
  value : Count
structure Flag where
  value : Bool
structure Pair where
  count : Count
  other : Other
  nested : Nested
  enabled : Flag
@[noinline] def combine (x : Count) (y : Other) : Other := ⟨x.value + y.value⟩
@[noinline] def bumpNested (x : Nested) : Nested := ⟨⟨x.value.value + 3⟩⟩
@[noinline] def toggle (x : Flag) : Flag := ⟨!x.value⟩
def run (x : Pair) : Pair :=
  { x with
    other := combine x.count x.other
    nested := bumpNested x.nested
    enabled := toggle x.enabled }
""")
        types = {item['name']: item for item in program['types']}
        expected = {
            'Records.Count': ['UInt32'],
            'Records.Other': ['UInt32'],
            'Records.Nested': ['Records.Count'],
            'Records.Flag': ['Bool'],
            'Records.Pair': ['Records.Count', 'Records.Other', 'Records.Nested', 'Records.Flag'],
        }
        self.assertEqual(set(types), set(expected) | {'Bool'})
        for name, fields in expected.items():
            self.assertEqual(types[name]['constructors'],
                             [dict(name=name + '.mk', tag=0, fields=fields)])
        functions = {item['name']: item for item in program['functions']}
        self.assertTrue({'Records.run', 'Records.combine', 'Records.bumpNested', 'Records.toggle'} <= set(functions))
        self.assertTrue(set(functions) <= {'Records.run', 'Records.combine', 'Records.bumpNested',
                                           'Records.toggle', 'Bool.Internal.not'})
        self.assert_source_boundary(functions['Records.bumpNested'], 'Records.Nested')
        self.assert_source_boundary(functions['Records.toggle'], 'Records.Flag')
        helper = functions['Records.combine']
        self.assertEqual([p['type'] for p in helper['params']], ['Records.Count', 'Records.Other'])
        self.assertEqual(helper['result'], 'Records.Other')
        root = functions['Records.run']
        self.assertEqual([p['type'] for p in root['params']], ['Records.Pair'])
        self.assertEqual(root['result'], 'Records.Pair')
        nodes = list(self.nodes(root['body']))
        pair_case = next(node for node in nodes if node['op'] == 'cases' and node['type'] == 'Records.Pair')
        self.assertEqual([p['type'] for p in pair_case['alts'][0]['params']], expected['Records.Pair'])
        reconstructed = {node['value']['name'] for node in nodes
                         if node['op'] == 'let' and node['value']['kind'] == 'constructor'}
        self.assertTrue({name + '.mk' for name in expected} <= reconstructed)
        self.assert_source_provenance(program, ['Records.run', 'Records.combine',
                                              'Records.Count.value', 'Records.Other.value',
                                              'Records.Nested.value', 'Records.Flag.value',
                                              'Records.Pair.count', 'Records.Pair.other',
                                              'Records.Pair.nested', 'Records.Pair.enabled'])

    def test_wrapper_around_recursive_sum_keeps_sum_and_wrapper_types(self):
        program = self.export("""inductive Tree where
  | leaf (value : UInt32)
  | branch (left right : Tree)
structure Box where
  value : Tree
@[noinline] def transform (x : Box) : Box :=
  match x.value with
  | .leaf value => ⟨.leaf (value + 1)⟩
  | .branch left right => ⟨.branch right left⟩
def run (x : Box) : Box := transform x
""")
        types = {item['name']: item for item in program['types']}
        self.assertEqual(set(types), {'Records.Box', 'Records.Tree'})
        self.assertEqual(types['Records.Box']['constructors'], [
            dict(name='Records.Box.mk', tag=0, fields=['Records.Tree'])])
        self.assertEqual(types['Records.Tree']['constructors'], [
            dict(name='Records.Tree.leaf', tag=0, fields=['UInt32']),
            dict(name='Records.Tree.branch', tag=1, fields=['Records.Tree', 'Records.Tree'])])
        functions = {item['name']: item for item in program['functions']}
        self.assertEqual(set(functions), {'Records.run', 'Records.transform'})
        for function in functions.values():
            self.assert_source_boundary(function, 'Records.Box')
        nodes = list(self.nodes(functions['Records.transform']['body']))
        tree_case = next(node for node in nodes if node['op'] == 'cases' and node['type'] == 'Records.Tree')
        self.assertEqual(tree_case['result'], 'Records.Box')
        self.assertEqual([alt['constructor'] for alt in tree_case['alts']],
                         ['Records.Tree.leaf', 'Records.Tree.branch'])
        self.assert_source_provenance(program, ['Records.run', 'Records.transform', 'Records.Box.value'])

    def test_recursive_single_field_stays_an_ordinary_recursive_adt(self):
        # Lean does not erase this recursive constructor. Do not start unboxing
        # its field forever or reject the previously supported source type.
        program = self.export("""inductive Loop where
  | next : Loop → Loop
def run (x : Loop) : Loop := x
""")
        self.assertEqual(program['types'], [dict(name='Records.Loop', constructors=[
            dict(name='Records.Loop.next', tag=0, fields=['Records.Loop'])])])
        function = program['functions'][0]
        self.assertEqual([p['type'] for p in function['params']], ['Records.Loop'])
        self.assertEqual(function['result'], 'Records.Loop')
        self.assertEqual(function['body'], dict(op='return', var=function['params'][0]['id']))

    def test_classic_and_modern_imported_record_helpers_keep_source_provenance(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                prefix = 'module\npublic section\n' if modern else ''
                (self.source / 'Model.lean').write_text(prefix + """namespace Model
structure Count where
  value : UInt32
@[noinline] def increment (x : Count) : Count := ⟨x.value + 1⟩
end Model
""")
                header = 'module\npublic import Model\npublic section\n' if modern else 'import Model\n'
                (self.source / 'Caller.lean').write_text(header + """namespace Caller
def run (x : Model.Count) : Model.Count := Model.increment x
end Caller
""")
                producer.export_module(self.root, self.source, 'Caller', 'Caller.run', self.output)
                program = json.loads(self.output.read_bytes())
                functions = {item['name']: item for item in program['functions']}
                self.assertEqual(set(functions), {'Caller.run', 'Model.increment'})
                for function in functions.values():
                    self.assert_source_boundary(function, 'Model.Count')
                self.assertEqual(program['types'], [dict(name='Model.Count', constructors=[
                    dict(name='Model.Count.mk', tag=0, fields=['UInt32'])])])
                self.assertTrue({'source/Model.lean', 'source/Caller.lean'} <= set(program['inputs']))
                self.assert_source_provenance(program, ['Caller.run', 'Model.increment', 'Model.Count.value'])

    def test_unsupported_source_fields_preserve_output(self):
        cases = {
            'proof_field': ("""structure Checked where
  value : UInt32
  witness : value = value
def run (x : Checked) : Checked := x
""", 'Unsupported|unsupported|proof|source field|wrapper'),
            'generic': ("""structure Generic (α : Type) where
  value : α
def run (x : Generic UInt32) : Generic UInt32 := x
""", 'monomorphic|Unsupported|unsupported|parameter|universe'),
            'dependent': ("""structure Dependent where
  count : Nat
  value : Fin count
def run (x : Dependent) : Dependent := x
""", 'monomorphic|Dependent|dependent|Unsupported|unsupported'),
            'uint64': ("""structure Wide where
  value : UInt64
def run (x : Wide) : Wide := x
""", 'UInt64|Unsupported|unsupported'),
        }
        for name, (body, diagnostic) in cases.items():
            with self.subTest(case=name):
                self.assert_rejected(body, diagnostic)

    def test_wrapper_depth_boundary_uses_bounded_admission(self):
        body = 'set_option maxRecDepth 4096\n'
        field = 'UInt32'
        for depth in range(64):
            body += f'structure Wrapper{depth} where\n  value : {field}\n'
            field = f'Wrapper{depth}'
        program = self.export(body + f'def run (x : {field}) : {field} := x\n')
        self.assertEqual(len(program['types']), 64)
        self.assert_source_boundary(program['functions'][0], 'Records.Wrapper63')
        self.assert_source_provenance(program, ['Records.run'])
        body += f'structure Wrapper64 where\n  value : {field}\n'
        body += 'def run (x : Wrapper64) : Wrapper64 := x\n'
        self.assert_rejected(body, '[Ww]rapper.*(depth|limit)|representation.*(depth|limit)')

    def test_foreign_and_axiomatic_record_helpers_preserve_output(self):
        record = 'structure Count where\n  value : UInt32\n'
        cases = {
            'foreign': ('@[extern "lean_go_untrusted"] opaque foreign : UInt32 → UInt32\n'
                        '@[noinline] def helper (x : Count) : Count := ⟨foreign x.value⟩\n',
                        'extern|External function|opaque'),
            'axiom': ('axiom forged : True\n'
                      '@[noinline] def helper (x : Count) : Count :=\n'
                      '  let _proof : True := forged\n  x\n',
                      '[Aa]xiom|provenance'),
        }
        for name, (helper, diagnostic) in cases.items():
            with self.subTest(case=name):
                self.assert_rejected(record + helper + 'def run (x : Count) : Count := helper x\n', diagnostic)

    def test_context_sensitive_accessor_and_helper_rewrites_are_rejected(self):
        record = 'structure Count where\n  value : UInt32\n'
        for target in ('Count.value', 'helper'):
            definitions = record
            if target == 'helper':
                definitions += '@[noinline] def helper (x : Count) : UInt32 := x.value\n'
            definitions += (f'@[noinline] def replacement (x : Count) : UInt32 := {target} x\n'
                            f'theorem replacement_eq : {target} = replacement := rfl\n')
            root = f'def run (x : Count) : UInt32 := {target} x\n'
            contexts = {
                'local': 'section\nattribute [local csimp] replacement_eq\n' + root + 'end\n',
                'scoped': ('namespace RewriteScope\nattribute [scoped csimp] replacement_eq\n'
                           'end RewriteScope\nopen scoped RewriteScope\n' + root),
                'late': root + 'attribute [csimp] replacement_eq\n',
            }
            for context, body in contexts.items():
                with self.subTest(target=target, context=context):
                    self.assert_rejected(definitions + body, 'rewrite|replacement|repeated compilation')


if __name__ == '__main__':
    unittest.main()
