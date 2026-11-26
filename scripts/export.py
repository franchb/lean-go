"""Pinned compiler identity, strict JSON parsing, processes and atomic publication shared by the producer scripts."""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from typing import NamedTuple


class ExportError(ValueError):
    pass


class Compiler(NamedTuple):
    version: str
    commit: str

    @property
    def toolchain(self):
        return f"leanprover/lean4:v{self.version}"

    @property
    def pin_file(self):
        return (self.toolchain + "\n").encode()

    def check_version(self, text):
        pattern = rf"Lean \(version {re.escape(self.version)}, [^,\r\n]+, commit {self.commit}, Release\)"
        if not re.fullmatch(pattern, text.strip()):
            raise ExportError(f"unexpected Lean version or commit: {text.strip()}")


DEFAULT_COMPILER = Compiler("4.36.0", "611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9")
SUPPORTED_COMPILERS = (DEFAULT_COMPILER, Compiler("4.36.0-rc2", "b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a"))


def selected_compiler():
    version = os.environ.get("LEAN_GO_TOOLCHAIN", DEFAULT_COMPILER.version)
    for compiler in SUPPORTED_COMPILERS:
        if version == compiler.version:
            return compiler
    supported = ", ".join(pin.version for pin in SUPPORTED_COMPILERS)
    raise ExportError(f"unsupported LEAN_GO_TOOLCHAIN {version!r}; expected one of {supported}")


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
