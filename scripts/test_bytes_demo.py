#!/usr/bin/env python3
"""Check the byte demo's selected roots, twelve proofs and isolated handoff."""
from contextlib import contextmanager, ExitStack
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_bytes_demo as demo
import test_clamp_demo as common
from export import ExportError
from check_cli_install import snapshot


@contextmanager
def simulated_cli(workspace, *, claims=None, failure=None):
    """Supply exact tracked artifacts while recording the verifier's boundaries."""
    tracked = demo.ROOT / demo.DEMO
    calls, deployments = [], []
    evidence = (tracked / 'proof-evidence.json').read_bytes()
    if claims is not None:
        evidence = json.dumps(dict(json.loads(evidence), claims=claims)).encode()

    def invoke(arguments, project, environment):
        calls.append(arguments)
        phase = 'generate' if arguments[1] == 'generate' else 'prove' if '--output' in arguments else 'validate'
        if phase == failure:
            raise ExportError('simulated ' + phase + ' failure')
        if phase == 'generate':
            for relative, content in demo.artifacts(tracked).items():
                if relative == 'proof-evidence.json':
                    continue
                target = workspace / 'candidate' / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(content)
        elif phase == 'prove':
            (workspace / 'candidate/proof-evidence.json').write_bytes(evidence)
        return ''

    def deploy(project, *_):
        deployments.append(sorted(snapshot(project)))

    with ExitStack() as patches:
        patches.enter_context(patch.object(demo, 'pinned_go', return_value=Path('/go/bin/go')))
        patches.enter_context(patch.object(demo, 'run', side_effect=invoke))
        patches.enter_context(patch.object(demo, 'run_go_demo', side_effect=deploy))
        yield calls, deployments


class BytesDemoVerificationTests(common.DemoVerificationTests):
    demo = demo

    def test_four_roots_and_twelve_proofs_precede_go_only_deployment(self):
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory).resolve()
            with simulated_cli(workspace) as (calls, deployments):
                demo.verify_demo(Path('/tools/lean-go'), workspace, {})
            self.assertEqual(len(calls), 3)
            selected = calls[0]
            self.assertEqual(selected[selected.index('--module') + 1], 'ByteCodec')
            self.assertEqual(tuple(selected[index + 1] for index, value in enumerate(selected) if value == '--root'),
                             demo.SPEC.roots)
            self.assertEqual(set(demo.SPEC.roots), {'ByteCodec.encodeLE', 'ByteCodec.decodeLE',
                                                    'ByteCodec.encodeBE', 'ByteCodec.decodeBE'})
            self.assertIn('--output', calls[1])
            self.assertIn('--evidence', calls[2])
            self.assertEqual(len(demo.SPEC.claims), 12)
            self.assertEqual(deployments, [sorted(['go.mod', 'main.go', 'main_test.go'] +
                             ['bytes/' + name for name in ['api.go', 'control.go', 'runtime.go', 'schema.go']])])

    def test_inventory_rejects_omissions_additions_duplicates_and_mismatched_names(self):
        original = json.loads((demo.ROOT / demo.DEMO / 'proof-evidence.json').read_bytes())['claims']
        mutations = [original[:-1], original + [original[0]], original[:-1] + [original[0]]]
        for field, replacement in [('name', 'ByteCodecProofs.unrelated'), ('expected', 'ByteCodecContract.unrelated')]:
            mutations.append([dict(original[0], **{field: replacement}), *original[1:]])
        for index, claims in enumerate(mutations):
            with self.subTest(mutation=index), tempfile.TemporaryDirectory() as directory:
                workspace = Path(directory).resolve()
                with simulated_cli(workspace, claims=claims) as (_, deployments), \
                        patch.object(demo.shared, 'compare_artifacts'):
                    with self.assertRaisesRegex(ExportError, 'all 12 source claims'):
                        demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                self.assertEqual(deployments, [])

    def test_each_proof_phase_failure_prevents_deployment(self):
        for phase in ['prove', 'validate']:
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as directory:
                workspace = Path(directory).resolve()
                with simulated_cli(workspace, failure=phase) as (calls, deployments):
                    with self.assertRaisesRegex(ExportError, 'simulated ' + phase + ' failure'):
                        demo.verify_demo(Path('/tools/lean-go'), workspace, {})
                self.assertEqual(len(calls), 2 if phase == 'prove' else 3)
                self.assertEqual(deployments, [])

    def test_consumer_clears_producer_path_and_download_settings(self):
        commands = []
        def capture(arguments, project, environment):
            commands.append((arguments, environment))
            return demo.OUTPUT if arguments[1] == 'run' else 'ok'
        with patch.object(demo, 'run', side_effect=capture):
            demo.run_go_demo(Path('project'), Path('/go/bin/go'),
                             dict(PATH='/producer/tools', CGO_ENABLED='1', GOPROXY='https://example.invalid'),
                             Path('workspace'))
        self.assertEqual([arguments[1] for arguments, _ in commands], ['test', 'run'])
        for arguments, environment in commands:
            self.assertEqual(arguments[0], str(Path('/go/bin/go')))
            self.assertIn('-mod=readonly', arguments)
            self.assertEqual({key: environment[key] for key in ['PATH', 'CGO_ENABLED', 'GOTOOLCHAIN', 'GOWORK',
                              'GOENV', 'GOFLAGS', 'GOPROXY', 'GOSUMDB', 'LEAN_GO_DEMO_GO_ONLY']},
                             dict(PATH=str(Path('/go/bin/go').parent), CGO_ENABLED='0', GOTOOLCHAIN='local',
                                  GOWORK='off', GOENV='off', GOFLAGS='', GOPROXY='off', GOSUMDB='off',
                                  LEAN_GO_DEMO_GO_ONLY='1'))


if __name__ == '__main__':
    unittest.main()
