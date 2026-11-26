#!/usr/bin/env python3
"""Export a Lean module and translation-validate the exported IR in one run.

This is the module export of export_module.py (same workspace, same export
harness, same IR) with the validation library added to the workspace and a
second `lean` process running the validation harness:

    import LeanGo.Module.Export
    import LeanGo.Validate
    import LeanGo.Validate.Command
    import Module
    #lean_go_validate "ir.json" [R1, ...] "Module" report "validation.json"
      heartbeats H timeout_ms T [strict] [skip "F ..."]

The command recomputes the producer's JSON value in that environment and
checks it against the ir.json the export process wrote. If the validation
process dies while proving an obligation (exit 137: the memory limit), it is
rerun with that obligation skipped, and the obligation is reported as failed.

The IR is the producer's ordinary output: its `inputs` digests cover exactly
export_module.PRODUCER_INPUTS, so a run with validation writes the same IR as
one without. The validation report additionally records the digests of the
validation library (VALIDATION_INPUTS). A failed or unattempted obligation is
reported, never an error, unless --strict is given.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from export import ExportError, parse_json, publish_files, selected_compiler
from diagnostics import emit, stage, unexpected
from module_diagnostics import run_compiler
import export_module as producer

# The object-level library, its automation and the producer-side command. Only
# copied into (and hashed for) workspaces that request validation.
VALIDATION_INPUTS = (
    "producer/LeanGo/Validate.lean",
    "producer/LeanGo/Validate/Attr.lean",
    "producer/LeanGo/Validate/Value.lean",
    "producer/LeanGo/Validate/Prim.lean",
    "producer/LeanGo/Validate/Syntax.lean",
    "producer/LeanGo/Validate/Semantics.lean",
    "producer/LeanGo/Validate/Soundness.lean",
    "producer/LeanGo/Validate/Spec.lean",
    "producer/LeanGo/Validate/Meta.lean",
    "producer/LeanGo/Validate/Meta/Derive.lean",
    "producer/LeanGo/Validate/Meta/Load.lean",
    "producer/LeanGo/Validate/Meta/Tactic.lean",
    "producer/LeanGo/Validate/Meta/Axioms.lean",
    "producer/LeanGo/Validate/Command.lean",
    "scripts/validate_module.py",
)
REPORT_SCHEMA = "lean-go-validation/v1"
DEFAULT_HEARTBEATS = 200000   # per obligation, in thousands (Lean's maxHeartbeats unit)
DEFAULT_TIMEOUT_MS = 120000   # per obligation, wall clock
MAX_REPORT_BYTES = 16 * 1024 * 1024
PROCESS_TIMEOUT = 3600
MAX_KILLED = 3   # obligations skipped after the validation process was killed


def validation_snapshot(root):
    result = {}
    for name in VALIDATION_INPUTS:
        path = Path(root) / name
        if path.is_symlink() or not path.is_file():
            raise ExportError(f"missing or symlinked validation input: {name}")
        result[name] = path.read_bytes()
    return result


def harness(module, entries, heartbeats, timeout_ms, strict, skip=()):
    """The validation harness. It runs in its own `lean` process after the
    unchanged export harness, so that nothing validation does (including the
    process being killed for memory) can affect the exported IR."""
    lines = ["import LeanGo.Module.Export", "import LeanGo.Validate", "import LeanGo.Validate.Command",
             f"import {module}"]
    validate = (f'#lean_go_validate "ir.json" [{", ".join(entries)}] "{module}" '
                f'report "validation.json" heartbeats {heartbeats} timeout_ms {timeout_ms}'
                + (" strict" if strict else "")
                + (f' skip "{" ".join(skip)}"' if skip else ""))
    return "\n".join(lines + [validate]) + "\n"


def last_attempt(progress):
    """The obligation a killed process was proving, from its progress file."""
    lines = progress.read_text().splitlines() if progress.is_file() else []
    for line in reversed(lines):
        _, _, message = line.partition(" ms ")
        if message.startswith("proving "):
            return message.removeprefix("proving ")
        if ": proved in " in message or ": failed in " in message:
            return None
    return None


def run(command, workspace, env):
    try:
        proc = subprocess.run(command, cwd=workspace, env=env, capture_output=True,
                              text=True, timeout=PROCESS_TIMEOUT, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ExportError(f"producer process failed: {exc}") from exc
    if proc.returncode:
        raise ExportError(f"producer process failed ({proc.returncode}): "
                          f"{proc.stderr.strip()}\n{proc.stdout.strip()}")
    return proc.stdout


def prepare(workspace, compiler, sources, files, env, lean):
    (workspace / "lean-toolchain").write_bytes(compiler.pin_file)
    (workspace / "lakefile.toml").write_bytes(producer.lake_config(sources))
    with stage("toolchain", "toolchain_mismatch"):
        compiler.check_version(run([lean, "--version"], workspace, env))
        prefix = Path(run([lean, "--print-prefix"], workspace, env).strip())
    mapped = producer.stage_sources(sources, workspace, prefix)
    for name, data in files.items():
        if name.startswith("producer/"):
            target = workspace / name.removeprefix("producer/")
            target.parent.mkdir(parents=True, exist_ok=True)
            if not target.is_file() or target.read_bytes() != data:
                target.write_bytes(data)
    return mapped


def validate_module(root, source_dir, module, entry, ir_output, report_output, *,
                    heartbeats=DEFAULT_HEARTBEATS, timeout_ms=DEFAULT_TIMEOUT_MS,
                    strict=False, workspace=None):
    """Run export plus validation. `workspace`, if given, is a persistent
    directory reused between runs so that Lake rebuilds incrementally."""
    with stage("toolchain", "toolchain_mismatch"):
        producer.check_python()
        compiler = selected_compiler()
    producer.validate_name(module)
    entries = producer.select_roots(entry)
    root, source_dir = Path(root), Path(source_dir)
    producer.validate_output(ir_output, source_dir, root)
    producer.validate_output(report_output, source_dir, root)
    sources = producer.source_snapshot(source_dir)
    if module not in sources:
        raise ExportError(f"requested module does not exist: {module}")
    trusted = producer.producer_snapshot(root)
    validation = validation_snapshot(root)
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("LEAN_", "LAKE_"))}
    env.update(ELAN_TOOLCHAIN=compiler.toolchain, CGO_ENABLED="0", GOTOOLCHAIN="local")
    lean, lake = shutil.which("lean"), shutil.which("lake")
    with stage("toolchain", "toolchain_unavailable"):
        if not lean or not lake:
            raise ExportError("pinned Lean and Lake executables must be available on PATH")
    with tempfile.TemporaryDirectory(prefix="lean-go-validate-") as directory:
        if workspace is None:
            workspace = Path(directory)
        else:
            workspace = Path(workspace).absolute()
            workspace.mkdir(parents=True, exist_ok=True)
        mapped = prepare(workspace, compiler, sources, {**trusted, **validation}, env, lean)
        run_compiler(run, [lake, "build", "LeanGo.Module.Export", "LeanGo.Validate",
                           "LeanGo.Validate.Command", *sorted(sources)], workspace, env, mapped)
        target = workspace / "LeanGo/Module/Harness.lean"
        target.write_text(producer.export_harness(module, entries))
        for name in ("ir.json", "validation.json", "validation.json.progress"):
            (workspace / name).unlink(missing_ok=True)
        run_compiler(run, [lake, "env", "lean", str(target.relative_to(workspace))],
                     workspace, env, mapped, admission=True)
        # Validation: a process killed while proving obligation F (exit 137 is
        # the memory limit) is rerun with F skipped; F is reported as failed
        # with that reason. Any other failure is reported, never raised,
        # unless `strict`.
        check = workspace / "LeanGo/Module/Validate.lean"
        report_file = workspace / "validation.json"
        killed = {}
        process_error = None
        for _ in range(MAX_KILLED + 1):
            check.write_text(harness(module, entries, heartbeats, timeout_ms, strict, sorted(killed)))
            report_file.unlink(missing_ok=True)
            try:
                run([lake, "env", "lean", str(check.relative_to(workspace))], workspace, env)
                process_error = None
                break
            except ExportError as exc:
                process_error = str(exc)
                victim = last_attempt(workspace / "validation.json.progress")
                if victim is None or victim in killed or report_file.is_file():
                    break
                code = process_error.partition("(")[2].partition(")")[0]
                killed[victim] = (f"validation process exited ({code}) while proving this obligation"
                                  + (" (memory limit)" if code == "137" else ""))
        if process_error and (strict or not report_file.is_file()):
            if strict:
                raise ExportError(f"validation failed: {process_error[:2000]}")
        with stage("build", "producer_protocol"):
            raw = (workspace / "ir.json").read_bytes()
            if len(raw) > producer.MAX_IR_BYTES:
                raise ExportError("producer IR exceeds 4 MiB")
            program = producer.complete_program(raw, module, entries,
                                                producer.input_digests(sources, trusted), compiler)
            ir = (json.dumps(program, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
                  + "\n").encode()
            if report_file.is_file():
                data = report_file.read_bytes()
                if len(data) > MAX_REPORT_BYTES:
                    raise ExportError("validation report exceeds 16 MiB")
                report = parse_json(data)
                if report.get("module") != module or report.get("roots") != entries:
                    raise ExportError("validation report names a different module or roots")
            else:
                report = {"module": module, "roots": entries, "status": "process_failed",
                          "reason": (process_error or "")[-4000:]}
            for function in report.get("functions", []):
                if function["name"] in killed:
                    function.update(status="failed", reason=killed[function["name"]])
            if killed and "summary" in report:
                summary = report["summary"]
                summary["not_attempted"] -= len(killed)
                summary["failed"] += len(killed)
            report.update(schema=REPORT_SCHEMA, lean_version=compiler.version,
                          lean_commit=compiler.commit,
                          ir_sha256=hashlib.sha256(ir).hexdigest(),
                          inputs=producer.input_digests(sources, {**trusted, **validation}))
            text = (json.dumps(report, sort_keys=True, indent=1, ensure_ascii=False) + "\n").encode()
        producer.verify_inputs(root, source_dir, program)
        if validation_snapshot(root) != validation:
            raise ExportError("validation inputs changed during the run")
    with stage("emission", "output_failed"):
        publish_files([(Path(ir_output), ir), (Path(report_output), text)])
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--source-dir", required=True, type=Path)
    parser.add_argument("--module", required=True)
    parser.add_argument("--root", required=True, action="append")
    parser.add_argument("--ir-output", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--heartbeats", type=int, default=DEFAULT_HEARTBEATS,
                        help="per-obligation heartbeat budget, in thousands (default %(default)s)")
    parser.add_argument("--timeout-ms", type=int, default=DEFAULT_TIMEOUT_MS,
                        help="per-obligation wall-clock budget (default %(default)s)")
    parser.add_argument("--strict", action="store_true",
                        help="fail when any obligation is not proved")
    parser.add_argument("--workspace", type=Path,
                        help="persistent workspace for incremental rebuilds (debugging, coverage)")
    parser.add_argument("--diagnostics", choices=("text", "json"), default="text")
    args = parser.parse_args()
    try:
        validate_module(Path(__file__).resolve().parents[1], args.source_dir, args.module,
                        args.root, args.ir_output, args.report, heartbeats=args.heartbeats,
                        timeout_ms=args.timeout_ms, strict=args.strict, workspace=args.workspace)
    except (ExportError, OSError) as exc:
        emit(exc, args.diagnostics)
        return 1
    except Exception as exc:
        unexpected(exc, args.diagnostics)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
