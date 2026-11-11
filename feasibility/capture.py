#!/usr/bin/env python3
"""Capture pinned compiler evidence; generated C is diagnostic material only."""

import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent
OUT = ROOT / ".evidence"
TEMP = ROOT / "ProbeSameFile.lean"
PREFIX = "LeanGo.Feasibility."
NORMALIZER = PREFIX + "normalize"
COMMIT = "eb6cafb7290e8bd5f2937522d8d804f58f02bafc"
SUCCESS = "capture checks passed; manual IR and primitive audit required"
MARKERS = (
    "CAPTURED", "CLOSURE_COMPLETE", "CODE", "EDGE", "CONSTRUCTOR",
    "REPLACEMENT", "EXTERNAL_LEAF", "EXTERN", "TYPE", "CTOR", "CTOR_TYPE",
    "SOURCE", "SOURCE_REPLACEMENT", "SOURCE_EXTERN", "AXIOMS_ACCEPTED",
)
PATTERN = re.compile(r"\b(" + "|".join(MARKERS) + r")\s+.*")
FLAGS = [
    "-Dcompiler.postponeCompile=false",
    "-Dcompiler.check=true",
    "-Dtrace.Compiler.saveBase=true",
    "-Dtrace.Compiler.saveMono=true",
]


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def run(report, label, command):
    entry = {"label": label, "argv": command, "exit_code": None,
             "log": f".evidence/{label}.log"}
    report["commands"].append(entry)
    try:
        result = subprocess.run(
            command, cwd=ROOT, text=True, stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, check=False,
        )
    except OSError as error:
        entry["error"] = f"{type(error).__name__}: {error}"
        (OUT / f"{label}.log").write_text(entry["error"] + "\n", encoding="utf-8")
        raise
    entry["exit_code"] = result.returncode
    (OUT / f"{label}.log").write_text(result.stdout, encoding="utf-8")
    report["inventories"][label] = inventory(result.stdout)
    report["join_edges"][label] = sorted(set(re.findall(
        r"\bJOIN_EDGE\s+[^\n]+", result.stdout,
    )))
    return result


def inventory(text):
    return sorted({match.group().strip() for line in text.splitlines()
                   if (match := PATTERN.search(line))})


def check_normalizer(label, result):
    require(result.returncode == 0, f"{label}: compiler failed; inspect log")
    records = inventory(result.stdout)
    for stage in ("base", "mono"):
        require(any(re.fullmatch(
            rf"CAPTURED {stage} {re.escape(NORMALIZER)} size=[1-9]\d*", row,
        ) for row in records), f"{label}: missing nonempty {stage} root")
        require(f"CLOSURE_COMPLETE {stage}" in records,
                f"{label}: incomplete {stage} closure")
    for theorem in ("normalize_preserves_eval", "normalize_is_normal"):
        require(any(re.fullmatch(
            rf"AXIOMS_ACCEPTED {re.escape(PREFIX + theorem)}: \[.*\]", row,
        ) for row in records), f"{label}: missing claim audit: {theorem}")
    for name in ("Tri", "Expr"):
        require(f"TYPE {PREFIX}{name} parameters=0" in records,
                f"{label}: missing original type metadata: {name}")
    constructors = {
        "Tri.f": (0, 0), "Tri.u": (1, 0), "Tri.t": (2, 0),
        "Expr.lit": (0, 1), "Expr.var": (1, 1), "Expr.neg": (2, 1),
        "Expr.conj": (3, 2), "Expr.disj": (4, 2),
    }
    for name, (tag, fields) in constructors.items():
        expected = f"CTOR {PREFIX}{name} tag={tag} parameters=0 fields={fields}"
        require(expected in records, f"{label}: incorrect or missing constructor: {name}")
    forbidden = ("eval", "normalizeAux_preserves_eval", "normalize_preserves_eval",
                 "normalizeAux_is_normal", "normalize_is_normal", "IsNormalForm",
                 "Tri.neg_neg", "Tri.neg_meet", "Tri.neg_join")
    runtime_rows = [row for row in records if row.startswith(("CODE ", "EDGE "))]
    require(not any(PREFIX + name in row for row in runtime_rows for name in forbidden),
            f"{label}: proof/reference code appears in runtime closure")
    require(all(" safe=true " in row for row in records if row.startswith("CODE ")),
            f"{label}: unsafe code in runtime closure")
    require(not any(row.startswith(("REPLACEMENT ", "EXTERNAL_LEAF ", "EXTERN ",
                                    "SOURCE_REPLACEMENT ", "SOURCE_EXTERN "))
                    for row in records), f"{label}: unexpected external or replacement")
    return records


