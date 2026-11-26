#!/usr/bin/env python3
"""Check the complete normalizer handoff and fail closed on stale or failed results."""
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import check_normalizer_demo as demo
import test_clamp_demo as common
from export import ExportError


class NormalizerDemoVerificationTests(common.DemoVerificationTests):
    demo = demo

    def test_fresh_generation_checks_exact_claims_then_deploys_only_go(self):
        tracked = demo.ROOT / demo.DEMO
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory).resolve()
            def generated(command, project, environment):
                if command[1] == 'generate':
                    self.assertEqual([command[i + 1] for i, arg in enumerate(command) if arg == '--root'],
                                     ['Normalizer.normalize'])
                    shutil.copytree(tracked / 'normalizer', workspace / 'candidate/normalizer')
                    shutil.copy2(tracked / 'normalizer.ir.json', workspace / 'candidate/normalizer.ir.json')
                elif '--output' in command:
                    shutil.copy2(tracked / 'proof-evidence.json', workspace / 'candidate/proof-evidence.json')
                return ''
            def consumer(project, go, environment, parent):
                files = sorted(path.relative_to(project).as_posix() for path in project.rglob('*') if path.is_file())
                self.assertEqual(files, ['go.mod', 'main.go', 'main_test.go', 'normalizer/api.go',
                                         'normalizer/control.go', 'normalizer/runtime.go', 'normalizer/schema.go'])
            with patch.object(demo, 'pinned_go', return_value=Path('/go/bin/go')), \
                    patch.object(demo, 'run', side_effect=generated) as run, \
                    patch.object(demo, 'run_go_demo', side_effect=consumer) as consume:
                demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                self.assertEqual(run.call_count, 3)
                self.assertIn('--evidence', run.call_args.args[0])
                consume.assert_called_once()

    def test_wrong_claim_inventory_is_rejected(self):
        evidence = json.loads((demo.ROOT / demo.DEMO / 'proof-evidence.json').read_bytes())
        for mutation in ['missing', 'duplicate', 'wrong']:
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as directory:
                claims = [dict(claim) for claim in evidence['claims']]
                if mutation == 'missing':
                    claims.pop()
                elif mutation == 'duplicate':
                    claims[-1] = claims[0]
                else:
                    claims[0]['expected'] = 'NormalizerContract.unrelated'
                fresh = dict(evidence, claims=claims)
                workspace = Path(directory).resolve()
                def generated(command, project, environment):
                    if '--output' in command:
                        (workspace / 'candidate/proof-evidence.json').write_text(json.dumps(fresh))
                    return ''
                with patch.object(demo, 'pinned_go', return_value=Path('/go/bin/go')), \
                        patch.object(demo, 'run', side_effect=generated), \
                        patch.object(demo.shared, 'compare_artifacts'), \
                        patch.object(demo, 'run_go_demo') as consumer:
                    with self.assertRaisesRegex(ExportError, 'all 2 source claims'):
                        demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                    consumer.assert_not_called()

    def test_go_consumer_matches_verifier_output(self):
        source = (demo.ROOT / demo.DEMO / 'main_test.go').read_text()
        for line in demo.OUTPUT.splitlines():
            self.assertIn('// ' + line + '\n', source)


if __name__ == '__main__':
    unittest.main()
