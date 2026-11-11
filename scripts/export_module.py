#!/usr/bin/env python3
"""Export ordinary local Lean modules using fresh, audited compilation."""
from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import sys
import tempfile

from export import (COMMIT, TOOLCHAIN, ExportError, check_version, parse_json,
                    publish_files, run)
import json

MAX_FILES = 256
MAX_SOURCE_BYTES = 8 * 1024 * 1024
MAX_IR_BYTES = 4 * 1024 * 1024
PRODUCER_INPUTS = ("producer/LeanGo/Module/Audit.lean", "producer/LeanGo/Module/Export.lean",
                   "producer/LeanGo/Module/Primitives.lean",
                   "producer/LeanGo/Module/Records.lean",
                   "producer/LeanGo/Module/Reduced.lean",
                   "producer/LeanGo/Module/Local.lean",
                   "producer/LeanGo/Module/LocalSource.lean",
                   "producer/LeanGo/Module/LocalObserve.lean",
                   "scripts/export_module.py", "scripts/export.py")
IDENTIFIER = re.compile(r"[A-Za-z_][A-Za-z0-9_']*(?:\.[A-Za-z_][A-Za-z0-9_']*)*\Z", re.ASCII)
RESERVED = {"Lean", "Init", "Std", "LeanGo", "Lake"}
IGNORED = {".git", ".lake", ".venv", "__pycache__"}


def check_python(version=None):
    version = sys.version_info if version is None else version
    if tuple(version[:2]) != (3, 15):
        raise ExportError("Python 3.15 is required; use uv run --locked python")


def validate_name(name):
    if not IDENTIFIER.fullmatch(name):
        raise ExportError(f"invalid ASCII Lean name: {name!r}")
    return name


def source_snapshot(source_dir):
    """Read regular source files only; never follow source-directory symlinks."""
    source_dir = Path(source_dir).absolute()
    if source_dir.resolve() != source_dir or not source_dir.is_dir():
        raise ExportError("source directory must exist and must not contain symlinks")
    sources = {}
    total = 0
    for directory, dirs, files in os.walk(source_dir, followlinks=False):
        dirs[:] = sorted(name for name in dirs if name not in IGNORED)
        if any((Path(directory) / name).is_symlink() for name in dirs):
            raise ExportError("symlinked source directory is forbidden")
        for name in sorted(files):
            if not name.endswith(".lean") or name == "lakefile.lean":
                continue
            path = Path(directory) / name
            relative = path.relative_to(source_dir).as_posix()
            module = relative[:-5].replace("/", ".")
            validate_name(module)
            if module.split(".")[0] in RESERVED:
                raise ExportError(f"reserved source module: {module}")
            if path.is_symlink() or not path.is_file():
                raise ExportError(f"source must be a regular file: {relative}")
            if len(sources) >= MAX_FILES or total + path.stat().st_size > MAX_SOURCE_BYTES:
                raise ExportError("source snapshot exceeds file or byte limit")
            data = path.read_bytes()
            total += len(data)
            if total > MAX_SOURCE_BYTES:
                raise ExportError("source snapshot exceeds file or byte limit")
            if module in sources:
                raise ExportError(f"ambiguous source module: {module}")
            sources[module] = (relative, data)
    if not sources:
        raise ExportError("source directory has no Lean modules")
    return sources


def header_tokens(text):
    """Read header tokens lazily, respecting nested comments and source positions."""
    identifier = re.compile(r"[A-Za-z_][A-Za-z0-9_'.]*", re.ASCII)
    i = 0
    while i < len(text):
        if text[i].isspace():
            i += 1
        elif text.startswith("--", i):
            end = text.find("\n", i)
            i = len(text) if end < 0 else end + 1
        elif text.startswith("/-", i):
            depth = 1
            i += 2
            while depth and i < len(text):
                if text.startswith("/-", i):
                    depth += 1
                    i += 2
                elif text.startswith("-/", i):
                    depth -= 1
                    i += 2
                else:
                    i += 1
            if depth:
                raise ExportError("unterminated Lean header comment")
        else:
            match = identifier.match(text, i)
            end = match.end() if match else i + 1
            yield text[i:end], i, end
            i = end


def instrument_source(data):
    try:
        text = data.decode("utf-8")
    except UnicodeError as exc:
        raise ExportError("Lean source must be UTF-8") from exc
    tokens = iter(header_tokens(text))
    token = next(tokens, None)
    modern = bool(token and token[0] == "module")
    end = 0
    if modern:
        end = token[2]
        token = next(tokens, None)
    if token and token[0] == "prelude":
        end = token[2]
        token = next(tokens, None)
    imports = []
    while token:
        while token and token[0] in {"public", "private", "meta"}:
            token = next(tokens, None)
        if not token or token[0] != "import":
            break
        token = next(tokens, None)
        if token and token[0] == "all":
            token = next(tokens, None)
        if not token:
            raise ExportError("missing imported module name")
        imports.append(validate_name(token[0]))
        end = token[2]
        token = next(tokens, None)
    audit = "public meta import" if modern else "import"
    injection = f"\n{audit} LeanGo.Module.Audit\nset_option compiler.postponeCompile false\n"
    return (text[:end] + injection + text[end:]).encode(), imports


