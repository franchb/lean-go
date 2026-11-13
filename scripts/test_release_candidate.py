"""Focused failure/isolation checks for the release walkthrough verifier."""
import io
import json
from pathlib import Path
import tempfile
import tomllib
import unittest
from unittest import mock

import check_release_candidate as walkthrough
from export import ExportError


class ReleaseWalkthroughTests(unittest.TestCase):
    def test_receipt_identifies_the_current_package(self):
        metadata = tomllib.loads((walkthrough.ROOT / 'pyproject.toml').read_text())
        self.assertEqual(walkthrough.CANDIDATE, metadata['project']['version'])
        self.assertEqual(walkthrough.CANDIDATE, '0.1.0a6.dev0')

    def test_missing_or_wrong_go_stops_before_generation(self):
        with mock.patch.object(walkthrough.shutil, 'which', return_value=None), \
                mock.patch.object(walkthrough, 'run') as run:
            with self.assertRaisesRegex(ExportError, 'Go 1.27.4'):
                walkthrough.verify()
            run.assert_not_called()
        with mock.patch.object(walkthrough.shutil, 'which', return_value='go'), \
                mock.patch.object(walkthrough, 'run', return_value=json.dumps(
                    {'GOVERSION': 'go1.28.0', 'GOROOT': '/missing'})) as run:
            with self.assertRaisesRegex(ExportError, 'Go 1.27.4'):
                walkthrough.verify()
            self.assertEqual(run.call_count, 1)

    def test_failed_generation_or_proof_check_cleans_workspace_without_receipt(self):
        for failing_call in (0, 1, 2):
            commands, workspaces = [], []
            def fail_at_stage(command, _root, _environment):
                commands.append(command)
                if '--source-dir' in command:
                    workspaces.append(Path(command[command.index('--source-dir') + 1]).parents[1])
                if len(commands) - 1 == failing_call:
                    raise ExportError('stage failed')
                return ''
            with self.subTest(stage=failing_call), \
                    mock.patch.object(walkthrough, 'pinned_go', return_value=Path('/go/bin/go')), \
                    mock.patch.object(walkthrough, 'run', side_effect=fail_at_stage), \
                    mock.patch('sys.stdout', new=io.StringIO()) as output:
                with self.assertRaisesRegex(ExportError, 'stage failed'):
                    walkthrough.main([])
                self.assertEqual(output.getvalue(), '')
            self.assertEqual(len(commands), failing_call + 1)
            self.assertTrue(workspaces)
            self.assertTrue(all(not workspace.exists() for workspace in workspaces))

    def test_consumer_runs_offline_with_only_go_and_rejects_wrong_output(self):
        with tempfile.TemporaryDirectory(prefix='release-consumer-test-') as directory:
            root = Path(directory)
            package = root / 'generated'
            package.mkdir()
            for name in ('api.go', 'control.go', 'runtime.go', 'schema.go'):
                (package / name).write_text('package clamp\n')
            go = root / 'go-bin/go'
            with mock.patch.object(walkthrough, 'run', return_value='unexpected') as run:
                with self.assertRaisesRegex(ExportError, 'unexpected result'):
                    walkthrough.run_consumer(root, package, go, walkthrough.base_environment())
                command, cwd, environment = run.call_args.args
                self.assertEqual(command, [str(go), 'run', '-buildvcs=false', '-mod=readonly', '.'])
                self.assertEqual(environment['PATH'], str(go.parent))
                self.assertEqual(environment['CGO_ENABLED'], '0')
                self.assertEqual(environment['GOPROXY'], 'off')
                self.assertEqual(environment['GOSUMDB'], 'off')
                self.assertEqual(environment['GOWORK'], 'off')
                self.assertFalse((cwd / 'proof').exists())
                self.assertFalse((cwd / 'source').exists())


if __name__ == '__main__':
    unittest.main()
