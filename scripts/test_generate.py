import hashlib
import importlib.util
import tempfile
import unittest
from unittest import mock
from pathlib import Path

spec = importlib.util.spec_from_file_location('generate', Path(__file__).with_name('generate.py'))
generate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generate)

class InputFreshnessTests(unittest.TestCase):
    def snapshot(self, root):
        inputs = {}
        for name in generate.INPUT_FILES:
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(('snapshot:' + name).encode())
            inputs[name] = hashlib.sha256(path.read_bytes()).hexdigest()
        return {'inputs': inputs}

    def test_fresh_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            generate.verify_inputs(root, self.snapshot(root))

    def test_stale_source_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            program = self.snapshot(root)
            (root / generate.INPUT_FILES[0]).write_bytes(b'changed after export')
            with self.assertRaisesRegex(ValueError, 'changed|mismatch|stale'):
                generate.verify_inputs(root, program)

    def test_wrong_key_set_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            program = self.snapshot(root)
            program['inputs']['../outside'] = '0' * 64
            with self.assertRaises(ValueError):
                generate.verify_inputs(root, program)

    def test_overlapping_output_paths_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            package = root / 'package'
            for output, ir_output in (
                (package, package / 'control.go'),
                (package, package),
                (package / 'nested', package),
                (package, package / 'unused' / '..' / 'control.go'),
            ):
                with self.subTest(output=output, ir_output=ir_output):
                    with self.assertRaisesRegex(ValueError, 'overlap'):
                        generate.validate_output_paths(output, ir_output)
            generate.validate_output_paths(package, root / 'program.json')

    def test_collision_fails_before_external_processes(self):
        with tempfile.TemporaryDirectory() as tmp:
            package = Path(tmp) / 'package'
            package.mkdir()
            (package / 'control.go').write_bytes(b'original generated source')
            (package / 'manifest.json').write_bytes(b'original manifest')
            before = {p.name: p.read_bytes() for p in package.iterdir()}
            arguments = ['generate.py', '--out', str(package), '--ir-output', str(package / 'control.go')]
            with mock.patch.object(generate.sys, 'argv', arguments), mock.patch.object(generate.subprocess, 'run') as run:
                with self.assertRaisesRegex(ValueError, 'overlap'):
                    generate.main()
                run.assert_not_called()
            self.assertEqual(before, {p.name: p.read_bytes() for p in package.iterdir()})

if __name__ == '__main__':
    unittest.main()
