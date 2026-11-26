#!/usr/bin/env python3
"""Generate pure Go from a fresh pinned Lean module export and verify input freshness."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from export import ExportError, selected_compiler
from diagnostics import emit, requested_mode, stage, diagnostic, unexpected
from export_module import (export_module, check_python, verify_inputs as verify_module_inputs,
                           select_roots, validate_name, validate_output, source_snapshot, PRODUCER_INPUTS)

MODULE_SCHEMAS = frozenset({"lean-go-ir/v24"})


def verify_inputs(root, program, source_dir=None):
    schema = program.get("schema")
    if schema not in MODULE_SCHEMAS:
        raise ValueError(f"unsupported IR schema {schema!r}; regenerate the IR with the current lean-go tool")
    if source_dir is None:
        raise ValueError("module source directory is required for module freshness checks")
    verify_module_inputs(root, source_dir, program)


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

def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path)
    parser.add_argument('--ir-output', type=Path)
    parser.add_argument('--source-dir', type=Path)
    parser.add_argument('--module')
    parser.add_argument('--root', action='append',
                        help='fully qualified root; repeat for a shared multi-root package')
    parser.add_argument("--diagnostics", choices=("text", "json"), default="text")
    parser.add_argument('--mode', choices=('metered', 'native'), default='metered',
                        help='Go emission mode: resource-metered (default) or plain native Go')
    return parser


def main(argv=None):
    args = arguments().parse_args(argv)
    with stage("toolchain", "toolchain_mismatch"):
        check_python()
    selected = (args.source_dir is not None, args.module is not None, args.root is not None)
    if not all(selected):
        raise ValueError('--source-dir, --module and --root are required and must be supplied together')
    if args.out is None or args.ir_output is None:
        raise ValueError('module generation requires explicit --out and --ir-output paths')
    root = Path(__file__).resolve().parents[1]
    output = args.out if args.out.is_absolute() else root / args.out
    ir_output = args.ir_output if args.ir_output.is_absolute() else root / args.ir_output
    validate_output_paths(output, ir_output)
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
        compiler = selected_compiler()
        environment = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local',
                           ELAN_TOOLCHAIN=compiler.toolchain)
        version = run_process([go, 'version'], root, environment, 'toolchain').strip()
        if not version.startswith('go version go1.27.4 '):
            raise ValueError(f'expected Go 1.27.4, got {version}')
    with tempfile.TemporaryDirectory(prefix='lean-go-generate-') as temporary:
        ir_path = Path(temporary) / 'program.json'
        export_module(root, args.source_dir.resolve(), args.module, entries, ir_path)
        content = ir_path.read_bytes()
        program = json.loads(content)
        verify_inputs(root, program, args.source_dir)
        with stage("emission", "emission_failed"):
            output.parent.mkdir(parents=True, exist_ok=True)
        run_process([go, 'run', './cmd/lean-go', '-in', str(ir_path),
                        '-out', str(output), '-mode', args.mode], root, environment, 'emission')
        with stage('emission', 'emission_failed'):
            publish_ir(ir_output, content)
    print(f'Generated {output}; recorded producer IR in {ir_output}')


def go_cli(arguments, root=None):
    """Build this checkout's Go lean-go command and run it with arguments.

    The script entry points are thin wrappers over the Go pipeline, so every
    command-line use exercises one implementation. A fresh binary is built
    from the checkout (or installed payload) next to this script; Go's build
    cache keeps that fast.
    """
    root = Path(__file__).resolve().parents[1] if root is None else Path(root)
    with stage('toolchain', 'toolchain_mismatch'):
        go = shutil.which('go')
        if go is None:
            raise ValueError('Go is unavailable; put the pinned Go executable on PATH')
    environment = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='')
    with tempfile.TemporaryDirectory(prefix='lean-go-build-') as directory:
        binary = Path(directory) / ('lean-go.exe' if os.name == 'nt' else 'lean-go')
        build = subprocess.run([go, 'build', '-buildvcs=false', '-o', str(binary), './cmd/lean-go'],
                               cwd=root, env=environment, check=False, text=True, capture_output=True)
        if build.returncode:
            error = ValueError('building the Go lean-go command failed: ' +
                               (build.stderr.strip() or build.stdout.strip()))
            error.diagnostic = diagnostic(error, stage='toolchain', code='toolchain_failed')
            raise error
        return subprocess.run([str(binary), *arguments], env=environment, check=False).returncode


def go_main(argv=None):
    """Script entry point: module extraction runs in the Go lean-go command.

    Paths keep this script's conventions: --source-dir is relative to the
    working directory, --out and --ir-output to the repository root.
    """
    argv = sys.argv[1:] if argv is None else argv
    args = arguments().parse_args(argv)
    # Argument errors need no Go build; the Go command reports them identically.
    if None in (args.source_dir, args.module, args.root):
        emit(ValueError('--source-dir, --module and --root are required and must be supplied together'),
             args.diagnostics)
        return 1
    if args.out is None or args.ir_output is None:
        emit(ValueError('module generation requires explicit --out and --ir-output paths'), args.diagnostics)
        return 1
    root = Path(__file__).resolve().parents[1]
    command = ['generate']
    if args.source_dir is not None:
        command.extend(['--source-dir', str(args.source_dir.absolute())])
    if args.module is not None:
        command.extend(['--module', args.module])
    for entry in args.root or []:
        command.extend(['--root', entry])
    for flag, value in (('--out', args.out), ('--ir-output', args.ir_output)):
        if value is not None:
            command.extend([flag, str(value if value.is_absolute() else root / value)])
    # Checkout scripts and CI reproduce tracked bytes, so they require the pinned Go.
    command.extend(['--diagnostics', args.diagnostics, '--mode', args.mode, '--strict-go'])
    try:
        return go_cli(command, root)
    except (ValueError, OSError) as error:
        emit(error, args.diagnostics)
        return 1


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
    sys.exit(go_main())
