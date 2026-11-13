#!/usr/bin/env python3
"""Check the complete flags handoff and fail closed on stale or failed results."""
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import check_flags_demo as demo
import test_clamp_demo as common
from export import ExportError


class FlagsDemoVerificationTests(common.DemoVerificationTests):
    demo = demo

    def test_fresh_generation_checks_exact_claims_then_deploys_only_go(self):
        tracked = demo.ROOT / demo.DEMO
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory).resolve()
            def generated(command, project, environment):
                if command[1] == 'generate':
                    self.assertEqual([command[i + 1] for i, arg in enumerate(command) if arg == '--root'],
                                     ['Flags.clear', 'Flags.contains', 'Flags.set', 'Flags.toggle'])
                    shutil.copytree(tracked / 'flags', workspace / 'candidate/flags')
                    shutil.copy2(tracked / 'flags.ir.json', workspace / 'candidate/flags.ir.json')
                elif '--output' in command:
                    shutil.copy2(tracked / 'proof-evidence.json', workspace / 'candidate/proof-evidence.json')
                return ''
            def consumer(project, go, environment, parent):
                files = sorted(path.relative_to(project).as_posix() for path in project.rglob('*') if path.is_file())
                self.assertEqual(files, ['flags/api.go', 'flags/control.go', 'flags/runtime.go', 'flags/schema.go',
                                         'go.mod', 'main.go', 'main_test.go'])
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
                    claims[0]['expected'] = 'FlagsContract.unrelated'
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
                    with self.assertRaisesRegex(ExportError, 'all 8 source claims'):
                        demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                    consumer.assert_not_called()


if __name__ == '__main__':
    unittest.main()
