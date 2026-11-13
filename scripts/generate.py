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

from export import ExportError, TOOLCHAIN, check_version
from diagnostics import emit, requested_mode, stage, diagnostic, unexpected
from export_module import (export_module, check_python, verify_inputs as verify_module_inputs,
                           select_roots, validate_name, validate_output, source_snapshot, PRODUCER_INPUTS)

INPUT_FILES = (
    'feasibility/LeanGo/Feasibility/Model.lean',
    'producer/lean-toolchain', 'producer/lakefile.toml', 'producer/Export.lean',
    'producer/LeanGo/Pilot/Export.lean', 'producer/Reference.lean', 'scripts/export.py',
)


def verify_inputs(root, program, source_dir=None):
    if program.get("schema") in {"lean-go-ir/v2", "lean-go-ir/v3", "lean-go-ir/v4", "lean-go-ir/v5", "lean-go-ir/v6", "lean-go-ir/v7", "lean-go-ir/v8", "lean-go-ir/v9"}:
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



def run_process(command, root, environment, phase):
    """Capture all child output so JSON errors remain a single stderr report."""
    with stage(phase, 'toolchain_mismatch' if phase == 'toolchain' else phase + '_failed'):
        result = subprocess.run(command, cwd=root, env=environment, check=False,
                                text=True, capture_output=True, timeout=300)
        if result.returncode:
            error = subprocess.CalledProcessError(result.returncode, command,
                                                   result.stdout, result.stderr)
            error.diagnostic = diagnostic(result.stderr.strip() or result.stdout.strip() or str(error),
                                          stage=phase, code=phase + '_failed')
            raise error
        return result.stdout

def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path)
    parser.add_argument('--ir-output', type=Path)
    parser.add_argument('--source-dir', type=Path)
    parser.add_argument('--module')
    parser.add_argument('--root', action='append',
                        help='fully qualified root; repeat for a shared multi-root package')
    parser.add_argument("--diagnostics", choices=("text", "json"), default="text")
    args = parser.parse_args(argv)
    with stage("toolchain", "toolchain_mismatch"):
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
        entries = select_roots(args.root)
        sources = source_snapshot(args.source_dir)
        if args.module not in sources:
            raise ValueError('requested source module does not exist')
        validate_output(ir_output, args.source_dir, root, allow_missing_parent=True)
        protected = [args.source_dir / relative for relative, _ in sources.values()]
        protected.extend(root / name for name in PRODUCER_INPUTS)
        if any(path.resolve().is_relative_to(output.resolve()) for path in protected):
            raise ValueError('package output overlaps source or trusted producer inputs')
    with stage('toolchain', 'toolchain_mismatch'):
        go = shutil.which('go')
        if go is None:
            raise ValueError('Go is unavailable; put the pinned Go executable on PATH')
        environment = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local',
                           ELAN_TOOLCHAIN=TOOLCHAIN.decode().strip())
        version = run_process([go, 'version'], root, environment, 'toolchain').strip()
        if not version.startswith('go version go1.27.4 '):
            raise ValueError(f'expected Go 1.27.4, got {version}')
    with tempfile.TemporaryDirectory(prefix='lean-go-generate-') as temporary:
        ir_path = Path(temporary) / 'program.json'
        if module_mode:
            export_module(root, args.source_dir.resolve(), args.module, entries, ir_path)
        else:
            with stage('toolchain', 'toolchain_unavailable'):
                lean, lake = shutil.which('lean'), shutil.which('lake')
                if not lean or not lake:
                    raise ExportError('pinned Lean and Lake executables must be available on PATH')
            with stage('toolchain', 'toolchain_mismatch'):
                check_version(run_process([lean, '--version'], root, environment, 'toolchain'))
            run_process([sys.executable, str(root / 'scripts/export.py'), '--output', str(ir_path)],
                        root, environment, 'build')
        content = ir_path.read_bytes()
        program = json.loads(content)
        verify_inputs(root, program, args.source_dir)
        with stage("emission", "emission_failed"):
            output.parent.mkdir(parents=True, exist_ok=True)
        run_process([go, 'run', './cmd/lean-go', '-in', str(ir_path),
                        '-out', str(output)], root, environment, 'emission')
        with stage('emission', 'emission_failed'):
            publish_ir(ir_output, content)
    print(f'Generated {output}; recorded producer IR in {ir_output}')


def cli():
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired, ExportError) as error:
        emit(error, requested_mode(sys.argv[1:]))
        return 1
    except Exception as error:
        unexpected(error, requested_mode(sys.argv[1:]))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(cli())
