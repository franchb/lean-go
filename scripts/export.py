#!/usr/bin/env python3
"""Export the one audited Lean model from a fresh, pinned, isolated workspace."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

MODEL = "feasibility/LeanGo/Feasibility/Model.lean"
MODEL_SHA256 = "c67a835949d1786638d2809c970ef69861a074a23cf6fdbcae3f8cad1043771f"
COMMIT = "b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a"
TOOLCHAIN = b"leanprover/lean4:v4.36.0-rc2\n"
CONFIG = b'''name = "lean_go_producer"
version = "0.0.0"

[[lean_lib]]
name = "LeanGo"

[[lean_exe]]
name = "normalizer-reference"
root = "Reference"
'''
INPUTS = (MODEL, "producer/lean-toolchain", "producer/lakefile.toml",
          "producer/Export.lean", "producer/LeanGo/Pilot/Export.lean",
          "producer/Reference.lean", "scripts/export.py")


class ExportError(Exception):
    pass


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ExportError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def parse_json(data):
    try:
        return json.loads(data, object_pairs_hook=unique_object,
                          parse_constant=lambda value: (_ for _ in ()).throw(
                              ExportError(f"invalid JSON constant: {value}")))
    except (ValueError, UnicodeError, RecursionError) as exc:
        raise ExportError(f"invalid JSON: {exc}") from exc


def snapshot(root):
    result = {}
    for relative in INPUTS:
        path = root / relative
        if path.is_symlink() or not path.is_file():
            raise ExportError(f"missing or symlinked input: {relative}")
        result[relative] = path.read_bytes()
    if hashlib.sha256(result[MODEL]).hexdigest() != MODEL_SHA256:
        raise ExportError("model SHA-256 differs from the audited revision")
    if result["producer/lakefile.toml"] != CONFIG:
        raise ExportError("producer configuration differs from the fixed dependency-free TOML")
    if result["producer/lean-toolchain"] != TOOLCHAIN:
        raise ExportError("producer toolchain configuration differs from the pin")
    return result


def run(command, workspace, env):
    try:
        proc = subprocess.run(command, cwd=workspace, env=env, capture_output=True,
                              text=True, timeout=300, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ExportError(f"producer process failed: {exc}") from exc
    if proc.returncode:
        raise ExportError(f"producer process failed ({proc.returncode}): "
                          f"{proc.stderr.strip()}\n{proc.stdout.strip()}")
    return proc.stdout


def check_version(text):
    if not re.search(r"Lean \(version 4\.36\.0\-rc2,.*commit " + COMMIT + r", Release\)", text):
        raise ExportError(f"unexpected Lean version or commit: {text.strip()}")


def validate_reference(data):
    if len(data) > 1048576:
        raise ExportError("reference input exceeds 1 MiB")
    items = parse_json(data)
    if not isinstance(items, list) or len(items) > 1024:
        raise ExportError("reference input must be an array of at most 1024 expressions")
    def tree(value, depth):
        if depth > 64 or not isinstance(value, dict):
            raise ExportError("invalid reference tree or depth exceeds 64")
        tag = value.get("tag")
        if type(tag) is not int or tag not in range(5):
            raise ExportError("invalid reference tree tag")
        keys = ({"tag", "tri"}, {"tag", "id"}, {"tag", "left"},
                {"tag", "left", "right"}, {"tag", "left", "right"})[tag]
        if value.keys() != keys:
            raise ExportError("invalid reference tree fields")
        if tag < 2:
            scalar = value["tri" if tag == 0 else "id"]
            maximum = 2 if tag == 0 else 4294967295
            if type(scalar) is not int or not 0 <= scalar <= maximum:
                raise ExportError("invalid reference scalar range")
        else:
            tree(value["left"], depth + 1)
            if tag > 2:
                tree(value["right"], depth + 1)
    for item in items:
        tree(item, 1)


def stage_file(destination, data):
    destination = Path(destination).absolute()
    if destination.is_symlink() or (destination.exists() and not destination.is_file()):
        raise ExportError("output must be a regular file, not a symlink")
    if not destination.parent.is_dir():
        raise ExportError("output parent does not exist")
    fd, path = tempfile.mkstemp(prefix=".lean-go-export-", dir=destination.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
    except BaseException:
        Path(path).unlink(missing_ok=True)
        raise
    return Path(path)


def export(root, output, reference_input=None, reference_output=None):
    if bool(reference_input) != bool(reference_output):
        raise ExportError("reference input and output must be supplied together")
    if reference_output and Path(output).resolve() == Path(reference_output).resolve():
        raise ExportError("IR and reference outputs must differ")
    sources = snapshot(Path(root))
    reference = Path(reference_input).read_bytes() if reference_input else None
    if reference is not None:
        validate_reference(reference)
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("LEAN_", "LAKE_"))}
    env["ELAN_TOOLCHAIN"] = TOOLCHAIN.decode().strip()
    lean, lake = shutil.which("lean"), shutil.which("lake")
    if not lean or not lake:
        raise ExportError("pinned Lean and Lake executables must be available on PATH")
    with tempfile.TemporaryDirectory(prefix="lean-go-export-") as directory:
        workspace = Path(directory)
        for key, data in sources.items():
            if key == MODEL:
                target = workspace / "LeanGo/Feasibility/Model.lean"
            elif key.startswith("producer/"):
                target = workspace / key.removeprefix("producer/")
            else:
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        # This fixed library root is configuration scaffolding, never a user import.
        (workspace / "LeanGo.lean").write_bytes(b"import LeanGo.Feasibility.Model\n")
        check_version(run([lean, "--version"], workspace, env))
        check_version(run([lake, "env", "lean", "--version"], workspace, env))
        manifest = parse_json((workspace / "lake-manifest.json").read_bytes())
        if manifest.get("packages") != []:
            raise ExportError("producer dependency lock must have no packages")
        run([lake, "build", "LeanGo.Pilot.Export"], workspace, env)
        run([lake, "env", "lean", "Export.lean"], workspace, env)
        raw = (workspace / "ir.json").read_bytes()
        if len(raw) > 4194304:
            raise ExportError("producer IR exceeds 4 MiB")
        program = parse_json(raw)
        if not isinstance(program, dict) or set(program) != {"types", "functions", "claims", "rewrites"}:
            raise ExportError("unexpected producer result fields")
        program.update(schema="lean-go-ir/v1", profile="audited-normalizer/v1",
                       stage="pure-mono", lean_version="4.36.0-rc2", lean_commit=COMMIT,
                       model_sha256=MODEL_SHA256, root="LeanGo.Feasibility.normalize",
                       inputs={key: hashlib.sha256(data).hexdigest() for key, data in sources.items()})
        result = (json.dumps(program, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode()
        if len(result) > 4194304:
            raise ExportError("complete IR exceeds 4 MiB")
        reference_result = None
        if reference is not None:
            (workspace / "reference-input.json").write_bytes(reference)
            run([lake, "build", "normalizer-reference"], workspace, env)
            run([str(workspace / ".lake/build/bin/normalizer-reference"),
                 "reference-input.json", "reference-output.json"], workspace, env)
            reference_result = (workspace / "reference-output.json").read_bytes()
            parse_json(reference_result)
    publish_files([(output, result)] +
                  ([(reference_output, reference_result)] if reference_result is not None else []))


def publish_files(outputs):
    """Stage every result first; retain owned rollback copies through publication."""
    staged, backups, published = [], {}, []
    retained = set()
    try:
        for destination, data in outputs:
            destination = Path(destination)
            staged.append((stage_file(destination, data), destination))
            backups[destination] = (stage_file(destination, destination.read_bytes())
                                    if destination.exists() else None)
        try:
            for temporary, destination in staged:
                os.replace(temporary, destination)
                published.append(destination)
        except OSError as publication_error:
            rollback_errors = []
            for destination in reversed(published):
                backup = backups[destination]
                try:
                    if backup is None:
                        destination.unlink()
                    else:
                        os.replace(backup, destination)
                except OSError as rollback_error:
                    if backup is not None:
                        retained.add(backup)
                        recovery = f"; original bytes retained at {backup}"
                    else:
                        recovery = "; newly published output could not be removed"
                    rollback_errors.append(f"{destination}: {rollback_error}{recovery}")
            if rollback_errors:
                raise ExportError(f"publication failed: {publication_error}; rollback failed: " +
                                  "; ".join(rollback_errors)) from publication_error
            raise
    finally:
        for temporary in [item[0] for item in staged] + list(backups.values()):
            if temporary is not None and temporary not in retained:
                temporary.unlink(missing_ok=True)



def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--reference-input", type=Path)
    parser.add_argument("--reference-output", type=Path)
    args = parser.parse_args()
    try:
        export(Path(__file__).resolve().parents[1], args.output,
               args.reference_input, args.reference_output)
    except (ExportError, OSError) as exc:
        print(f"export failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