def producer_snapshot(root):
    result = {}
    for name in PRODUCER_INPUTS:
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise ExportError(f"missing or symlinked trusted producer input: {name}")
        result[name] = path.read_bytes()
    return result


def input_digests(sources, producer):
    logical = {"source/" + relative: data for relative, data in sources.values()}
    logical.update(producer)
    return {name: hashlib.sha256(data).hexdigest() for name, data in logical.items()}


def verify_inputs(root, source_dir, program):
    current = input_digests(source_snapshot(source_dir), producer_snapshot(Path(root)))
    if program.get("inputs") != current:
        raise ExportError("module producer inputs changed after export")


def validate_output(output, source_dir, root, *, allow_missing_parent=False):
    output = Path(output).absolute()
    if output.is_symlink() or (output.exists() and not output.is_file()):
        raise ExportError("output must be a regular file, not a symlink")
    if not allow_missing_parent and not output.parent.is_dir():
        raise ExportError("output parent does not exist")
    resolved = output.resolve()
    if resolved.is_relative_to(Path(source_dir).resolve()) and (
            resolved.suffix == ".lean" or resolved.name in {"lakefile.toml", "lean-toolchain"}):
        raise ExportError("output conflicts with source or project configuration")
    if resolved in {(Path(root) / name).resolve() for name in PRODUCER_INPUTS}:
        raise ExportError("output conflicts with trusted producer input")


def lake_config(sources):
    roots = sorted({name.split(".")[0] for name in sources} | {"LeanGo"})
    return ('name = "lean_go_module_producer"\nversion = "0.0.0"\n' +
            ''.join(f'\n[[lean_lib]]\nname = "{name}"\n' for name in roots)).encode()


def export_module(root, source_dir, module, entry, output):
    check_python()
    validate_name(module)
    validate_name(entry)
    root, source_dir = Path(root), Path(source_dir)
    validate_output(output, source_dir, root)
    sources = source_snapshot(source_dir)
    if module not in sources:
        raise ExportError(f"requested module does not exist: {module}")
    producer = producer_snapshot(root)
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("LEAN_", "LAKE_"))}
    env.update(ELAN_TOOLCHAIN=TOOLCHAIN.decode().strip(), CGO_ENABLED="0", GOTOOLCHAIN="local")
    lean, lake = shutil.which("lean"), shutil.which("lake")
    if not lean or not lake:
        raise ExportError("pinned Lean and Lake executables must be available on PATH")
    with tempfile.TemporaryDirectory(prefix="lean-go-module-") as directory:
        workspace = Path(directory)
        (workspace / "lean-toolchain").write_bytes(TOOLCHAIN)
        (workspace / "lakefile.toml").write_bytes(lake_config(sources))
        check_version(run([lean, "--version"], workspace, env))
        prefix = Path(run([lean, "--print-prefix"], workspace, env).strip())
        for name, (relative, data) in sources.items():
            instrumented, imports = instrument_source(data)
            for imported in imports:
                if imported in sources:
                    continue
                if imported.split(".")[0] not in {"Lean", "Init", "Std"} or not (
                        prefix / "lib/lean" / (imported.replace(".", "/") + ".olean")).is_file():
                    raise ExportError(f"unsupported or missing local import: {imported}")
            target = workspace / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(instrumented)
        for name, data in producer.items():
            if name.startswith("producer/"):
                target = workspace / name.removeprefix("producer/")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(data)
        check_version(run([lake, "env", "lean", "--version"], workspace, env))
        manifest = parse_json((workspace / "lake-manifest.json").read_bytes())
        if manifest.get("packages") != []:
            raise ExportError("producer dependency lock must have no packages")
        run([lake, "build", "LeanGo.Module.Export", *sorted(sources)], workspace, env)
        harness = workspace / "LeanGo/Module/Harness.lean"
        harness.write_text(f'import LeanGo.Module.Export\nimport {module}\n'
                           f'#lean_go_export "ir.json" {entry} "{module}"\n')
        run([lake, "env", "lean", str(harness.relative_to(workspace))], workspace, env)
        raw = (workspace / "ir.json").read_bytes()
        if len(raw) > MAX_IR_BYTES:
            raise ExportError("producer IR exceeds 4 MiB")
        program = parse_json(raw)
        fields = {"module", "root", "types", "functions", "claims", "rewrites", "provenance", "api"}
        if not isinstance(program, dict) or set(program) != fields:
            raise ExportError("unexpected module producer result fields")
        if program["module"] != module or program["root"] != entry:
            raise ExportError("module producer returned a different module or root")
        program.update(schema="lean-go-ir/v6", profile="pure-module/v4", stage="pure-mono",
                       lean_version="4.35.0", lean_commit=COMMIT,
                       inputs=input_digests(sources, producer))
        result = (json.dumps(program, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode()
        if len(result) > MAX_IR_BYTES:
            raise ExportError("complete IR exceeds 4 MiB")
        verify_inputs(root, source_dir, program)
    publish_files([(output, result)])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-dir", required=True, type=Path)
    parser.add_argument("--module", required=True)
    parser.add_argument("--root", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        export_module(Path(__file__).resolve().parents[1], args.source_dir,
                      args.module, args.root, args.output)
    except (ExportError, OSError) as exc:
        print(f"module export failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
