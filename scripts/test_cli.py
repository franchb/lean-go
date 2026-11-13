#!/usr/bin/env python3
"""Exercise the installed command boundary without requiring producer tools."""
from contextlib import redirect_stdout, redirect_stderr
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from lean_go import cli
from export import COMMIT

ROOT = Path(__file__).resolve().parents[1]


class PackageTests(unittest.TestCase):
    def test_explicit_payload_covers_only_required_pipeline_sources(self):
        config = tomllib.loads((ROOT / 'pyproject.toml').read_text())
        mapping = config['tool']['hatch']['build']['targets']['wheel']['force-include']
        expected = {'go.mod', 'feasibility/LeanGo/Feasibility/Model.lean',
                    'producer/lean-toolchain', 'producer/lakefile.toml'}
        expected.update(path.relative_to(ROOT).as_posix() for path in (ROOT / 'producer').rglob('*.lean')
                        if '.lake' not in path.parts)
        expected.update('scripts/' + name + '.py' for name in
                        ['generate', 'export', 'export_module', 'check_proofs', 'proof_contract'])
        for directory in ['cmd/lean-go', 'internal/ir', 'internal/gogen',
                          'internal/moduleir', 'internal/modulegen']:
            expected.update(path.relative_to(ROOT).as_posix() for path in (ROOT / directory).glob('*.go')
                            if not path.name.endswith('_test.go'))
            expected.update(path.relative_to(ROOT).as_posix() for path in (ROOT / directory / 'templates').glob('*.tmpl'))
        self.assertEqual(set(mapping), expected)
        self.assertEqual(mapping, {name: 'lean_go/_payload/' + name for name in expected})
        with cli.workspace() as workspace:
            found = {path.relative_to(workspace).as_posix() for path in workspace.rglob('*') if path.is_file()}
            self.assertEqual(found, expected)
            for name in expected:
                self.assertEqual((workspace / name).read_bytes(), (ROOT / name).read_bytes(), name)

    def test_temporary_payload_is_removed_and_original_is_unchanged(self):
        with cli.workspace() as workspace:
            script = workspace / 'scripts/export.py'
            original = script.read_bytes()
            script.write_text('temporary mutation')
        self.assertFalse(workspace.exists())
        with cli.workspace() as another:
            self.assertEqual((another / 'scripts/export.py').read_bytes(), original)


class CommandTests(unittest.TestCase):
    def test_help_and_version_need_no_workspace_or_tools(self):
        for arguments in [['--help'], ['--version'], ['generate', '--help'], ['check-proofs', '--help']]:
            with self.subTest(arguments=arguments), patch.object(cli, 'workspace') as materialize:
                with redirect_stdout(io.StringIO()), self.assertRaises(SystemExit) as exited:
                    cli.main(arguments)
                self.assertEqual(exited.exception.code, 0)
                materialize.assert_not_called()

    def test_generate_preserves_roots_and_resolves_paths_from_caller(self):
        args = cli.parser().parse_args(['generate', '--source-dir', 'source with spaces',
                                       '--module', 'Example', '--root', 'Example.one',
                                       '--root', 'Example.two', '--out', 'out space', '--ir-output', 'ir space.json'])
        script, arguments = cli.pipeline_arguments(args)
        self.assertEqual(script, 'generate.py')
        self.assertEqual(arguments, ['--source-dir', str(Path('source with spaces').absolute()),
                                    '--out', str(Path('out space').absolute()),
                                    '--ir-output', str(Path('ir space.json').absolute()),
                                    '--module', 'Example', '--root', 'Example.one', '--root', 'Example.two'])

    def test_parent_relative_path_is_lexically_normalized(self):
        self.assertEqual(cli.absolute(Path('../source')), str(Path.cwd().parent / 'source'))

    def test_pilot_defaults_belong_to_caller(self):
        _, arguments = cli.pipeline_arguments(cli.parser().parse_args(['generate']))
        self.assertEqual(arguments, ['--out', str(Path('generated/normalizer').absolute()),
                                    '--ir-output', str(Path('testdata/normalizer.ir.json').absolute())])

    def test_module_does_not_gain_implicit_outputs(self):
        _, arguments = cli.pipeline_arguments(cli.parser().parse_args(
            ['generate', '--source-dir', 'source', '--module', 'Example', '--root', 'Example.run']))
        self.assertNotIn('--out', arguments)
        self.assertNotIn('--ir-output', arguments)

    def test_proof_paths_and_mode_are_forwarded(self):
        for mode in ['--output', '--evidence']:
            args = cli.parser().parse_args(['check-proofs', '--contract', 'project/contract.json',
                                           '--manifest', 'generated/manifest.json', mode, 'proof evidence.json'])
            script, arguments = cli.pipeline_arguments(args)
            self.assertEqual(script, 'check_proofs.py')
            self.assertEqual(arguments, ['--contract', str(Path('project/contract.json').absolute()),
                                        '--manifest', str(Path('generated/manifest.json').absolute()),
                                        mode, str(Path('proof evidence.json').absolute())])

    def test_proof_mode_is_exclusive(self):
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as exited:
            cli.main(['check-proofs', '--contract', 'c', '--manifest', 'm', '--output', 'o', '--evidence', 'e'])
        self.assertEqual(exited.exception.code, 2)

    def test_output_cannot_overlap_installed_package(self):
        installed = Path(cli.__file__).parent
        for output in [installed, installed / '_payload/new', installed.parent]:
            with self.subTest(output=output), self.assertRaisesRegex(ValueError, 'installed'):
                cli.protect_install([output])


