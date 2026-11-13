"""Explicit compiler identities and bounded, run-owned research pin adaptations."""
from __future__ import annotations

import difflib
import hashlib
import os
from pathlib import Path
import re
import tomllib

from export import COMMIT, TOOLCHAIN

PINNED_VERSION = TOOLCHAIN.decode().removeprefix("leanprover/lean4:v").strip()
PINNED_COMMIT = COMMIT
VERSION_PATTERN = r"4\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?"


class HarnessError(ValueError):
    """Invalid baseline, tool selection, corpus or harness precondition."""


def digest(data):
    return hashlib.sha256(data).hexdigest()


def parse_identity(output):
    match = re.fullmatch(
        rf"Lean \(version ({VERSION_PATTERN}), [^\r\n,]+, commit ([0-9a-f]{{40}}), Release\)\s*", output)
    if match is None:
        raise HarnessError("expected one release Lean identity with a full 40-digit commit")
    return {"version": match[1], "commit": match[2]}


def matching_identity(direct, through_lake):
    identity = parse_identity(direct)
    if parse_identity(through_lake) != identity:
        raise HarnessError("lean --version and lake env lean --version disagree")
    return identity


def compiler_bin(path):
    """Accept an installation or bin directory, never an elan shim selection."""
    path = Path(path).expanduser().resolve(strict=True)
    if (path / "bin").is_dir():
        path /= "bin"
    for name in ("lean", "lake"):
        executable = path / name
        if not executable.is_file() or executable.is_symlink() or not os.access(executable, os.X_OK):
            raise HarnessError(f"expected real {name} executable in explicit compiler bin directory")
    for name in ("lib/lean/Init.olean", "lib/lean/Lean.olean"):
        if not (path.parent / name).is_file():
            raise HarnessError("explicit compiler must have an installed Lean library; elan shims are not installations")
    return path


def source_snapshot(repo):
    """Use the reviewed wheel closure; do not copy caches or arbitrary checkout files."""
    config = tomllib.loads((repo / "pyproject.toml").read_text())
    names = config["tool"]["hatch"]["build"]["targets"]["wheel"]["force-include"]
    result = {}
    for name in sorted(names):
        path = repo / name
        if Path(name).is_absolute() or ".." in Path(name).parts or path.is_symlink() or not path.is_file():
            raise HarnessError(f"invalid trusted snapshot path: {name}")
        result[name] = path.read_bytes()
    return result


def write_snapshot(destination, snapshot):
    destination.mkdir()
    for name, data in snapshot.items():
        path = destination / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)


def pin_substitutions(identity):
    """Exact known identity literals only: no producer semantics or numeric audit edits."""
    version, commit = identity["version"], identity["commit"]
    parse_identity(f"Lean (version {version}, research, commit {commit}, Release)")
    return {
        "producer/lean-toolchain": [(TOOLCHAIN.decode(), f"leanprover/lean4:v{version}\n")],
        "scripts/export.py": [
            (f'COMMIT = "{PINNED_COMMIT}"', f'COMMIT = "{commit}"'),
            (f'TOOLCHAIN = b"leanprover/lean4:v{PINNED_VERSION}\\n"', f'TOOLCHAIN = b"leanprover/lean4:v{version}\\n"'),
            (rf'Lean \(version {re.escape(PINNED_VERSION)},.*commit ', rf'Lean \(version {re.escape(version)},.*commit '),
            (f'lean_version="{PINNED_VERSION}", lean_commit=COMMIT', f'lean_version="{version}", lean_commit=COMMIT'),
        ],
        "scripts/export_module.py": [
            (f"lean_version='{PINNED_VERSION}', lean_commit=COMMIT", f"lean_version='{version}', lean_commit=COMMIT"),
        ],
        "internal/moduleir/types.go": [
            (f'LeanVersion     = "{PINNED_VERSION}"', f'LeanVersion     = "{version}"'),
            (f'LeanCommit      = "{PINNED_COMMIT}"', f'LeanCommit      = "{commit}"'),
        ],
    }


def adapt_pins(snapshot, identity):
    """Validate every anchor before producing any edits, including same-pin runs."""
    adapted, changes, patches = dict(snapshot), [], []
    for name, replacements in pin_substitutions(identity).items():
        original = snapshot[name].decode()
        modified = original
        for old, new in replacements:
            if original.count(old) != 1:
                raise HarnessError(f"pin adaptation anchor drift: {name}: {old!r}")
            modified = modified.replace(old, new, 1)
        if modified != original:
            adapted[name] = modified.encode()
            changes.append({"path": name, "before_sha256": digest(snapshot[name]),
                            "after_sha256": digest(adapted[name])})
            patches.extend(difflib.unified_diff(original.splitlines(True), modified.splitlines(True),
                                               fromfile="a/" + name, tofile="b/" + name))
    patch = "".join(patches)
    return adapted, {"kind": "research_identity_only", "files": changes,
                     "patch_sha256": digest(patch.encode()), "semantic_adaptations": []}, patch
