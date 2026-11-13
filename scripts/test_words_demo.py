#!/usr/bin/env python3
"""Check fresh word-packing proofs and the complete isolated Go-only handoff."""
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import check_words_demo as demo
import test_clamp_demo as common
from export import ExportError


class WordsDemoVerificationTests(common.DemoVerificationTests):
    demo = demo

    def test_fresh_generation_checks_seven_claims_then_deploys_only_go(self):
        tracked = demo.ROOT / demo.DEMO
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory).resolve()
            def generated(command, project, environment):
                if command[1] == 'generate':
                    self.assertEqual(command[command.index('--module') + 1], 'Words')
                    self.assertEqual([command[i + 1] for i, arg in enumerate(command) if arg == '--root'],
                                     ['Words.high', 'Words.low', 'Words.narrow', 'Words.pack', 'Words.widen'])
                    shutil.copytree(tracked / 'words', workspace / 'candidate/words')
                    shutil.copy2(tracked / 'words.ir.json', workspace / 'candidate/words.ir.json')
                elif '--output' in command:
                    shutil.copy2(tracked / 'proof-evidence.json', workspace / 'candidate/proof-evidence.json')
                return ''
            def consumer(project, go, environment, parent):
                files = sorted(path.relative_to(project).as_posix() for path in project.rglob('*') if path.is_file())
                self.assertEqual(files, ['go.mod', 'main.go', 'main_test.go', 'words/api.go',
                                         'words/control.go', 'words/runtime.go', 'words/schema.go'])
            with patch.object(demo, 'pinned_go', return_value=Path('/go/bin/go')), \
                    patch.object(demo, 'run', side_effect=generated) as run, \
                    patch.object(demo, 'run_go_demo', side_effect=consumer) as consume:
                demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                self.assertEqual(run.call_count, 3)
                self.assertIn('--output', run.call_args_list[1].args[0])
                self.assertIn('--evidence', run.call_args.args[0])
                consume.assert_called_once()

    def test_wrong_claim_inventory_is_rejected(self):
        evidence = json.loads((demo.ROOT / demo.DEMO / 'proof-evidence.json').read_bytes())
        for mutation in ['missing', 'extra', 'duplicate', 'wrong theorem', 'wrong expected']:
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as directory:
                claims = [dict(claim) for claim in evidence['claims']]
                if mutation == 'missing':
                    claims.pop()
                elif mutation == 'extra':
                    claims.append({'name': 'WordsProofs.extra', 'expected': 'WordsContract.extra'})
                elif mutation == 'duplicate':
                    claims[-1] = claims[0]
                elif mutation == 'wrong theorem':
                    claims[0]['name'] = 'WordsProofs.unrelated'
                else:
                    claims[0]['expected'] = 'WordsContract.unrelated'
                workspace = Path(directory).resolve()
                def generated(command, project, environment):
                    if '--output' in command:
                        (workspace / 'candidate/proof-evidence.json').write_text(json.dumps(dict(evidence, claims=claims)))
                    return ''
                with patch.object(demo, 'pinned_go', return_value=Path('/go/bin/go')), \
                        patch.object(demo, 'run', side_effect=generated), \
                        patch.object(demo.shared, 'compare_artifacts'), \
                        patch.object(demo, 'run_go_demo') as consumer:
                    with self.assertRaisesRegex(ExportError, 'all 7 source claims'):
                        demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                    consumer.assert_not_called()

    def test_failed_proof_or_evidence_check_stops_deployment(self):
        tracked = demo.ROOT / demo.DEMO
        for failing_flag in ['--output', '--evidence']:
            with self.subTest(phase=failing_flag), tempfile.TemporaryDirectory() as directory:
                workspace = Path(directory).resolve()
                def generated(command, project, environment):
                    if failing_flag in command:
                        raise ExportError('proof check failed')
                    if '--output' in command:
                        shutil.copy2(tracked / 'proof-evidence.json', workspace / 'candidate/proof-evidence.json')
                    return ''
                with patch.object(demo, 'pinned_go', return_value=Path('/go/bin/go')), \
                        patch.object(demo, 'run', side_effect=generated), \
                        patch.object(demo.shared, 'compare_artifacts'), \
                        patch.object(demo, 'run_go_demo') as consumer:
                    with self.assertRaisesRegex(ExportError, 'proof check failed'):
                        demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                    consumer.assert_not_called()

    def test_go_consumer_disables_producer_tools_and_downloads(self):
        with patch.object(demo, 'run', side_effect=['ok', demo.OUTPUT]) as run:
            demo.run_go_demo(Path('project'), Path('/go/bin/go'),
                             {'PATH': '/producer/tools', 'CGO_ENABLED': '1', 'GOPROXY': 'https://example.invalid'},
                             Path('workspace'))
        self.assertEqual(run.call_count, 2)
        for call in run.call_args_list:
            command, project, environment = call.args
            self.assertEqual(command[0], str(Path('/go/bin/go')))
            self.assertIn('-mod=readonly', command)
            for key, expected in {'PATH': str(Path('/go/bin/go').parent), 'CGO_ENABLED': '0', 'GOTOOLCHAIN': 'local',
                                  'GOWORK': 'off', 'GOENV': 'off', 'GOFLAGS': '', 'GOPROXY': 'off',
                                  'GOSUMDB': 'off', 'LEAN_GO_DEMO_GO_ONLY': '1'}.items():
                self.assertEqual(environment[key], expected)


if __name__ == '__main__':
    unittest.main()
