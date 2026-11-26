"""Shared producer helpers: compiler pins, process failures and atomic publication."""
import importlib.util
import os
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
        self.root = Path(self.temp.name)
        self.output = Path(self.temp.name) / "result.json"
        self.output.write_bytes(b"previous success\n")

    def test_only_selected_version_and_commit_pass(self):
        for compiler in producer.SUPPORTED_COMPILERS:
            with self.subTest(compiler=compiler), patch.dict(os.environ, LEAN_GO_TOOLCHAIN=compiler.version):
                self.assertEqual(producer.selected_compiler(), compiler)
                current = f"Lean (version {compiler.version}, test, commit {compiler.commit}, Release)"
                compiler.check_version(current + "\n")
                for other in producer.SUPPORTED_COMPILERS:
                    if other != compiler:
                        with self.assertRaises(producer.ExportError):
                            compiler.check_version(current.replace(compiler.commit, other.commit))
                        with self.assertRaises(producer.ExportError):
                            compiler.check_version(current.replace(compiler.version, other.version))
                for invalid in (current + "\n" + current, "prefix " + current,
                                current.replace(compiler.commit, compiler.commit[:8]),
                                current.replace("Release", "Debug")):
                    with self.assertRaises(producer.ExportError):
                        compiler.check_version(invalid)

    def test_default_and_unknown_selections(self):
        with patch.dict(os.environ, {"ELAN_TOOLCHAIN": "unrelated"}, clear=True):
            self.assertEqual(producer.selected_compiler().version, "4.36.0")
        for version in ("", "stable", "4.36.0-rc1", "4.37.0"):
            with patch.dict(os.environ, LEAN_GO_TOOLCHAIN=version), self.assertRaises(producer.ExportError):
                producer.selected_compiler()

    def test_failed_process_has_stderr(self):
        process = subprocess.CompletedProcess(["lean"], 9, "compiler stdout", "precise compiler stderr")
        with patch.object(producer.subprocess, "run", return_value=process):
            with self.assertRaisesRegex(producer.ExportError, "precise compiler stderr"):
                producer.run(["lean"], self.root, {})

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
