"""Protocol, process isolation and native smoke tests for generated programs."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock

import differential_backend as backend
from export import COMMIT


class ResultProtocolTests(unittest.TestCase):
    def test_exact_uint64_values(self):
        values = [0, 9007199254740993, 18446744073709551615]
        self.assertEqual(backend.decode_results(json.dumps(values), json.dumps({"raw": values, "typed": values}), 3, 64),
                         {"native": values, "raw": values, "typed": values})

    def test_rejects_wrong_count_type_width_and_shape(self):
        for values in ([], [True], [1.0], [-1], [1 << 32], ["1"], None):
            with self.subTest(values=values), self.assertRaises(backend.BackendError):
                backend.decode_results(json.dumps(values), '{"raw":[1],"typed":[1]}', 1, 32)
        for raw in ('{}', '{"raw":[1]}', '{"raw":[1],"typed":[1],"extra":[]}',
                    '{"raw":[true],"typed":[1]}', '{"raw":[1],"typed":[1.0]}', 'noise'):
            with self.subTest(raw=raw), self.assertRaises(backend.BackendError):
                backend.decode_results('[1]', raw, 1, 32)


class ProcessTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.runner = backend.Backend(self.directory, self.directory / "attempts", timeout=2)
        self.runner.last_work = self.directory

    def test_forces_pure_go_and_logs_commands(self):
        result = subprocess.CompletedProcess(["program"], 0, "[1]", "warning")
        with mock.patch.object(backend, "run_process", return_value=result) as run:
            self.assertEqual(self.runner._run("example", ["program"], self.directory, data="input", go_only=True), "[1]")
        options = run.call_args.kwargs
        self.assertEqual(options["timeout"], 2)
        self.assertEqual(options["env"]["CGO_ENABLED"], "0")
        self.assertEqual(options["env"]["GOTOOLCHAIN"], "local")
        self.assertEqual(options["env"]["GOWORK"], "off")
        self.assertEqual(options["env"]["GOPROXY"], "off")
        self.assertEqual(options["env"]["PATH"], str(self.directory / "no-producer-tools"))
        self.assertEqual(options["data"], "input")
        record = json.loads((self.directory / "commands.jsonl").read_text())
        self.assertEqual(record["command"], ["program"])
        self.assertEqual(record["stdout"], "[1]")
        self.assertEqual(record["stderr"], "warning")

    def test_nonzero_exit_is_not_a_successful_sample(self):
        result = subprocess.CompletedProcess(["compiler"], 1, "context", "unsupported")
        with mock.patch.object(backend, "run_process", return_value=result), self.assertRaises(backend.BackendError) as caught:
            self.runner._run("extract", ["compiler"], self.directory)
        self.assertEqual(caught.exception.stage, "extract")
        self.assertIn("unsupported", str(caught.exception))

    def test_timeout_preserves_partial_diagnostics(self):
        failure = subprocess.TimeoutExpired(["compiler"], 2, output=b"started", stderr=b"slow")
        with mock.patch.object(backend, "run_process", side_effect=failure), self.assertRaises(backend.BackendError) as caught:
            self.runner._run("native-build", ["compiler"], self.directory)
        self.assertEqual(caught.exception.stage, "native-build")
        record = json.loads((self.directory / "commands.jsonl").read_text())
        self.assertEqual(record["stdout"], "started")
        self.assertEqual(record["stderr"], "slow")

    @unittest.skipUnless(os.name == "posix", "POSIX process-group timeout")
    def test_timeout_kills_grandchild_holding_output_pipe(self):
        script = ("import subprocess,sys,time; "
                  "subprocess.Popen([sys.executable,'-c','import time;time.sleep(30)']); "
                  "print('child started',flush=True); time.sleep(30)")
        start = time.monotonic()
        with self.assertRaises(subprocess.TimeoutExpired) as caught:
            backend.run_process([sys.executable, "-c", script], cwd=self.directory,
                                env=dict(os.environ), data=None, timeout=0.5)
        self.assertLess(time.monotonic() - start, 5)
        self.assertIn("child started", caught.exception.stdout)

    def test_missing_tool_and_wrong_version_fail(self):
        with mock.patch.object(backend.shutil, "which", side_effect=["/tools/go", None]), self.assertRaisesRegex(backend.BackendError, "unavailable"):
            self.runner._preflight()
        self.assertEqual(self.runner._tools, {})
        lean = f"Lean (version 4.35.0, x86_64, commit {COMMIT}, Release)"
        with mock.patch.object(backend.shutil, "which", side_effect=lambda name: "/tools/" + name), \
             mock.patch.object(self.runner, "_run", side_effect=[lean, "go version go1.26.0 linux/amd64"]), \
             self.assertRaisesRegex(backend.BackendError, "expected Go"):
            self.runner._preflight()
        self.assertEqual(self.runner._tools, {})

    def test_timeout_must_be_positive_finite(self):
        for timeout in (0, -1, float("nan"), float("inf"), True):
            with self.subTest(timeout=timeout), self.assertRaises(ValueError):
                backend.Backend(self.directory, self.directory, timeout)

    def test_lean_reference_has_exact_input_literals(self):
        source = backend.reference_source(64, [((1 << 64) - 1, (1 << 64) - 1)])
        self.assertIn("UInt64 × UInt64", source)
        self.assertIn("18446744073709551615", source)
        self.assertIn("Generated.run pair.1 pair.2", source)

    def test_check_builds_native_and_isolated_raw_typed_consumer(self):
        source = "namespace Generated\ndef run (a b : UInt32) : UInt32 := a + b\nend Generated\n"
        fake_model = SimpleNamespace(render_lean=lambda program: source)
        self.runner._tools = {name: "/tools/" + name for name in ("go", "lean", "lake")}
        responses = ["", "[3]", "", "", '{"raw":[3],"typed":[3]}']
        with mock.patch.dict(backend.sys.modules, differential_model=fake_model), \
             mock.patch.object(self.runner, "_run", side_effect=responses) as run:
            result = self.runner.check(SimpleNamespace(width=32), [(1, 2)])
        self.assertEqual(result, {name: [3] for name in ("native", "raw", "typed")})
        stages = [call.args[0] for call in run.call_args_list]
        self.assertEqual(stages, ["native-build", "native-run", "extract", "go-build", "go-run"])
        self.assertEqual(run.call_args_list[0].args[1], ["/tools/lake", "build", "reference"])
        self.assertTrue(run.call_args_list[-1].kwargs["go_only"])
        self.assertEqual(json.loads(run.call_args_list[-1].kwargs["data"]), [[1, 2]])
        check_dir = self.runner.last_work
        self.assertEqual([p.name for p in (check_dir / "source").iterdir()], ["Generated.lean"])
        self.assertEqual((check_dir / "source/Generated.lean").read_text(), (check_dir / "native/Generated.lean").read_text())
        self.assertIn("x.RunTyped", (check_dir / "consumer/main.go").read_text())
        self.assertIn("x.Run(args", (check_dir / "consumer/main.go").read_text())

    def test_rejects_invalid_inputs_before_processes(self):
        fake_model = SimpleNamespace(render_lean=mock.Mock())
        with mock.patch.dict(backend.sys.modules, differential_model=fake_model), \
             mock.patch.object(self.runner, "_run") as run:
            for inputs in ([], [(True, 0)], [(-1, 0)], [(1 << 32, 0)], [(1,)], [(1, 2)] * 4097):
                with self.subTest(inputs=inputs[:2]), self.assertRaises(ValueError):
                    self.runner.check(SimpleNamespace(width=32), inputs)
        run.assert_not_called()
        fake_model.render_lean.assert_not_called()


@unittest.skipUnless(os.environ.get("LEAN_GO_DIFFERENTIAL") == "1", "set LEAN_GO_DIFFERENTIAL=1 for native generated-program smoke")
class NativeSmokeTests(unittest.TestCase):
    def test_native_and_go_match_independent_model(self):
        from differential_model import evaluate, generate

        with tempfile.TemporaryDirectory(prefix="lean-go-differential-smoke-") as directory:
            runner = backend.Backend(Path(__file__).resolve().parents[1], Path(directory))
            seen_functions = set()
            for width in (32, 64):
                with self.subTest(width=width):
                    program = generate(0, width, depth=3)
                    maximum = (1 << width) - 1
                    inputs = [(0, 0), (0, maximum), (maximum, width + 1), (maximum - 1, width)]
                    expected = [evaluate(program, a, b) for a, b in inputs]
                    self.assertEqual(runner.check(program, inputs), {name: expected for name in ("native", "raw", "typed")})
                    # Coverage only: the independent semantic oracle above never reads IR.
                    ir = json.loads((runner.last_work / "program.ir.json").read_text())
                    self.assertTrue({"Generated.Box", "Generated.State"} <= {item["name"] for item in ir["types"]})
                    seen_functions.update(item["name"] for item in ir["functions"])
            self.assertTrue({"Generated.helper", "Generated.readState", "Generated.unbox"} <= seen_functions)


if __name__ == "__main__":
    unittest.main()
