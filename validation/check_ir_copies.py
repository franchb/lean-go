#!/usr/bin/env python3
"""Check that validation/Examples/IR/*.ir.json still match the tracked IR.

The validated programs are copies (so that the evidence is reproducible while
other work regenerates the tracked files). This script compares the two
semantic sections the loader reads, `types` and `functions`, and ignores all
metadata. Exit status 1 lists every copy that drifted; rerun the copy and the
Lean build to re-validate.

Usage (from the repository root or from validation/):
    python3 validation/check_ir_copies.py
"""

import json
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parent

SOURCES = {
    "clamp.ir.json": "examples/proofs/clamp/clamp.ir.json",
    "flags.ir.json": "examples/proofs/flags/flags.ir.json",
    "words.ir.json": "examples/proofs/words/words.ir.json",
    "crc16.ir.json": "examples/proofs/crc16/crc16.ir.json",
    "adler32.ir.json": "examples/proofs/adler32/adler32.ir.json",
    "quota.ir.json": "testdata/quota.ir.json",
    "trees.ir.json": "testdata/trees.ir.json",
    "counters.ir.json": "testdata/counters.ir.json",
    "records.ir.json": "testdata/records.ir.json",
    "reduced.ir.json": "testdata/reduced.ir.json",
    "joins.ir.json": "testdata/joins.ir.json",
}


def semantic(path: pathlib.Path):
    data = json.loads(path.read_text())
    return {"types": data.get("types"), "functions": data.get("functions")}


def main() -> int:
    drift = []
    for copy, tracked in SOURCES.items():
        copy_path = HERE / "Examples" / "IR" / copy
        tracked_path = ROOT / tracked
        if not tracked_path.exists():
            drift.append(f"{tracked}: missing")
            continue
        if semantic(copy_path) != semantic(tracked_path):
            drift.append(f"{tracked}: differs from validation/Examples/IR/{copy}")
    for line in drift:
        print(line)
    if not drift:
        print(f"{len(SOURCES)} IR copies match their tracked programs")
    return 1 if drift else 0


if __name__ == "__main__":
    sys.exit(main())
