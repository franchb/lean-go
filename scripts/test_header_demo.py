#!/usr/bin/env python3
"""Check the standalone header verifier and its independent source-claim inventory."""
import json
import unittest

import check_header_demo as demo
import test_clamp_demo as common


class HeaderDemoVerificationTests(common.DemoVerificationTests):
    demo = demo

    def test_contract_and_evidence_cover_selected_claims(self):
        project = demo.ROOT / demo.DEMO
        contract = json.loads((project / 'proof-contract.json').read_bytes())
        evidence = json.loads((project / 'proof-evidence.json').read_bytes())
        self.assertEqual(contract['module'], demo.SPEC.module)
        self.assertEqual(sorted(contract['roots']), sorted(demo.SPEC.roots))
        self.assertCountEqual([(c['theorem'], c['expected']) for c in contract['claims']], demo.SPEC.claims)
        self.assertCountEqual([(c['name'], c['expected']) for c in evidence['claims']], demo.SPEC.claims)


if __name__ == '__main__':
    unittest.main()
