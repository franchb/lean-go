"""The Go emission mode reaches the Go emitter from both command lines."""
from pathlib import Path
import io
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr
from unittest import mock

import generate
from lean_go import cli


class ModeTests(unittest.TestCase):
    def emitter_command(self, extra):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            source = work / 'source'
            source.mkdir()
            (source / 'Example.lean').write_text('def run (x : UInt32) := x\n')
            arguments = ['generate.py', '--source-dir', str(source), '--module', 'Example',
                         '--root', 'Example.run', '--out', str(work / 'package'),
                         '--ir-output', str(work / 'program.json'), *extra]
            commands = []

            def process(command, **kwargs):
                commands.append(command)
                if command[-1] == 'version':
                    return subprocess.CompletedProcess(command, 0, 'go version go1.27.4 linux/amd64\n')
                return subprocess.CompletedProcess(command, 0)

            def export(root, source_dir, module, entries, output):
                output.write_text('{"schema":"lean-go-ir/v6"}')
            with mock.patch.object(generate.sys, 'argv', arguments), \
                    mock.patch.object(generate.shutil, 'which', return_value='go'), \
                    mock.patch.object(generate.subprocess, 'run', side_effect=process), \
                    mock.patch.object(generate, 'export_module', side_effect=export), \
                    mock.patch.object(generate, 'verify_inputs'), mock.patch('builtins.print'):
                generate.main()
            emitter = [command for command in commands if './cmd/lean-go' in command]
            self.assertEqual(len(emitter), 1)
            return emitter[0]

    def test_generate_defaults_to_metered(self):
        command = self.emitter_command([])
        self.assertEqual(command[command.index('-mode') + 1], 'metered')

    def test_generate_forwards_native(self):
        command = self.emitter_command(['--mode', 'native'])
        self.assertEqual(command[command.index('-mode') + 1], 'native')

    def test_generate_rejects_unknown_mode(self):
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            generate.main(['--mode', 'fast'])

    def test_installed_cli_forwards_only_a_non_default_mode(self):
        base = ['generate', '--source-dir', 'source', '--module', 'Example', '--root', 'Example.run']
        _, arguments = cli.pipeline_arguments(cli.parser().parse_args(base))
        self.assertNotIn('--mode', arguments)
        _, arguments = cli.pipeline_arguments(cli.parser().parse_args([*base, '--mode', 'native']))
        self.assertEqual(arguments[-2:], ['--mode', 'native'])


if __name__ == '__main__':
    unittest.main()
