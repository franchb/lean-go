"""Producer boundary tests; real compiler reproducibility is an explicit smoke test."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("producer_export", Path(__file__).with_name("export.py"))
producer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(producer)


class ProducerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="lean-go-producer-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "source"
        actual = Path(__file__).resolve().parents[1]
        for name in producer.INPUTS:
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes((actual / name).read_bytes())
        self.output = Path(self.temp.name) / "result.json"
        self.output.write_bytes(b"previous success\n")

    def test_modified_model_preserves_output(self):
        path = self.root / producer.MODEL
        path.write_bytes(path.read_bytes() + b"\n")
        with self.assertRaisesRegex(producer.ExportError, "model SHA-256"):
            producer.export(self.root, self.output)
        self.assertEqual(self.output.read_bytes(), b"previous success\n")

    def test_fixed_configuration(self):
        for name in ("producer/lakefile.toml", "producer/lean-toolchain"):
            with self.subTest(name=name):
                path = self.root / name
                before = path.read_bytes()
                path.write_bytes(before + b"\n")
                with self.assertRaises(producer.ExportError):
                    producer.export(self.root, self.output)
                path.write_bytes(before)
        self.assertEqual(self.output.read_bytes(), b"previous success\n")

    def test_wrong_version_before_compilation_and_cleanup(self):
        workspaces = []
        def fake_run(command, workspace, env):
            workspaces.append(workspace)
            self.assertEqual(command[-1], "--version")
            return "Lean (version 4.34.0, wrong commit)"
        with patch.object(producer, "run", side_effect=fake_run), patch.object(producer.shutil, "which", return_value="tool"):
            with self.assertRaisesRegex(producer.ExportError, "unexpected Lean version"):
                producer.export(self.root, self.output)
        self.assertTrue(workspaces)
        self.assertTrue(all(not path.exists() for path in workspaces))
        self.assertEqual(self.output.read_bytes(), b"previous success\n")

    def test_fresh_workspace_contains_only_snapshots(self):
        (self.root / "producer/rogue.olean").write_bytes(b"untrusted compiled source")
        workspaces = []
        version = f"Lean (version 4.35.0, test, commit {producer.COMMIT}, Release)"
        def fake_run(command, workspace, env):
            if workspace not in workspaces:
                workspaces.append(workspace)
                files = {str(p.relative_to(workspace)) for p in workspace.rglob("*") if p.is_file()}
                self.assertEqual(files, {"lean-toolchain", "lakefile.toml", "Export.lean", "Reference.lean",
                                        "LeanGo.lean", "LeanGo/Pilot/Export.lean", "LeanGo/Feasibility/Model.lean"})
                self.assertEqual((workspace / "LeanGo/Feasibility/Model.lean").read_bytes(),
                                 (self.root / producer.MODEL).read_bytes())
                self.assertNotIn("LEAN_PATH", env)
            if command[-1] == "--version":
                (workspace / "lake-manifest.json").write_text('{"packages":[]}')
                return version
            if command[-1] == "Export.lean":
                (workspace / "ir.json").write_text('{"types":[],"functions":[],"claims":[],"rewrites":[]}')
            return ""
        with patch.object(producer, "run", side_effect=fake_run), patch.object(producer.shutil, "which", return_value="tool"):
            producer.export(self.root, self.output)
            first = self.output.read_bytes()
            producer.export(self.root, self.output)
        self.assertEqual(first, self.output.read_bytes())
        self.assertEqual(len(workspaces), 2)
        self.assertNotEqual(*workspaces)
        self.assertTrue(all(not path.exists() for path in workspaces))
        result = json.loads(first)
        self.assertEqual(set(result["inputs"]), set(producer.INPUTS))
        self.assertEqual(result["inputs"][producer.MODEL], producer.MODEL_SHA256)

    def test_failed_process_has_stderr(self):
        process = subprocess.CompletedProcess(["lean"], 9, "compiler stdout", "precise compiler stderr")
        with patch.object(producer.subprocess, "run", return_value=process):
            with self.assertRaisesRegex(producer.ExportError, "precise compiler stderr"):
                producer.run(["lean"], self.root, {})

    def test_failed_lean_preserves_output_and_cleans_only_own_workspace(self):
        sentinel = self.root / "keep.txt"
        sentinel.write_text("keep")
        version = f"Lean (version 4.35.0, test, commit {producer.COMMIT}, Release)"
        workspaces = []
        def fake_run(command, workspace, env):
            workspaces.append(workspace)
            if command[-1] == "--version":
                (workspace / "lake-manifest.json").write_text('{"packages":[]}')
                return version
            raise producer.ExportError("actual compiler failure")
        with patch.object(producer, "run", side_effect=fake_run), patch.object(producer.shutil, "which", return_value="tool"):
            with self.assertRaisesRegex(producer.ExportError, "actual compiler failure"):
                producer.export(self.root, self.output)
        self.assertEqual(self.output.read_bytes(), b"previous success\n")
        self.assertEqual(sentinel.read_text(), "keep")
        self.assertTrue(all(not path.exists() for path in workspaces))

    def test_reference_strict_shapes_and_uint32(self):
        producer.validate_reference(b'[{"tag":1,"id":4294967295},{"tag":0,"tri":0}]')
        invalid = [b'[{"tag":1,"id":4294967296}]', b'[{"tag":1,"id":1.0}]',
                   b'[{"tag":0,"tri":false}]', b'[{"tag":0,"tri":0,"id":0}]',
                   b'[{"tag":0,"tag":0,"tri":0}]', b'[{"tag":2}]', b'[] trailing',
                   b'[{"tag":0,"tri":NaN}]', b'\xff']
        tree = {"tag":0,"tri":0}
        for _ in range(64):
            tree = {"tag":2,"left":tree}
        invalid.append(json.dumps([tree]).encode())
        invalid.append(json.dumps([{"tag":0,"tri":0}] * 1025).encode())
        for data in invalid:
            with self.subTest(data=data[:80]):
                with self.assertRaises(producer.ExportError):
                    producer.validate_reference(data)

    def test_second_publication_failure_rolls_back_both_outputs(self):
        second = Path(self.temp.name) / "second.json"
        second.write_bytes(b"old reference")
        real_replace = producer.os.replace
        calls = 0
        def fail_second(source, destination):
            nonlocal calls
            calls += 1
            if calls == 2:
                raise OSError("injected publication failure")
            real_replace(source, destination)
        with patch.object(producer.os, "replace", side_effect=fail_second):
            with self.assertRaisesRegex(OSError, "publication failure"):
                producer.publish_files([(self.output, b"new IR"), (second, b"new reference")])
        self.assertEqual(self.output.read_bytes(), b"previous success\n")
        self.assertEqual(second.read_bytes(), b"old reference")
        self.assertEqual(list(Path(self.temp.name).glob(".lean-go-export-*")), [])

    def test_failed_rollback_retains_original_bytes_and_reports_both_errors(self):
        second = Path(self.temp.name) / "second.json"
        second.write_bytes(b"old reference")
        real_replace = producer.os.replace
        calls = 0
        def fail_publication_and_rollback(source, destination):
            nonlocal calls
            calls += 1
            if calls == 2:
                raise OSError("injected publication failure")
            if calls == 3:
                raise OSError("injected rollback failure")
            real_replace(source, destination)
        with patch.object(producer.os, "replace", side_effect=fail_publication_and_rollback):
            with self.assertRaises(producer.ExportError) as raised:
                producer.publish_files([(self.output, b"new IR"), (second, b"new reference")])
        message = str(raised.exception)
        self.assertIn("injected publication failure", message)
        self.assertIn("injected rollback failure", message)
        backups = list(Path(self.temp.name).glob(".lean-go-export-*"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_bytes(), b"previous success\n")
        self.assertIn(str(backups[0]), message)
        self.assertIn(str(self.output), message)
        self.assertEqual(self.output.read_bytes(), b"new IR")
        self.assertEqual(second.read_bytes(), b"old reference")

    def test_staging_failure_preserves_first_output(self):
        second = Path(self.temp.name) / "missing" / "result.json"
        with self.assertRaises(producer.ExportError):
            producer.publish_files([(self.output, b"new IR"), (second, b"new reference")])
        self.assertEqual(self.output.read_bytes(), b"previous success\n")
        self.assertEqual(list(Path(self.temp.name).glob(".lean-go-export-*")), [])

    def test_symlink_destination_preserved(self):
        target = Path(self.temp.name) / "target"
        target.write_text("keep")
        link = Path(self.temp.name) / "link"
        link.symlink_to(target)
        with self.assertRaises(producer.ExportError):
            producer.stage_file(link, b"new")
        self.assertEqual(target.read_text(), "keep")
        self.assertTrue(link.is_symlink())


if __name__ == "__main__":
    unittest.main()
