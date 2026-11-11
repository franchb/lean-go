#!/usr/bin/env python3
"""Generate the audited pilot from fresh Lean output and verify input freshness."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

INPUT_FILES = (
    'feasibility/LeanGo/Feasibility/Model.lean',
    'producer/lean-toolchain', 'producer/lakefile.toml', 'producer/Export.lean',
    'producer/LeanGo/Pilot/Export.lean', 'producer/Reference.lean', 'scripts/export.py',
)


def verify_inputs(root, program):
    inputs = program.get('inputs')
    if not isinstance(inputs, dict) or set(inputs) != set(INPUT_FILES):
        raise ValueError('producer input key set mismatch')
    for name in INPUT_FILES:
        actual = hashlib.sha256((root / name).read_bytes()).hexdigest()
        if actual != inputs[name]:
            raise ValueError(f'producer input changed after export: {name}')


def validate_output_paths(output, ir_output):
    package = output.resolve()
    ir_file = ir_output.resolve()
    if package == ir_file or package in ir_file.parents or ir_file in package.parents:
        raise ValueError('package and IR output paths must not overlap')


def publish_ir(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        raise ValueError('IR output must not be a symlink')
    if path.exists() and path.read_bytes() == content:
        return
    fd, temporary = tempfile.mkstemp(prefix='.ir-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, default=Path('generated/normalizer'))
    parser.add_argument('--ir-output', type=Path, default=Path('testdata/normalizer.ir.json'))
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    output = args.out if args.out.is_absolute() else root / args.out
    ir_output = args.ir_output if args.ir_output.is_absolute() else root / args.ir_output
    validate_output_paths(output, ir_output)
    go = shutil.which('go')
    if go is None:
        raise ValueError('Go is unavailable; put the pinned Go executable on PATH')
    environment = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local')
    version = subprocess.run([go, 'version'], env=environment, check=True,
                             text=True, capture_output=True).stdout.strip()
    if not version.startswith('go version go1.27.4 '):
        raise ValueError(f'expected Go 1.27.4, got {version}')
    with tempfile.TemporaryDirectory(prefix='lean-go-generate-') as temporary:
        ir_path = Path(temporary) / 'program.json'
        subprocess.run([sys.executable, str(root / 'scripts/export.py'),
                        '--output', str(ir_path)], cwd=root, env=environment, check=True)
        content = ir_path.read_bytes()
        program = json.loads(content)
        verify_inputs(root, program)
        output.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run([go, 'run', './cmd/lean-go', '-in', str(ir_path),
                        '-out', str(output)], cwd=root, env=environment, check=True)
        publish_ir(ir_output, content)
    print(f'Generated {output}; recorded producer IR in {ir_output}')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f'generation failed: {error}', file=sys.stderr)
        sys.exit(1)
