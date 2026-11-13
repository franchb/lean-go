"""Reusable proof contracts: protocol failures and an opt-in external Lean project."""
import copy
import dataclasses
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import check_proofs as proofs
import proof_contract as contracts
from export import COMMIT, TOOLCHAIN, ExportError

REPOSITORY = Path(__file__).resolve().parents[1]
GENERATED = {'api.go', 'control.go', 'runtime.go', 'schema.go'}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def claim_records(contract):
    return [dict(name=name, expected=expected, statement='checked proposition', axioms=['propext'])
            for name, expected in contract.claims]


def load_proof_fixture(path):
    contract = contracts.load_contract(path)
    return contract, contracts.snapshot(contract), proofs.tool_snapshot()


def replace_proof_declaration(project, module, start_marker, end_marker, declaration):
    changed = contracts.ProjectSnapshot(project.runtime.copy(), project.proofs.copy())
    relative, raw = changed.proofs[module]
    text = raw.decode()
    start = text.index(start_marker)
    end = text.index(end_marker, start)
    changed.proofs[module] = (relative, (text[:start] + declaration + '\n\n' + text[end:]).encode())
    return changed


class ContractFixture(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='proof-contract-test-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.contract_path = self.root / 'proof-contract.json'
        self.source = self.root / 'source/Example.lean'
        self.source.parent.mkdir()
        self.source.write_text('namespace Example\ndef identity (n : UInt64) := n\nend Example\n')
        proof = self.root / 'proof'
        proof.mkdir()
        (proof / 'ExampleProofs.lean').write_text('import Example\ntheorem ExampleProofs.identity (n : UInt64) : Example.identity n = n := rfl\n')
        (proof / 'ExampleContract.lean').write_text('import Example\ndef ExampleContract.identity : Prop := ∀ n : UInt64, Example.identity n = n\n')
        self.document = dict(schema='lean-go-proof-contract/v1', source_dir='source', proof_dir='proof',
                             module='Example', roots=['Example.identity'],
                             claims=[dict(theorem='ExampleProofs.identity', expected='ExampleContract.identity')])
        self.write_contract()
        self.manifest_path = self.root / 'generated/manifest.json'
        self.manifest_path.parent.mkdir()
        for name in GENERATED:
            (self.manifest_path.parent / name).write_bytes(('package extracted // ' + name).encode())
        self.write_manifest()

    def write_contract(self, document=None):
        self.contract_path.write_text(json.dumps(self.document if document is None else document))

    def project(self):
        contract = contracts.load_contract(self.contract_path)
        return contract, contracts.snapshot(contract)

    def write_manifest(self):
        contract, project = self.project()
        multiple = len(contract.roots) > 1
        program = dict(schema='lean-go-ir/v7' if multiple else 'lean-go-ir/v6',
                       profile='pure-module/v5' if multiple else 'pure-module/v4',
                       stage='pure-mono', module=contract.module, lean_version='4.35.0', lean_commit=COMMIT,
                       inputs={'source/' + relative: sha(data) for relative, data in project.runtime.values()})
        program['roots' if multiple else 'root'] = list(contract.roots) if multiple else contract.roots[0]
        self.manifest = dict(schema='lean-go-manifest/v7' if multiple else 'lean-go-manifest/v6', program=program,
                             generated_files={name: sha((self.manifest_path.parent / name).read_bytes()) for name in GENERATED})
        self.save_manifest()

    def save_manifest(self):
        self.manifest_path.write_text(json.dumps(self.manifest))

    def verify_manifest(self):
        contract, project = self.project()
        return contracts.verify_manifest(self.manifest_path, contract, project)

    def sample_claims(self):
        return claim_records(contracts.load_contract(self.contract_path))

    def evidence(self, **kwargs):
        return proofs.evidence(self.contract_path, self.manifest_path, **kwargs)

    def arguments(self):
        return ['--contract', str(self.contract_path), '--manifest', str(self.manifest_path)]


class ContractValidationTests(ContractFixture):
    def test_strict_document_schema_and_duplicate_keys(self):
        documents = [None, [], {}, dict(self.document, schema='other'), dict(self.document, hooks=[])]
        documents.extend({key: value for key, value in self.document.items() if key != absent} for absent in self.document)
        for document in documents:
            self.contract_path.write_text(json.dumps(document))
            with self.subTest(document=document), self.assertRaises(ExportError):
                contracts.load_contract(self.contract_path)
        for raw in (b'{', b'{"schema":1,"schema":2}', b'{"schema":NaN}'):
            self.contract_path.write_bytes(raw)
            with self.subTest(raw=raw), self.assertRaises(ExportError):
                contracts.load_contract(self.contract_path)

    def test_roots_and_claims_are_bounded_unique_and_canonical(self):
        self.document['roots'] = ['Example.z', 'Example.identity']
        self.document['claims'].insert(0, dict(theorem='ExampleProofs.z', expected='ExampleContract.identity'))
        self.write_contract()
        contract = contracts.load_contract(self.contract_path)
        self.assertEqual(contract.roots, ('Example.identity', 'Example.z'))
        self.assertEqual(tuple(name for name, _ in contract.claims), ('ExampleProofs.identity', 'ExampleProofs.z'))
        for key, values in [('roots', [None, [], 'Example.identity', ['Example.identity'] * 2,
                                     ['Example.f' + str(i) for i in range(65)]]),
                            ('claims', [None, [], {}, self.document['claims'] * 2,
                                        [dict(theorem='Proof.f' + str(i), expected='Expected.p') for i in range(65)]])]:
            for value in values:
                changed = dict(self.document, **{key: value})
                self.write_contract(changed)
                with self.subTest(key=key, value=value), self.assertRaises(ExportError):
                    contracts.load_contract(self.contract_path)

    def test_names_and_claim_fields_are_strict(self):
        for name in (None, 1, '', 'A..b', 'A.b c', 'A.λ', '../A', 'A.b\n'):
            for field in ('module', 'root', 'theorem', 'expected'):
                document = copy.deepcopy(self.document)
                if field == 'module':
                    document['module'] = name
                elif field == 'root':
                    document['roots'] = [name]
                else:
                    document['claims'][0][field] = name
                self.write_contract(document)
                with self.subTest(field=field, name=name), self.assertRaises(ExportError):
                    contracts.load_contract(self.contract_path)
        for claim in (None, [], {'theorem': 'Proof.p'}, {'expected': 'Expected.p'},
                      dict(theorem='Proof.p', expected='Expected.p', axioms=[])):
            self.write_contract(dict(self.document, claims=[claim]))
            with self.subTest(claim=claim), self.assertRaises(ExportError):
                contracts.load_contract(self.contract_path)

    def test_directories_are_canonical_relative_and_nonoverlapping(self):
        for path in (None, '', '.', '..', '../source', '/tmp/source', './source', 'source/',
                     'source//nested', 'source/../source', 'C:\\source', 'source\\nested',
                     'source\0', 'source\n', 'source.', 'source ', 'source/*'):
            for field in ('source_dir', 'proof_dir'):
                self.write_contract(dict(self.document, **{field: path}))
                with self.subTest(field=field, path=path), self.assertRaises(ExportError):
                    self.project()
        self.write_contract(dict(self.document, proof_dir='source'))
        with self.assertRaises(ExportError):
            self.project()
        (self.source.parent / 'nested').mkdir()
        (self.source.parent / 'nested/Proof.lean').write_text('def nested := 0')
        self.write_contract(dict(self.document, proof_dir='source/nested'))
        with self.assertRaises(ExportError):
            self.project()

    def test_symlinks_module_collisions_and_reserved_modules_fail(self):
        linked = self.root / 'source/Linked.lean'
        linked.symlink_to(self.source)
        with self.assertRaises(ExportError):
            self.project()
        linked.unlink()
        collision = self.root / 'proof/Example.lean'
        collision.write_bytes(self.source.read_bytes())
        with self.assertRaises(ExportError):
            self.project()
        collision.unlink()
        for directory in ('source', 'proof'):
            forbidden = self.root / directory / 'Init.lean'
            forbidden.write_text('def reserved := 0')
            with self.subTest(directory=directory), self.assertRaises(ExportError):
                self.project()
            forbidden.unlink()
        target = self.root / 'contract-target.json'
        self.contract_path.rename(target)
        self.contract_path.symlink_to(target)
        with self.assertRaises(ExportError):
            self.project()

    def test_build_files_and_caches_are_not_project_inputs(self):
        for relative in ('source/lakefile.lean', 'proof/lakefile.lean', 'source/.lake/Bad.lean', 'proof/.git/Bad.lean'):
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('poison')
        _, project = self.project()
        self.assertEqual(set(project.modules), {'Example', 'ExampleProofs', 'ExampleContract'})
        self.assertEqual(set(project.inputs), {'source/Example.lean', 'proof/ExampleProofs.lean', 'proof/ExampleContract.lean'})

    def test_combined_file_limit_includes_runtime_and_proofs(self):
        for directory in ('source', 'proof'):
            for index in range(128):
                (self.root / directory / (directory.capitalize() + str(index) + '.lean')).write_text('-- file')
        with self.assertRaisesRegex(ExportError, 'limit|exceeds|bound'):
            self.project()

    def test_combined_byte_limit_includes_runtime_and_proofs(self):
        self.source.write_bytes(b'--' + b'x' * (4 * 1024 * 1024))
        (self.root / 'proof/ExampleProofs.lean').write_bytes(b'--' + b'x' * (4 * 1024 * 1024))
        with self.assertRaisesRegex(ExportError, 'limit|exceeds|bound'):
            self.project()

    def test_contract_size_and_missing_runtime_module_are_rejected(self):
        self.contract_path.write_bytes(b' ' * (64 * 1024 + 1))
        with self.assertRaisesRegex(ExportError, 'limit'):
            contracts.load_contract(self.contract_path)
        self.write_contract(dict(self.document, module='Missing'))
        with self.assertRaisesRegex(ExportError, 'module'):
            self.project()


class ManifestBindingTests(ContractFixture):
    def test_single_and_multi_root_manifest_profiles(self):
        self.assertEqual(self.verify_manifest(), self.manifest_path.read_bytes())
        self.document['roots'].append('Example.another')
        self.write_contract()
        self.write_manifest()
        self.assertEqual(self.manifest['schema'], 'lean-go-manifest/v7')
        self.assertEqual(self.verify_manifest(), self.manifest_path.read_bytes())
        for key, value in [('roots', ['Example.identity']), ('root', 'Example.identity'),
                           ('lean_commit', 'other'), ('lean_version', '4.0.0'),
                           ('profile', 'pure-module/v4'), ('schema', 'lean-go-ir/v6'), ('module', 'Other')]:
            original = copy.deepcopy(self.manifest)
            self.manifest['program'][key] = value
            self.save_manifest()
            with self.subTest(key=key), self.assertRaises(ExportError):
                self.verify_manifest()
            self.manifest = original
        self.save_manifest()

    def test_single_root_cannot_use_plural_or_wrong_root(self):
        for key, value in [('root', 'Example.other'), ('roots', ['Example.identity'])]:
            original = copy.deepcopy(self.manifest)
            self.manifest['program'][key] = value
            self.save_manifest()
            with self.subTest(key=key), self.assertRaises(ExportError):
                self.verify_manifest()
            self.manifest = original

    def test_source_hash_and_complete_runtime_inventory_are_bound(self):
        for entries in ({}, {'source/Example.lean': '0' * 64},
                        dict(self.manifest['program']['inputs'], **{'source/Injected.lean': '0' * 64})):
            self.manifest['program']['inputs'] = entries
            self.save_manifest()
            with self.subTest(entries=entries), self.assertRaisesRegex(ExportError, 'source|runtime'):
                self.verify_manifest()
        self.write_manifest()
        (self.root / 'source/New.lean').write_text('def extra := 1')
        with self.assertRaisesRegex(ExportError, 'source|runtime'):
            self.verify_manifest()

    def test_generated_inventory_hashes_and_symlinks_are_checked(self):
        for name in ('extra.go', '../runtime.go', '/tmp/runtime.go'):
            self.manifest['generated_files'][name] = '0' * 64
            self.save_manifest()
            with self.subTest(name=name), self.assertRaisesRegex(ExportError, 'inventory'):
                self.verify_manifest()
            del self.manifest['generated_files'][name]
        self.save_manifest()
        for name in ('injected.go', 'injected.s', 'extra.json'):
            extra = self.manifest_path.parent / name
            extra.write_text('unlisted')
            with self.subTest(name=name), self.assertRaisesRegex(ExportError, 'inventory'):
                self.verify_manifest()
            extra.unlink()
        generated = self.manifest_path.parent / 'api.go'
        original = generated.read_bytes()
        generated.write_bytes(b'corrupted')
        with self.assertRaisesRegex(ExportError, 'hash'):
            self.verify_manifest()
        generated.write_bytes(original)
        target = self.root / 'outside.go'
        generated.rename(target)
        generated.symlink_to(target)
        with self.assertRaisesRegex(ExportError, 'symlink|regular'):
            self.verify_manifest()

    def test_malformed_manifest_fails_closed(self):
        for raw in (b'[]', b'null', b'{', b'{"schema":1,"schema":2}'):
            self.manifest_path.write_bytes(raw)
            with self.subTest(raw=raw), self.assertRaises(ExportError):
                self.verify_manifest()

    def test_manifest_cannot_reuse_generated_go_filename(self):
        raw = self.manifest_path.read_bytes()
        self.manifest_path.unlink()
        self.manifest_path = self.manifest_path.parent / 'api.go'
        self.manifest_path.write_bytes(raw)
        with self.assertRaises(ExportError):
            self.verify_manifest()


class EvidenceProtocolTests(ContractFixture):
    def test_claim_records_match_names_expectations_and_axiom_policy(self):
        contract, _ = self.project()
        self.assertEqual(proofs.validate_claims(self.sample_claims(), contract), self.sample_claims())
        for field, values in [('name', ['Other.claim']), ('expected', ['Other.expected']), ('statement', ['', None]),
                              ('axioms', ['propext', [None], ['propext'] * 2, ['sorryAx'], ['Custom.trust'],
                                          ['Lean.ofReduceBool'], ['Lean.trustCompiler']])]:
            for value in values:
                claims = self.sample_claims()
                claims[0][field] = value
                with self.subTest(field=field, value=value), self.assertRaises(ExportError):
                    proofs.validate_claims(claims, contract)
        for claims in (None, [], self.sample_claims() * 2):
            with self.subTest(claims=claims), self.assertRaises(ExportError):
                proofs.validate_claims(claims, contract)

    def test_evidence_is_deterministic_and_binds_project_tool_and_manifest(self):
        with mock.patch.object(proofs, 'prove', return_value=self.sample_claims()):
            raw = self.evidence()
            self.assertEqual(raw, self.evidence())
        evidence = json.loads(raw)
        _, project = self.project()
        self.assertEqual(evidence['schema'], 'lean-go-proof-evidence/v1')
        self.assertEqual(evidence['contract_sha256'], sha(self.contract_path.read_bytes()))
        self.assertEqual(evidence['project_inputs'], {name: sha(data) for name, data in project.inputs.items()})
        self.assertTrue({'scripts/diagnostics.py', 'scripts/source_locations.py',
                         'scripts/module_diagnostics.py'} <= evidence['tool_inputs'].keys())
        self.assertEqual(evidence['tool_inputs'], {name: sha(data) for name, data in proofs.tool_snapshot().items()})
        self.assertEqual(evidence['generated_manifest_sha256'], sha(self.manifest_path.read_bytes()))
        self.assertNotIn(str(self.root).encode(), raw)
        self.assertNotIn(str(REPOSITORY).encode(), raw)

    def test_project_contract_and_generated_mutations_during_proving_fail(self):
        for target in (self.source, self.root / 'proof/ExampleProofs.lean', self.contract_path,
                       self.manifest_path, self.manifest_path.parent / 'api.go'):
            original = target.read_bytes()
            claims = self.sample_claims()
            def mutate(*_args):
                target.write_bytes(original + (b'\n' if target.suffix == '.json' else b'\n-- changed'))
                return claims
            with self.subTest(target=target.name), mock.patch.object(proofs, 'prove', side_effect=mutate):
                with self.assertRaises(ExportError):
                    self.evidence()
            target.write_bytes(original)

    def test_tool_pin_and_changes_during_proving_fail(self):
        tool_root = self.root / 'tools'
        for relative in proofs.TOOL_INPUTS:
            path = tool_root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes((REPOSITORY / relative).read_bytes())
        target = tool_root / 'producer/lean-toolchain'
        original = target.read_bytes()
        target.write_bytes(b'leanprover/lean4:v0.0.0\n')
        with self.assertRaisesRegex(ExportError, 'pin|toolchain'):
            proofs.tool_snapshot(tool_root)
        target.write_bytes(original)
        target = tool_root / proofs.CHECKER
        original = target.read_bytes()
        claims = self.sample_claims()
        def mutate(*_args):
            target.write_bytes(original + b'changed')
            return claims
        with mock.patch.object(proofs, 'prove', side_effect=mutate), self.assertRaises(ExportError):
            self.evidence(tool_root=tool_root)

    def test_cli_rechecks_stale_evidence_and_preserves_output_on_failure(self):
        output = self.root / 'proof-evidence.json'
        with mock.patch.object(proofs, 'prove', return_value=self.sample_claims()), mock.patch('builtins.print'):
            self.assertEqual(proofs.main(self.arguments() + ['--output', str(output)]), 0)
            self.assertEqual(proofs.main(self.arguments() + ['--evidence', str(output)]), 0)
            original = output.read_bytes()
            for raw in (b'{}', b'{', b'{"schema":1,"schema":2}', original.replace(b'propext', b'badAx')):
                output.write_bytes(raw)
                with self.subTest(raw=raw[:40]), self.assertRaises(ExportError):
                    proofs.main(self.arguments() + ['--evidence', str(output)])
                self.assertEqual(output.read_bytes(), raw)
        output.write_bytes(original)
        with mock.patch.object(proofs, 'prove', side_effect=ExportError('proof failed')):
            with self.assertRaisesRegex(ExportError, 'proof failed'):
                proofs.main(self.arguments() + ['--output', str(output)])
        self.assertEqual(output.read_bytes(), original)

    def test_cli_output_cannot_replace_inputs_or_enter_generated_package(self):
        inputs = [self.contract_path, self.source, self.root / 'proof/ExampleProofs.lean', self.manifest_path,
                  self.manifest_path.parent / 'api.go', REPOSITORY / proofs.TOOL_INPUTS[0]]
        forbidden = inputs + [self.manifest_path.parent / 'evidence.json', self.manifest_path.parent / 'nested/evidence.json']
        with mock.patch.object(proofs, 'prove', return_value=self.sample_claims()), mock.patch.object(proofs, 'publish_files') as publish:
            for output in forbidden:
                with self.subTest(output=output), self.assertRaises(ExportError):
                    proofs.main(self.arguments() + ['--output', str(output)])
            publish.assert_not_called()
        alias = self.root / 'input-alias.json'
        alias.symlink_to(self.source)
        with self.assertRaises(ExportError):
            proofs.main(self.arguments() + ['--output', str(alias)])

    def test_cli_requires_exactly_one_evidence_mode(self):
        for arguments in ([], ['--output', '/tmp/a', '--evidence', '/tmp/b']):
            with self.subTest(arguments=arguments), mock.patch('sys.stderr', new=io.StringIO()), self.assertRaises(SystemExit) as failure:
                proofs.main(self.arguments() + arguments)
            self.assertEqual(failure.exception.code, 2)

    def test_fresh_workspace_uses_snapshots_and_ignores_project_caches(self):
        (self.root / 'lakefile.lean').write_text('unsafe project build script')
        cache = self.root / '.lake/build/lib/lean/Example.olean'
        cache.parent.mkdir(parents=True)
        cache.write_text('stale compiled proof')
        contract, project = self.project()
        commands = []
        def fake_run(command, workspace, env):
            commands.append(command)
            self.assertNotEqual(workspace, self.root)
            self.assertFalse((workspace / 'lakefile.lean').exists())
            self.assertFalse((workspace / '.lake').exists())
            for relative, data in project.modules.values():
                self.assertEqual((workspace / relative).read_bytes(), data)
            self.assertNotIn('LEAN_PATH', env)
            self.assertNotIn('LAKE_HOME', env)
            self.assertEqual(env['ELAN_TOOLCHAIN'], TOOLCHAIN.decode().strip())
            (workspace / 'lake-manifest.json').write_text('{"packages":[]}')
            if command[-1] == '--version':
                return f'Lean (version 4.35.0, test, commit {COMMIT}, Release)'
            if command[-1].endswith('.lean'):
                (workspace / 'claims.json').write_text(json.dumps(self.sample_claims()))
            return ''
        with mock.patch.object(proofs, 'run', side_effect=fake_run), \
                mock.patch.object(proofs.shutil, 'which', side_effect=lambda name: name), \
                mock.patch.dict(os.environ, {'LEAN_PATH': '/poison', 'LAKE_HOME': '/poison'}):
            self.assertEqual(proofs.prove(contract, project, proofs.tool_snapshot()), self.sample_claims())
        self.assertEqual(len(commands), 4)
        self.assertIn('build', commands[2])

    def test_staged_input_and_dependency_changes_are_rejected(self):
        contract, project = self.project()
        for target in ('Example.lean', 'LeanGo/Proofs/Check.lean', 'lean-toolchain', 'lakefile.toml',
                       'lakefile.lean', 'Injected.lean', 'lake-manifest.json'):
            def fake_run(command, workspace, _env):
                if command[-1] == '--version':
                    (workspace / 'lake-manifest.json').write_text('{"packages":[]}')
                    return f'Lean (version 4.35.0, test, commit {COMMIT}, Release)'
                if 'build' in command:
                    data = '{"packages":[{"name":"unexpected"}]}' if target == 'lake-manifest.json' else 'changed input'
                    (workspace / target).write_text(data)
                return ''
            with self.subTest(target=target), mock.patch.object(proofs, 'run', side_effect=fake_run), \
                    mock.patch.object(proofs.shutil, 'which', side_effect=lambda name: name), \
                    self.assertRaisesRegex(ExportError, 'workspace|dependenc|staged'):
                proofs.prove(contract, project, proofs.tool_snapshot())


@unittest.skipUnless(os.environ.get('LEAN_GO_PROOFS') == '1', 'set LEAN_GO_PROOFS=1 for Lean')
class RealProofContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture = REPOSITORY / 'examples/proofs/clamp'
        cls.contract, cls.project, cls.tools = load_proof_fixture(cls.fixture / 'proof-contract.json')

    def modified_project(self, module, declaration):
        return replace_proof_declaration(self.project, module, 'def bounded : Prop',
                                         'def preserves : Prop', declaration)

    def test_expected_declaration_kind_is_checked(self):
        for declaration in ('axiom bounded : Prop', 'theorem bounded : True := True.intro',
                            'opaque bounded : Prop := True'):
            project = self.modified_project('ClampContract', declaration)
            with self.subTest(declaration=declaration), self.assertRaisesRegex(ExportError, 'Expected statement is not a definition'):
                proofs.prove(self.contract, project, self.tools)

    def test_expected_definition_must_be_closed_prop_without_universes(self):
        for declaration in ('def bounded : Nat := 0', 'def bounded (p : Prop) : Prop := p',
                            'def bounded.{u} : Prop := ∀ (_ : Sort u), True'):
            project = self.modified_project('ClampContract', declaration)
            with self.subTest(declaration=declaration), self.assertRaisesRegex(ExportError, 'closed nonpolymorphic Prop'):
                proofs.prove(self.contract, project, self.tools)

    def test_changed_expected_proposition_is_rejected(self):
        project = self.modified_project('ClampContract', 'def bounded : Prop := True')
        with self.assertRaisesRegex(ExportError, 'Theorem type changed'):
            proofs.prove(self.contract, project, self.tools)

    def test_polymorphic_theorem_is_rejected(self):
        project = contracts.ProjectSnapshot(self.project.runtime.copy(), self.project.proofs.copy())
        relative, raw = project.proofs['ClampProofs']
        project.proofs['ClampProofs'] = (relative, raw + b'\ntheorem ClampProofs.poly.{u} (A : Sort u) (x : A) : x = x := rfl\n')
        contract = dataclasses.replace(self.contract, claims=(('ClampProofs.poly', 'ClampContract.bounded'),))
        with self.assertRaisesRegex(ExportError, 'Polymorphic theorem is unsupported'):
            proofs.prove(contract, project, self.tools)

    def test_compilation_cannot_modify_snapshotted_runtime_source(self):
        project = contracts.ProjectSnapshot(self.project.runtime.copy(), self.project.proofs.copy())
        relative, body = project.proofs['ClampProofs']
        project.proofs['ClampProofs'] = (relative, b'import Lean\n' + body +
                                      b'\nrun_cmd IO.FS.writeFile "Clamp.lean" "-- altered runtime source\\n"\n')
        with self.assertRaisesRegex(ExportError, 'workspace|staged'):
            proofs.prove(self.contract, project, self.tools)

    def test_external_project_extracts_proves_and_runs_with_go_only(self):
        with tempfile.TemporaryDirectory(prefix='external-proof-project-') as directory:
            work = Path(directory)
            project = work / 'project'
            shutil.copytree(self.fixture, project)
            (project / 'lakefile.lean').write_text('this project build script must never execute')
            cache = project / '.lake/build/lib/lean/ClampProofs.olean'
            cache.parent.mkdir(parents=True)
            cache.write_text('stale proof cache must never load')
            package, repeated = work / 'generated', work / 'regenerated'
            self.generate(project, package)
            contract, manifest = project / 'proof-contract.json', package / 'manifest.json'
            evidence, again = work / 'evidence.json', work / 'again.json'
            base = ['--contract', str(contract), '--manifest', str(manifest)]
            self.command('scripts/check_proofs.py', base + ['--output', str(evidence)])
            self.command('scripts/check_proofs.py', base + ['--evidence', str(evidence)])
            self.generate(project, repeated)
            self.assertEqual(self.package_bytes(package), self.package_bytes(repeated))
            self.command('scripts/check_proofs.py', ['--contract', str(contract), '--manifest',
                         str(repeated / 'manifest.json'), '--output', str(again)])
            self.assertEqual(evidence.read_bytes(), again.read_bytes())
            self.assertNotIn(str(work).encode(), evidence.read_bytes())
            self.run_consumer(work, package)
            self.assert_workspace_replacements_rejected(project, manifest, evidence)
            before = evidence.read_bytes()
            for target in (package / 'api.go', project / 'source/Clamp/Bounds.lean'):
                original = target.read_bytes()
                target.write_bytes(original + b'\n-- mismatch')
                result = self.command('scripts/check_proofs.py', base + ['--output', str(evidence)], success=False)
                self.assertIn('hash', result.stderr)
                self.assertEqual(evidence.read_bytes(), before)
                target.write_bytes(original)
            document = json.loads(contract.read_bytes())
            document['roots'] = ['Clamp.missing']
            contract.write_text(json.dumps(document))
            result = self.command('scripts/check_proofs.py', base + ['--output', str(evidence)], success=False)
            self.assertIn('roots', result.stderr)
            self.assertEqual(evidence.read_bytes(), before)

    def assert_workspace_replacements_rejected(self, project, manifest, output):
        contract = contracts.load_contract(project / 'proof-contract.json')
        original = contracts.snapshot(contract)
        altered = replace_proof_declaration(original, 'ClampProofs', 'theorem bounded ', 'theorem preserves ',
                    'theorem bounded (value limit : UInt64) : Clamp.clamp value limit ≤ limit := by sorry')
        forged = [dict(name=name, expected=expected, statement='FORGED unchecked statement', axioms=[])
                  for name, expected in contract.claims]
        replacement = 'import Lean\nrun_cmd IO.FS.writeFile "claims.json" ' + json.dumps(json.dumps(forged)) + '\n'
        relative, admitted = altered.proofs['ClampProofs']
        injection = '\nrun_cmd IO.FS.writeFile "LeanGo/Proofs/Harness.lean" ' + json.dumps(replacement) + '\n'
        lakefile = 'import Lake\nopen Lake DSL\npackage replaced\nlean_lib LeanGo\nlean_lib Clamp\nlean_lib ClampProofs\nlean_lib ClampContract\n'
        scripts = [admitted + injection.encode(), original.proofs['ClampProofs'][1] +
                   ('\nrun_cmd IO.FS.writeFile "lakefile.lean" ' + json.dumps(lakefile) + '\n').encode()]
        target = project / 'proof' / relative
        before = output.read_bytes()
        for body in scripts:
            target.write_bytes(b'import Lean\n' + body)
            result = self.command('scripts/check_proofs.py', ['--contract', str(contract.path), '--manifest',
                                  str(manifest), '--output', str(output)], success=False)
            self.assertRegex(result.stderr, 'Unapproved theorem axiom|workspace|harness|staged')
            self.assertEqual(output.read_bytes(), before)
        target.write_bytes(original.proofs['ClampProofs'][1])

    def command(self, script, arguments, success=True):
        environment = os.environ.copy()
        environment.update(CGO_ENABLED='0', GOTOOLCHAIN='local', GOWORK='off')
        result = subprocess.run(['uv', 'run', '--locked', 'python', script, *arguments],
                                cwd=REPOSITORY, env=environment, text=True, capture_output=True, timeout=300)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def generate(self, project, output):
        self.command('scripts/generate.py', ['--source-dir', str(project / 'source'), '--module', 'Clamp',
                     '--root', 'Clamp.clamp', '--out', str(output), '--ir-output', str(output.with_suffix('.ir.json'))])

    def package_bytes(self, package):
        return {path.name: path.read_bytes() for path in package.iterdir()}

    def run_consumer(self, work, package):
        consumer = work / 'consumer'
        (consumer / 'clamp').mkdir(parents=True)
        for path in package.glob('*.go'):
            shutil.copyfile(path, consumer / 'clamp' / path.name)
        (consumer / 'go.mod').write_text('module consumer\n\ngo 1.27.4\n')
        (consumer / 'main.go').write_text('''package main
import("fmt";"os/exec";clamp "consumer/clamp")
func main(){
for _,tool:=range []string{"lean","lake","uv","python","python3","cc","gcc","clang"}{if _,err:=exec.LookPath(tool);err==nil{panic("producer available")}}
for _,pair:=range [][2]uint64{{0,0},{0,^uint64(0)},{^uint64(0),0},{^uint64(0),^uint64(0)},{9007199254740993,9007199254740992},{42,99}}{
want:=pair[0];if want>pair[1]{want=pair[1]};got,err:=clamp.RunTyped(pair[0],pair[1],clamp.DefaultLimits());if err!=nil||got!=want{panic("clamp mismatch")}
}
fmt.Println("proof-contract Go-only consumer: OK")
}
''')
        go = shutil.which('go')
        self.assertIsNotNone(go, 'Go must be installed for the consumer check')
        environment = os.environ.copy()
        environment.update(CGO_ENABLED='0', GOTOOLCHAIN='local', GOWORK='off', GOPROXY='off',
                           GOSUMDB='off', GOFLAGS='', PATH=str(Path(go).parent))
        result = subprocess.run([go, 'run', '-buildvcs=false', '.'], cwd=consumer, env=environment,
                                text=True, capture_output=True, timeout=180)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout, 'proof-contract Go-only consumer: OK\n')


if __name__ == '__main__':
    unittest.main()
