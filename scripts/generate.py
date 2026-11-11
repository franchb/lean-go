#!/usr/bin/env python3
"""Generate pure Go from fresh pinned Lean output and verify input freshness."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from export import ExportError
from export_module import (check_python, verify_inputs as verify_module_inputs,
                           validate_name, validate_output, source_snapshot, PRODUCER_INPUTS)

INPUT_FILES = (
    'feasibility/LeanGo/Feasibility/Model.lean',
    'producer/lean-toolchain', 'producer/lakefile.toml', 'producer/Export.lean',
    'producer/LeanGo/Pilot/Export.lean', 'producer/Reference.lean', 'scripts/export.py',
)


def verify_inputs(root, program, source_dir=None):
    if program.get("schema") in {"lean-go-ir/v2", "lean-go-ir/v3", "lean-go-ir/v4", "lean-go-ir/v5"}:
        if source_dir is None:
            raise ValueError("module source directory is required for module freshness checks")
        verify_module_inputs(root, source_dir, program)
        return
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
    parser.add_argument('--out', type=Path)
    parser.add_argument('--ir-output', type=Path)
    parser.add_argument('--source-dir', type=Path)
    parser.add_argument('--module')
    parser.add_argument('--root')
    args = parser.parse_args()
    check_python()
    selected = (args.source_dir is not None, args.module is not None, args.root is not None)
    if any(selected) and not all(selected):
        raise ValueError('--source-dir, --module and --root must be supplied together')
    module_mode = all(selected)
    if module_mode and (args.out is None or args.ir_output is None):
        raise ValueError('module generation requires explicit --out and --ir-output paths')
    args.out = args.out or Path('generated/normalizer')
    args.ir_output = args.ir_output or Path('testdata/normalizer.ir.json')
    root = Path(__file__).resolve().parents[1]
    output = args.out if args.out.is_absolute() else root / args.out
    ir_output = args.ir_output if args.ir_output.is_absolute() else root / args.ir_output
    validate_output_paths(output, ir_output)
    if module_mode:
        validate_name(args.module)
        validate_name(args.root)
        sources = source_snapshot(args.source_dir)
        if args.module not in sources:
            raise ValueError('requested source module does not exist')
        validate_output(ir_output, args.source_dir, root, allow_missing_parent=True)
        protected = [args.source_dir / relative for relative, _ in sources.values()]
        protected.extend(root / name for name in PRODUCER_INPUTS)
        if any(path.resolve().is_relative_to(output.resolve()) for path in protected):
            raise ValueError('package output overlaps source or trusted producer inputs')
    go = shutil.which('go')
    if go is None:
        raise ValueError('Go is unavailable; put the pinned Go executable on PATH')
    environment = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local')
    version = subprocess.run([go, 'version'], env=environment, check=True,
                             text=True, capture_output=True, timeout=30).stdout.strip()
    if not version.startswith('go version go1.27.4 '):
        raise ValueError(f'expected Go 1.27.4, got {version}')
    with tempfile.TemporaryDirectory(prefix='lean-go-generate-') as temporary:
        ir_path = Path(temporary) / 'program.json'
        script = 'export_module.py' if module_mode else 'export.py'
        command = [sys.executable, str(root / 'scripts' / script), '--output', str(ir_path)]
        if module_mode:
            command += ['--source-dir', str(args.source_dir.resolve()), '--module', args.module,
                        '--root', args.root]
        subprocess.run(command, cwd=root, env=environment, check=True, timeout=300)
        content = ir_path.read_bytes()
        program = json.loads(content)
        verify_inputs(root, program, args.source_dir)
        output.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run([go, 'run', './cmd/lean-go', '-in', str(ir_path),
                        '-out', str(output)], cwd=root, env=environment, check=True, timeout=300)
        publish_ir(ir_output, content)
    print(f'Generated {output}; recorded producer IR in {ir_output}')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired, ExportError) as error:
        print(f'generation failed: {error}', file=sys.stderr)
        sys.exit(1)
