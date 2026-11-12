"""Quota proof protocol tests; LEAN_GO_QUOTA=1 enables fresh Lean mutation tests."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import check_quota as quota


def sample_claims():
    return [dict(name='QuotaProofs.' + name, statement='checked statement', axioms=['propext'])
            for name in quota.CLAIMS]


class QuotaEvidenceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='quota-evidence-test-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        for relative in quota.INPUTS:
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'input:' + relative.encode())
        (self.root / 'producer/lean-toolchain').write_bytes(quota.TOOLCHAIN)
        self.manifest_path = self.root / quota.MANIFEST
        self.manifest_path.parent.mkdir(parents=True)
        self.manifest = {
            'schema': 'lean-go-manifest/v7',
            'program': dict(schema='lean-go-ir/v7', profile='pure-module/v5',
                            stage='pure-mono', module='Quota', roots=quota.ROOTS.copy(),
                            lean_version='4.35.0', lean_commit=quota.COMMIT,
                            inputs={'source/Quota.lean': quota.digest(self.source())}),
            'generated_files': {},
        }
        for name in quota.GENERATED:
            data = ('package quota // ' + name).encode()
            (self.manifest_path.parent / name).write_bytes(data)
            self.manifest['generated_files'][name] = quota.digest(data)
        self.write_manifest()

    def source(self):
        return (self.root / quota.SOURCE).read_bytes()

    def write_manifest(self):
        self.manifest_path.write_text(json.dumps(self.manifest))

    def test_manifest_binds_exact_source_roots_and_generated_bytes(self):
        self.assertEqual(quota.verify_manifest(self.manifest_path, self.source()),
                         self.manifest_path.read_bytes())
        for key, value in [('roots', ['Quota.create']), ('lean_commit', 'other'),
                           ('profile', 'pure-module/v4'), ('module', 'Other')]:
            original = self.manifest['program'][key]
            self.manifest['program'][key] = value
            self.write_manifest()
            with self.subTest(key=key), self.assertRaises(quota.ExportError):
                quota.verify_manifest(self.manifest_path, self.source())
            self.manifest['program'][key] = original
        self.write_manifest()
        with self.assertRaisesRegex(quota.ExportError, 'source hash'):
            quota.verify_manifest(self.manifest_path, self.source() + b'changed')
        (self.manifest_path.parent / 'api.go').write_bytes(b'corrupted')
        with self.assertRaisesRegex(quota.ExportError, 'file hash'):
            quota.verify_manifest(self.manifest_path, self.source())

    def test_manifest_rejects_missing_extra_unsafe_files_and_sources(self):
        for name in ('extra.go', '../runtime.go', '/tmp/runtime.go'):
            self.manifest['generated_files'][name] = '0' * 64
            self.write_manifest()
            with self.subTest(name=name), self.assertRaisesRegex(quota.ExportError, 'inventory'):
                quota.verify_manifest(self.manifest_path, self.source())
            del self.manifest['generated_files'][name]
        del self.manifest['generated_files']['api.go']
        self.write_manifest()
        with self.assertRaisesRegex(quota.ExportError, 'inventory'):
            quota.verify_manifest(self.manifest_path, self.source())
        self.manifest['program']['inputs']['source/Injected.lean'] = '0' * 64
        self.write_manifest()
        with self.assertRaisesRegex(quota.ExportError, 'source hash'):
            quota.verify_manifest(self.manifest_path, self.source())

    def test_manifest_rejects_symlinked_generated_files(self):
        target = self.root / 'external.go'
        generated = self.manifest_path.parent / 'api.go'
        generated.rename(target)
        generated.symlink_to(target)
        with self.assertRaisesRegex(quota.ExportError, 'symlink'):
            quota.verify_manifest(self.manifest_path, self.source())

    def test_manifest_rejects_unlisted_files_in_generated_directory(self):
        for name in ('injected.go', 'injected.s', 'extra.json'):
            extra = self.manifest_path.parent / name
            extra.write_bytes(b'unlisted package content')
            with self.subTest(name=name), self.assertRaisesRegex(quota.ExportError, 'inventory'):
                quota.verify_manifest(self.manifest_path, self.source())
            extra.unlink()

    def test_malformed_manifest_is_rejected(self):
        for raw in (b'[]', b'null', b'{', b'{"schema":1,"schema":2}'):
            self.manifest_path.write_bytes(raw)
            with self.subTest(raw=raw), self.assertRaises(quota.ExportError):
                quota.verify_manifest(self.manifest_path, self.source())

    def test_claim_protocol_rejects_unknown_axioms_and_malformed_records(self):
        self.assertEqual(quota.validate_claims(sample_claims()), sample_claims())
        for axiom in ('sorryAx', 'Custom.trust', 'Lean.ofReduceBool', 'Lean.trustCompiler'):
            claims = sample_claims()
            claims[0]['axioms'] = [axiom]
            with self.subTest(axiom=axiom), self.assertRaisesRegex(quota.ExportError, 'axiom'):
                quota.validate_claims(claims)
        malformed = [None, [], sample_claims()[:-1]]
        for field, value in [('name', 'Wrong'), ('statement', ''), ('axioms', ['propext'] * 2),
                             ('axioms', [None]), ('axioms', 'propext')]:
            claims = sample_claims()
            claims[0][field] = value
            malformed.append(claims)
        for claims in malformed:
            with self.subTest(claims=claims), self.assertRaises(quota.ExportError):
                quota.validate_claims(claims)

    @mock.patch.object(quota, 'prove', return_value=sample_claims())
    def test_evidence_is_deterministic_and_hash_bound(self, _prove):
        raw = quota.evidence(self.root, self.manifest_path)
        self.assertEqual(raw, quota.evidence(self.root, self.manifest_path))
        evidence = json.loads(raw)
        self.assertEqual(evidence['inputs'][quota.SOURCE], quota.digest(self.source()))
        self.assertEqual(evidence['generated_manifest_sha256'],
                         quota.digest(self.manifest_path.read_bytes()))
        self.assertEqual(set(evidence['inputs']), set(quota.INPUTS))

    def test_changes_during_proof_run_are_rejected(self):
        def change_source(_sources):
            (self.root / quota.SOURCE).write_bytes(b'changed')
            return sample_claims()
        with mock.patch.object(quota, 'prove', side_effect=change_source):
            with self.assertRaisesRegex(quota.ExportError, 'changed during checking'):
                quota.evidence(self.root, self.manifest_path)

    @mock.patch.object(quota, 'prove', return_value=sample_claims())
    def test_cli_rechecks_tracked_evidence_and_rejects_malformed_or_stale(self, _prove):
        output = self.root / quota.EVIDENCE
        args = ['--root', str(self.root)]
        with mock.patch('builtins.print'):
            self.assertEqual(quota.main(args + ['--output', str(output)]), 0)
            self.assertEqual(quota.main(args), 0)
        original = output.read_bytes()
        for raw in (b'{}', b'{', b'{"schema":1,"schema":2}', original.replace(b'propext', b'badAx')):
            output.write_bytes(raw)
            with self.subTest(raw=raw[:50]), self.assertRaises(quota.ExportError):
                quota.main(args)

    def test_cli_cannot_overwrite_inputs(self):
        for relative in (quota.SOURCE, quota.CHECKER, quota.MANIFEST, 'generated/quota/api.go'):
            with self.subTest(relative=relative), self.assertRaisesRegex(quota.ExportError, 'overwrite'):
                quota.main(['--root', str(self.root), '--output', str(self.root / relative)])

    def test_cli_cannot_write_evidence_into_generated_package(self):
        before = {path.name: path.read_bytes() for path in self.manifest_path.parent.iterdir()}
        for name in ('proof-evidence.json', 'nested/proof-evidence.json'):
            output = self.manifest_path.parent / name
            with self.subTest(name=name), self.assertRaisesRegex(quota.ExportError, 'outside'):
                quota.main(['--root', str(self.root), '--output', str(output)])
        after = {path.name: path.read_bytes() for path in self.manifest_path.parent.iterdir()}
        self.assertEqual(after, before)

    def test_fresh_workspace_ignores_project_scripts_and_cache(self):
        for name in ('lakefile.lean', '.lake/build/lib/lean/Quota.olean'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'poisoned project cache or script')
        commands = []

        def fake_run(command, workspace, env):
            commands.append(command)
            self.assertFalse((workspace / 'lakefile.lean').exists())
            self.assertFalse((workspace / '.lake').exists())
            self.assertEqual((workspace / 'lakefile.toml').read_bytes(), quota.CONFIG)
            self.assertEqual((workspace / 'Quota.lean').read_bytes(), self.source())
            self.assertNotIn('LEAN_PATH', env)
            self.assertEqual(env['ELAN_TOOLCHAIN'], quota.TOOLCHAIN.decode().strip())
            if command[-1] == '--version':
                (workspace / 'lake-manifest.json').write_text('{"packages":[]}')
                return f'Lean (version 4.35.0, test, commit {quota.COMMIT}, Release)'
            if command[-1] == 'Check.lean':
                (workspace / 'claims.json').write_text(json.dumps(sample_claims()))
            return ''

        with mock.patch.object(quota, 'run', side_effect=fake_run), \
                mock.patch.object(quota.shutil, 'which', side_effect=lambda name: name), \
                mock.patch.dict(os.environ, {'LEAN_PATH': '/poison'}):
            self.assertEqual(quota.prove(quota.snapshot(self.root)), sample_claims())
        self.assertEqual([command[1:] for command in commands],
                         [['--version'], ['env', 'lean', '--version'],
                          ['build', 'QuotaProofs'], ['env', 'lean', 'Check.lean']])


@unittest.skipUnless(os.environ.get('LEAN_GO_QUOTA') == '1', 'set LEAN_GO_QUOTA=1 for Lean')
class QuotaRealProofTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.sources = quota.snapshot(Path(__file__).resolve().parents[1])

    def replace_create(self, declaration):
        sources = self.sources.copy()
        proof = sources[quota.PROOFS].decode()
        start = proof.index('theorem create_valid ')
        end = proof.index('theorem reserve_accepted ', start)
        sources[quota.PROOFS] = (proof[:start] + declaration + '\n\n' + proof[end:]).encode()
        return sources

    def test_all_exact_claims_compile_deterministically(self):
        first = quota.prove(self.sources)
        self.assertEqual(first, quota.prove(self.sources))
        self.assertEqual(len(first), 10)
        self.assertTrue(all(set(claim['axioms']) <= quota.AXIOMS for claim in first))

    def test_weakened_true_statement_is_rejected(self):
        sources = self.replace_create('theorem create_valid : True := by trivial')
        with self.assertRaisesRegex(quota.ExportError, 'Theorem type changed'):
            quota.prove(sources)

    def test_same_type_axiom_is_not_a_theorem(self):
        sources = self.replace_create('axiom create_valid (capacity : UInt64) :\n'
                                     '  (Quota.create capacity).used ≤ (Quota.create capacity).capacity')
        with self.assertRaisesRegex(quota.ExportError, 'Claim is not a theorem'):
            quota.prove(sources)

    def test_same_type_sorry_and_custom_axiom_are_rejected(self):
        statement = ('(capacity : UInt64) :\n'
                     '  (Quota.create capacity).used ≤ (Quota.create capacity).capacity')
        declarations = [
            'theorem create_valid ' + statement + ' := by sorry',
            'axiom customTrust ' + statement + '\n\n' +
            'theorem create_valid ' + statement + ' := customTrust capacity',
        ]
        for declaration in declarations:
            with self.subTest(declaration=declaration), \
                    self.assertRaisesRegex(quota.ExportError, 'Unapproved theorem axiom'):
                quota.prove(self.replace_create(declaration))

    def test_native_decide_dependency_is_rejected(self):
        sources = self.replace_create('''theorem create_valid (capacity : UInt64) :
    (Quota.create capacity).used ≤ (Quota.create capacity).capacity := by
  have seed : (0 : UInt64) = 0 := by native_decide
  exact Eq.mp (congrArg (fun n : UInt64 => n ≤ capacity) seed) (by simp)''')
        with self.assertRaisesRegex(quota.ExportError, 'Unapproved theorem axiom.*native_decide'):
            quota.prove(sources)

    def test_semantic_source_mutation_fails_proofs(self):
        sources = self.sources.copy()
        original = sources[quota.SOURCE]
        sources[quota.SOURCE] = original.replace(b'used := state.used + amount',
                                               b'used := state.used')
        self.assertNotEqual(sources[quota.SOURCE], original)
        with self.assertRaisesRegex(quota.ExportError, 'QuotaProofs.lean'):
            quota.prove(sources)


if __name__ == '__main__':
    unittest.main()
