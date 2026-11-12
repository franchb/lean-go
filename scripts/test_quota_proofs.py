"""Quota CLI compatibility and real source-proof regressions through the shared gate."""
import os
from pathlib import Path
import unittest
from unittest import mock

import check_proofs as proofs
import check_quota as quota
import proof_contract as contracts
from export import ExportError
from test_proof_contracts import load_proof_fixture, replace_proof_declaration

REPOSITORY = Path(__file__).resolve().parents[1]


class QuotaCompatibilityTests(unittest.TestCase):
    def test_default_verification_and_explicit_publication_keep_cli_paths(self):
        root = Path('/tmp/quota-wrapper-project')
        base = ['--contract', str(root / 'libraries/quota/proof-contract.json')]
        cases = [
            (['--root', str(root)], base + ['--manifest', str(root / 'generated/quota/manifest.json'),
                                          '--evidence', str(root / 'libraries/quota/proof-evidence.json')]),
            (['--root', str(root), '--manifest', '/tmp/candidate/manifest.json',
              '--output', '/tmp/candidate-evidence.json'],
             base + ['--manifest', '/tmp/candidate/manifest.json', '--output', '/tmp/candidate-evidence.json']),
        ]
        for arguments, expected in cases:
            with self.subTest(arguments=arguments), mock.patch.object(quota.check_proofs, 'main', return_value=7) as shared:
                self.assertEqual(quota.main(arguments), 7)
                shared.assert_called_once_with(expected)

    def test_quota_contract_still_selects_ten_claims_and_four_operations(self):
        contract = contracts.load_contract(REPOSITORY / 'libraries/quota/proof-contract.json')
        self.assertEqual(contract.module, 'Quota')
        self.assertEqual(contract.roots, ('Quota.create', 'Quota.release', 'Quota.remaining', 'Quota.reserve'))
        self.assertEqual(len(contract.claims), 10)
        self.assertTrue(all(name.startswith('QuotaProofs.') and expected.startswith('QuotaContract.')
                            for name, expected in contract.claims))


@unittest.skipUnless(os.environ.get('LEAN_GO_QUOTA') == '1', 'set LEAN_GO_QUOTA=1 for Lean')
class QuotaRealProofTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.contract, cls.project, cls.tools = load_proof_fixture(REPOSITORY / 'libraries/quota/proof-contract.json')

    def prove(self, project=None):
        return proofs.prove(self.contract, self.project if project is None else project, self.tools)

    def replace_create(self, declaration):
        return replace_proof_declaration(self.project, 'QuotaProofs', 'theorem create_valid ',
                                         'theorem reserve_accepted ', declaration)

    def test_all_exact_claims_compile_deterministically(self):
        first = self.prove()
        self.assertEqual(first, self.prove())
        self.assertEqual(len(first), 10)
        self.assertTrue(all(set(claim['axioms']) <= proofs.AXIOMS for claim in first))

    def test_weakened_true_statement_is_rejected(self):
        project = self.replace_create('theorem create_valid : True := by trivial')
        with self.assertRaisesRegex(ExportError, 'Theorem type changed'):
            self.prove(project)

    def test_same_type_axiom_is_not_a_theorem(self):
        project = self.replace_create('axiom create_valid (capacity : UInt64) :\n'
                                      '  (Quota.create capacity).used ≤ (Quota.create capacity).capacity')
        with self.assertRaisesRegex(ExportError, 'Claim is not a theorem'):
            self.prove(project)

    def test_same_type_sorry_and_custom_axiom_are_rejected(self):
        statement = ('(capacity : UInt64) :\n'
                     '  (Quota.create capacity).used ≤ (Quota.create capacity).capacity')
        declarations = [
            'theorem create_valid ' + statement + ' := by sorry',
            'axiom customTrust ' + statement + '\n\n' +
            'theorem create_valid ' + statement + ' := customTrust capacity',
        ]
        for declaration in declarations:
            with self.subTest(declaration=declaration), self.assertRaisesRegex(ExportError, 'Unapproved theorem axiom'):
                self.prove(self.replace_create(declaration))

    def test_native_decide_dependency_is_rejected(self):
        project = self.replace_create('''theorem create_valid (capacity : UInt64) :
    (Quota.create capacity).used ≤ (Quota.create capacity).capacity := by
  have seed : (0 : UInt64) = 0 := by native_decide
  exact Eq.mp (congrArg (fun n : UInt64 => n ≤ capacity) seed) (by simp)''')
        with self.assertRaisesRegex(ExportError, 'Unapproved theorem axiom.*native_decide'):
            self.prove(project)

    def test_semantic_source_mutation_fails_proofs(self):
        project = contracts.ProjectSnapshot(self.project.runtime.copy(), self.project.proofs.copy())
        relative, original = project.runtime['Quota']
        changed = original.replace(b'used := state.used + amount', b'used := state.used')
        self.assertNotEqual(changed, original)
        project.runtime['Quota'] = (relative, changed)
        with self.assertRaisesRegex(ExportError, 'QuotaProofs.lean'):
            self.prove(project)


if __name__ == '__main__':
    unittest.main()
