"""Compile generated source independently with native Lean and the Go extractor."""
from __future__ import annotations

import json
import math
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time

from export import ExportError, TOOLCHAIN, check_version
from export_module import check_python


class BackendError(RuntimeError):
    """A failed build, execution or output protocol, never an accepted sample."""

    def __init__(self, stage: str, detail: str):
        self.stage = stage
        self.detail = detail
        super().__init__(f"{stage}: {detail}")


def run_process(command: list[str], *, cwd: Path, env: dict[str, str], data: str | None,
                timeout: float) -> subprocess.CompletedProcess:
    """Bound the compiler process tree on POSIX; kill the direct child elsewhere."""
    with subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                          start_new_session=os.name == "posix") as process:
        try:
            stdout, stderr = process.communicate(data, timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            if os.name == "posix":
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            else:
                process.kill()
            stdout, stderr = process.communicate()
            raise subprocess.TimeoutExpired(command, timeout, output=stdout, stderr=stderr) from exc
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def checked_values(value, count: int, width: int, stage: str) -> list[int]:
    """Check exact output shape without accepting bools or rounded wide numbers."""
    if not isinstance(value, list) or len(value) != count:
        raise BackendError(stage, f"expected {count} output values")
    if any(type(item) is not int or not 0 <= item < (1 << width) for item in value):
        raise BackendError(stage, f"expected exact unsigned {width}-bit integers")
    return value


def decode_results(native: str, go: str, count: int, width: int) -> dict[str, list[int]]:
    try:
        lean_values, go_values = json.loads(native), json.loads(go)
    except (ValueError, TypeError) as exc:
        raise BackendError("output", f"invalid result JSON: {exc}") from exc
    if not isinstance(go_values, dict) or set(go_values) != {"raw", "typed"}:
        raise BackendError("output", "Go result must contain exactly raw and typed arrays")
    return {
        "native": checked_values(lean_values, count, width, "native-output"),
        "raw": checked_values(go_values["raw"], count, width, "raw-output"),
        "typed": checked_values(go_values["typed"], count, width, "typed-output"),
    }


def reference_source(width: int, inputs: list[tuple[int, int]]) -> str:
    pairs = ",\n  ".join(f"({a}, {b})" for a, b in inputs)
    return f'''import Generated

def samples : List (UInt{width} × UInt{width}) := [
  {pairs}]

def main : IO Unit := do
  let values := samples.map (fun pair => toString (Generated.run pair.1 pair.2).toNat)
  IO.println ("[" ++ String.intercalate "," values ++ "]")
'''


def consumer_source(width: int) -> str:
    return f'''package main

import (
    "encoding/json"
    "os"
    x "differentialconsumer/extracted"
)

func main() {{
    var inputs [][2]uint{width}
    if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {{ panic(err) }}
    result := struct {{
        Raw []uint{width} `json:"raw"`
        Typed []uint{width} `json:"typed"`
    }}{{make([]uint{width}, len(inputs)), make([]uint{width}, len(inputs))}}
    limits := x.DefaultLimits()
    for i, pair := range inputs {{
        args := make([]x.Value, 2)
        for j, value := range pair {{
            v, err := x.Import(&x.Input{{Type: "UInt{width}", UInt{width}: value}}, limits)
            if err != nil {{ panic(err) }}
            args[j] = v
        }}
        raw, err := x.Run(args, limits)
        if err != nil {{ panic(err) }}
        value, ok := raw.UInt{width}()
        if !ok || raw.Type() != "UInt{width}" || len(raw.Fields()) != 0 {{ panic("invalid scalar result") }}
        result.Raw[i] = value
        typed, err := x.RunTyped(pair[0], pair[1], limits)
        if err != nil {{ panic(err) }}
        result.Typed[i] = typed
    }}
    if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {{ panic(err) }}
}}
'''


class Backend:
    """One clean compilation per candidate, with retained commands and artifacts."""

    def __init__(self, repo: Path, work: Path, timeout: float = 300):
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not math.isfinite(timeout) or timeout <= 0:
            raise ValueError("timeout must be a positive finite number of seconds")
        self.repo = Path(repo).resolve()
        self.work = Path(work).resolve()
        self.timeout = timeout
        self.last_work: Path | None = None
        self._tools: dict[str, str] = {}
        self._versions: dict[str, str] = {}
        self.environment = {key: value for key, value in os.environ.items()
                            if not key.startswith(("LEAN_", "LAKE_"))}
        self.environment.update(ELAN_TOOLCHAIN=TOOLCHAIN.decode().strip(), CGO_ENABLED="0",
                                GOTOOLCHAIN="local", GOWORK="off", GOPROXY="off", GOSUMDB="off",
                                GOFLAGS="")

    def _log(self, record: dict) -> None:
        assert self.last_work is not None
        with (self.last_work / "commands.jsonl").open("a", encoding="utf-8") as stream:
            stream.write(json.dumps(record, sort_keys=True) + "\n")

    def _run(self, stage: str, command: list[str], cwd: Path, *, data: str | None = None,
             go_only: bool = False) -> str:
        environment = self.environment.copy()
        if go_only:
            environment["PATH"] = str(cwd / "no-producer-tools")
        start = time.monotonic()
        record = {"stage": stage, "command": command, "cwd": str(cwd),
                  "timeout_seconds": self.timeout, "go_only": go_only,
                  "environment": {key: environment[key] for key in
                                  ("CGO_ENABLED", "GOTOOLCHAIN", "GOWORK", "GOPROXY", "ELAN_TOOLCHAIN")}}
        try:
            result = run_process(command, cwd=cwd, env=environment, data=data, timeout=self.timeout)
        except (OSError, subprocess.TimeoutExpired) as exc:
            record.update(error=str(exc), elapsed_seconds=time.monotonic() - start)
            for key in ("stdout", "stderr"):
                output = getattr(exc, key, None)
                if output is not None:
                    record[key] = output.decode(errors="replace") if isinstance(output, bytes) else output
            self._log(record)
            raise BackendError(stage, str(exc)) from exc
        record.update(returncode=result.returncode, stdout=result.stdout, stderr=result.stderr,
                      elapsed_seconds=time.monotonic() - start)
        self._log(record)
        if result.returncode:
            raise BackendError(stage, f"exit {result.returncode}: {result.stderr.strip()}\n{result.stdout.strip()}")
        return result.stdout

    def _preflight(self) -> None:
        try:
            check_python()
        except ExportError as exc:
            raise BackendError("toolchain", str(exc)) from exc
        if self._tools:
            return
        tools = {}
        for name in ("go", "lean", "lake"):
            tool = shutil.which(name)
            if tool is None:
                raise BackendError("toolchain", f"{name} executable is unavailable")
            tools[name] = tool
        assert self.last_work is not None
        lean = self._run("lean-version", [tools["lean"], "--version"], self.last_work)
        try:
            check_version(lean)
        except ExportError as exc:
            raise BackendError("toolchain", str(exc)) from exc
        go = self._run("go-version", [tools["go"], "version"], self.last_work).strip()
        if not go.startswith("go version go1.27.4 "):
            raise BackendError("toolchain", f"expected Go 1.27.4, got {go}")
        self._tools = tools
        self._versions = {"lean": lean.strip(), "go": go, "python": sys.version}

    def check(self, program, inputs: list[tuple[int, int]]) -> dict[str, list[int]]:
        from differential_model import render_lean

        width = program.width
        if width not in (32, 64) or not isinstance(inputs, list) or not 1 <= len(inputs) <= 4096:
            raise ValueError("expected width 32 or 64 and between 1 and 4096 input pairs")
        if any(not isinstance(pair, (list, tuple)) or len(pair) != 2 or
               any(type(value) is not int or not 0 <= value < (1 << width) for value in pair)
               for pair in inputs):
            raise ValueError("input pairs must contain exact unsigned integers of the program width")
        source = render_lean(program)
        self.work.mkdir(parents=True, exist_ok=True)
        self.last_work = Path(tempfile.mkdtemp(prefix="check-", dir=self.work))
        source_dir, native, consumer = (self.last_work / name for name in ("source", "native", "consumer"))
        for directory in (source_dir, native, consumer):
            directory.mkdir()
        (source_dir / "Generated.lean").write_text(source, encoding="utf-8")
        (native / "Generated.lean").write_text(source, encoding="utf-8")
        (native / "Reference.lean").write_text(reference_source(width, inputs), encoding="utf-8")
        (native / "lean-toolchain").write_bytes(TOOLCHAIN)
        (native / "lakefile.toml").write_text(
            'name = "differential_reference"\nversion = "0.0.0"\n'
            '[[lean_lib]]\nname = "Generated"\n'
            '[[lean_exe]]\nname = "reference"\nroot = "Reference"\n', encoding="utf-8")
        wire = json.dumps(inputs)
        (self.last_work / "inputs.json").write_text(wire + "\n", encoding="utf-8")
        self._preflight()
        (self.last_work / "toolchain.json").write_text(json.dumps(self._versions, indent=2) + "\n", encoding="utf-8")
        self._run("native-build", [self._tools["lake"], "build", "reference"], native)
        suffix = ".exe" if os.name == "nt" else ""
        native_output = self._run("native-run", [str(native / ".lake/build/bin" / ("reference" + suffix))], native)
        self._run("extract", [sys.executable, str(self.repo / "scripts/generate.py"),
                              "--source-dir", str(source_dir), "--module", "Generated", "--root", "Generated.run",
                              "--out", str(consumer / "extracted"), "--ir-output", str(self.last_work / "program.ir.json")], self.repo)
        (consumer / "go.mod").write_text("module differentialconsumer\n\ngo 1.27.4\n", encoding="utf-8")
        (consumer / "main.go").write_text(consumer_source(width), encoding="utf-8")
        binary = consumer / ("consumer" + suffix)
        self._run("go-build", [self._tools["go"], "build", "-buildvcs=false", "-o", str(binary), "."], consumer)
        go_output = self._run("go-run", [str(binary)], consumer, data=wire, go_only=True)
        results = decode_results(native_output, go_output, len(inputs), width)
        (self.last_work / "results.json").write_text(json.dumps(results, indent=2) + "\n", encoding="utf-8")
        return results