class InvocationTests(unittest.TestCase):
    def test_python_subprocess_keeps_exit_status_and_clean_environment(self):
        with patch.object(cli.subprocess, 'run', return_value=subprocess.CompletedProcess([], 7)) as run:
            with patch.dict(os.environ, {'LEAN_PATH': 'bad', 'LAKE_HOME': 'bad', 'PYTHONPATH': 'bad',
                                         'GOFLAGS': 'bad', 'GOWORK': 'bad', 'CGO_ENABLED': '1'}):
                self.assertEqual(cli.main(['generate', '--out', 'out', '--ir-output', 'ir']), 7)
        call = run.call_args
        env = call.kwargs['env']
        self.assertEqual(call.args[0][1], '-B')
        self.assertFalse(set(env) & {'LEAN_PATH', 'LAKE_HOME', 'PYTHONPATH'})
        for name, value in {'CGO_ENABLED': '0', 'GOWORK': 'off', 'GOTOOLCHAIN': 'local',
                            'GOENV': 'off', 'GOFLAGS': '', 'PYTHONDONTWRITEBYTECODE': '1'}.items():
            self.assertEqual(env[name], value)
        self.assertFalse(Path(call.kwargs['cwd']).exists())

    def test_missing_payload_reports_clean_error(self):
        with patch.object(cli, 'workspace', side_effect=FileNotFoundError('missing payload')):
            output = io.StringIO()
            with redirect_stderr(output):
                self.assertEqual(cli.main(['generate']), 1)
            self.assertIn('missing payload', output.getvalue())

    def test_symlinked_source_is_not_resolved_and_output_survives(self):
        with tempfile.TemporaryDirectory(prefix='lean-go-cli-test-') as directory:
            root = Path(directory)
            source = root / 'source'
            source.mkdir()
            (source / 'Example.lean').write_text('def example : UInt32 := 1\n')
            link = root / 'link'
            try:
                link.symlink_to(source, target_is_directory=True)
            except OSError as error:
                self.skipTest(f'directory symlink unavailable: {error}')
            self.assertEqual(cli.absolute(link), str(link))
            nested = link / '..' / 'source'
            self.assertEqual(cli.absolute(nested), str(nested))
            output = root / 'output'
            output.mkdir()
            (output / 'sentinel').write_bytes(b'preserve')
            for selected in [link, nested]:
                result = subprocess.run([sys.executable, '-B', '-m', 'lean_go', 'generate',
                                         '--source-dir', str(selected), '--module', 'Example',
                                         '--root', 'example', '--out', str(output),
                                         '--ir-output', str(root / 'ir')],
                                        capture_output=True, text=True, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('source directory must exist and must not contain symlinks', result.stderr)
            self.assertEqual((output / 'sentinel').read_bytes(), b'preserve')
            self.assertFalse((root / 'ir').exists())


    def test_symlinked_contract_is_rejected_before_tools(self):
        with tempfile.TemporaryDirectory(prefix='lean-go-proof-path-') as directory:
            root = Path(directory)
            contract = root / 'contract.json'
            contract.write_text('{}')
            link = root / 'linked.json'
            try:
                link.symlink_to(contract)
            except OSError as error:
                self.skipTest(f'file symlink unavailable: {error}')
            output = root / 'evidence.json'
            output.write_bytes(b'preserve evidence')
            result = subprocess.run([sys.executable, '-B', '-m', 'lean_go', 'check-proofs',
                                     '--contract', str(link), '--manifest', str(root / 'manifest.json'),
                                     '--output', str(output)], capture_output=True, text=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('symlinked input', result.stderr)
            self.assertEqual(output.read_bytes(), b'preserve evidence')


class DoctorTests(unittest.TestCase):
    def doctor(self, versions=None, missing=False):
        versions = versions or ['go version go1.27.4 linux/amd64',
                                f'Lean (version 4.35.0, target, commit {COMMIT}, Release)',
                                f'Lean (version 4.35.0, target, commit {COMMIT}, Release)']
        output = io.StringIO()
        commands = []
        def run(command, **kwargs):
            commands.append(command)
            return subprocess.CompletedProcess(command, 0, versions[len(commands) - 1], '')
        with patch.object(cli.shutil, 'which', side_effect=lambda name: None if missing else name),                 patch.object(cli.subprocess, 'run', side_effect=run), redirect_stdout(output):
            result = cli.main(['doctor'])
        return result, output.getvalue(), commands

    def test_missing_tools_fail_with_actionable_messages(self):
        result, output, commands = self.doctor(missing=True)
        self.assertEqual(result, 1)
        self.assertIn('go is missing from PATH', output)
        self.assertIn('lean is missing from PATH', output)
        self.assertIn('lake is missing from PATH', output)
        self.assertEqual(commands, [])

    def test_exact_tools_and_lake_environment_are_checked(self):
        result, output, commands = self.doctor()
        self.assertEqual(result, 0, output)
        self.assertEqual(commands[-1], ['lake', 'env', 'lean', '--version'])

    def test_wrong_go_and_lean_commit_fail(self):
        result, output, _ = self.doctor(['go version go1.27.3 linux/amd64',
                                        'Lean (version 4.35.0, target, commit wrong, Release)',
                                        'Lean (version 4.34.0, target, commit wrong, Release)'])
        self.assertEqual(result, 1)
        self.assertIn('expected go1.27.4', output)
        self.assertIn('unexpected Lean version or commit', output)


if __name__ == '__main__':
    unittest.main()
