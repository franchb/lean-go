from pathlib import Path
import re
import subprocess
import tempfile

source = Path("LeanGo/Feasibility/Model.lean").read_text()
semantic_old = "then .disj (normalizeAux true a) (normalizeAux true b)"
assert source.count(semantic_old) == 1
semantic = source.replace(
    semantic_old,
    "then .conj (normalizeAux true a) (normalizeAux true b)",
)
normalize_old = "def normalize (e : Expr) : Expr :=\n  normalizeAux false e"
assert source.count(normalize_old) == 1
shape = source.replace(normalize_old, "def normalize (e : Expr) : Expr :=\n  e")
proof_old = "  simpa [normalize] using normalizeAux_preserves_eval e rho false"
assert shape.count(proof_old) == 1
shape = shape.replace(proof_old, "  rfl")

cases = [
    ("wrong De Morgan rule", semantic, "theorem normalizeAux_preserves_eval",
     "theorem normalize_preserves_eval"),
    ("identity normalizer", shape, "theorem normalize_is_normal",
     "end LeanGo.Feasibility"),
]
for label, text, start, end in cases:
    lines = text.splitlines()
    lo = next(i + 1 for i, line in enumerate(lines) if line.startswith(start))
    hi = next(i + 1 for i, line in enumerate(lines) if line.startswith(end))
    with tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", dir=".", prefix="Mutant",
        suffix=".lean", delete=False,
    ) as stream:
        stream.write(text)
        path = Path(stream.name)
    try:
        run = subprocess.run(
            ["lake", "env", "lean", path.name],
            text=True, capture_output=True,
        )
        output = run.stdout + run.stderr
        errors = re.findall(r"^.*\berror:.*$", output, re.MULTILINE | re.IGNORECASE)
        positions = [int(n) for n in re.findall(
            r"^" + re.escape(path.name) + r":(\d+):\d+: error:",
            output, re.MULTILINE,
        )]
        if (
            run.returncode != 1
            or not positions
            or len(positions) != len(errors)
            or not all(lo <= n < hi for n in positions)
        ):
            print(output)
            raise SystemExit(f"{label}: expected theorem failure was not observed")
        print(f"{label}: rejected at the expected theorem")
    finally:
        path.unlink()
assert Path("LeanGo/Feasibility/Model.lean").read_text() == source