def check_raw_roots(text):
    """Locate the actual root declaration inside each stage's own trace block."""
    roots = {}
    blocks = re.finditer(
        r"^\[Compiler.save(Base|Mono)\] size: (\d+)\n((?:[ \t].*(?:\n|$))*)",
        text, re.MULTILINE,
    )
    for block in blocks:
        body = block.group(3)
        if not re.match(r"\s+def " + re.escape(NORMALIZER) + r"\s", body):
            continue
        stage = block.group(1).lower()
        require(stage not in roots, f"Multiple raw {stage} root bodies")
        require(int(block.group(2)) > 0 and re.search(r"^\s+return \S", body, re.MULTILINE),
                f"Missing implementation in raw {stage} root body")
        roots[stage] = {"line": text.count("\n", 0, block.start()) + 1,
                        "size": int(block.group(2)),
                        "sha256": digest(body.encode())}
    require(set(roots) == {"base", "mono"}, "Missing actual raw base/mono root trace body")
    return roots


def check_bad_claim(result):
    diagnostics = re.findall(r"^.*\berror:.*$", result.stdout, re.MULTILINE | re.IGNORECASE)
    expected = re.findall(
        r"^BadClaim\.lean:\d+:\d+: error: Unexpected axiom BadClaim\.untrusted "
        r"in BadClaim\.advertised$", result.stdout, re.MULTILINE,
    )
    require(result.returncode == 1 and len(expected) == len(diagnostics) == 1,
            "Bad claim must fail only at its expected axiom audit; inspect log")


def compile_file(report, label, filename):
    return run(report, label, ["lake", "env", "lean", *FLAGS,
                               "-c", f".evidence/{label}.c", filename])


def capture(report):
    require(not TEMP.exists(), "Temporary probe already exists; inspect it first")
    for path in sorted(ROOT.rglob("*.lean")):
        relative = path.relative_to(ROOT)
        if not any(part in {".lake", ".evidence"} for part in relative.parts):
            report["inputs"][relative.as_posix()] = digest(path.read_bytes())
    for name in ("lean-toolchain", "lakefile.toml", "lake-manifest.json",
                 "capture.py", "check_mutations.py"):
        report["inputs"][name] = digest((ROOT / name).read_bytes())
    require((ROOT / "lean-toolchain").read_text().strip() == "leanprover/lean4:v4.35.0",
            "Unexpected toolchain pin")
    version = run(report, "version", ["lake", "env", "lean", "--version"])
    require(version.returncode == 0 and re.search(
        rf"^Lean \(version 4\.35\.0, [^,\n]+, commit {COMMIT}, Release\)$",
        version.stdout, re.MULTILINE,
    ), "Wrong or unavailable pinned Lean version/commit")
    for target in ("LeanGo", "LeanGo.Feasibility.ProbeSupport"):
        build = run(report, "build-" + target, ["lake", "build", target])
        require(build.returncode == 0, f"Build failed: {target}")

    sections = (ROOT / "Inspect.lean").read_text().split("-- PROBE ACTIONS")
    require(len(sections) == 2, "Expected exactly one probe-action marker")
    source = ("import LeanGo.Feasibility.ProbeSupport\n"
              + (ROOT / "LeanGo/Feasibility/Model.lean").read_text()
              + "\n" + sections[1])
    # Exclusive creation prevents overwriting a probe left by another invocation.
    with TEMP.open("x", encoding="utf-8") as stream:
        try:
            stream.write(source)
            stream.flush()
            report["inputs"][TEMP.name] = digest(TEMP.read_bytes())
            same = compile_file(report, "samefile", TEMP.name)
            same_records = check_normalizer("samefile", same)
            report["raw_roots"] = check_raw_roots(same.stdout)
            imported = compile_file(report, "imported", "Inspect.lean")
            imported_records = check_normalizer("imported", imported)
            require(same_records == imported_records,
                    "Same-file/imported inventories differ; inspect logs")
        finally:
            TEMP.unlink()

    foreign = compile_file(report, "foreign", "ForeignFixture.lean")
    require(foreign.returncode == 0, "Foreign fixture failed before classification")
    records = report["inventories"]["foreign"]
    require("SOURCE_EXTERN ForeignFixture.foreign" in records,
            "Foreign source attribute was missed")
    for stage in ("base", "mono"):
        require(f"EXTERNAL_LEAF {stage} ForeignFixture.foreign" in records,
                f"Foreign executable dependency was missed at {stage}")

    replaced = compile_file(report, "replacement", "ReplacementFixture.lean")
    require(replaced.returncode == 0, "Replacement fixture failed before classification")
    records = report["inventories"]["replacement"]
    edge = "ReplacementFixture.root -> ReplacementFixture.replacement"
    require("SOURCE_REPLACEMENT " + edge in records,
            "Original replacement attribute was missed")
    for stage in ("base", "mono"):
        require(f"REPLACEMENT {stage} {edge}" in records,
                f"Substitution was missed at {stage}")
    check_bad_claim(compile_file(report, "bad-claim", "BadClaim.lean"))


def main():
    report = {"commands": [], "inputs": {}, "inventories": {},
              "join_edges": {}, "verdict": "failed"}
    OUT.mkdir(exist_ok=True)
    try:
        capture(report)
        report["verdict"] = SUCCESS
    except Exception as error:
        report["error"] = f"{type(error).__name__}: {error}"
        print(report["error"], file=sys.stderr)
        return 1
    finally:
        (OUT / "capture.json").write_text(
            json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8",
        )
    print(SUCCESS)
    return 0


if __name__ == "__main__":
    sys.exit(main())
