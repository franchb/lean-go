"""A small installed front end to the original, hash-bound pipeline scripts."""
from __future__ import annotations

import argparse
from contextlib import contextmanager
from importlib import metadata, resources
import os
from pathlib import Path
import runpy
import shutil
import subprocess
import sys
import tempfile


def parser():
    result = argparse.ArgumentParser(prog='lean-go', description=__doc__)
    result.add_argument('--version', action='version',
                        version='%(prog)s ' + metadata.version('lean-go-tools'))
    commands = result.add_subparsers(dest='command', required=True)
    commands.add_parser('doctor', help='check Python, pinned Go, Lean and Lake')
    generate = commands.add_parser('generate', help='generate fresh pure Go from Lean source')
    generate.add_argument('--source-dir', type=Path)
    generate.add_argument('--module')
    generate.add_argument('--root', action='append', help='fully qualified root; repeat to share types')
    generate.add_argument('--out', type=Path, help='Go package output directory')
    generate.add_argument('--ir-output', type=Path, help='separate producer IR output file')
    generate.add_argument('--diagnostics', choices=('text', 'json'), default='text')
    proofs = commands.add_parser('check-proofs', help='freshly check and bind source proofs')
    proofs.add_argument('--contract', type=Path, required=True)
    proofs.add_argument('--manifest', type=Path, required=True)
    mode = proofs.add_mutually_exclusive_group(required=True)
    mode.add_argument('--output', type=Path, help='write freshly checked evidence')
    mode.add_argument('--evidence', type=Path, help='freshly verify existing evidence')
    return result


@contextmanager
def workspace():
    """Keep installed resources immutable, even when Go or Lake writes build state."""
    payload = metadata.distribution('lean-go-tools').locate_file('lean_go/_payload')
    with resources.as_file(payload) as source, tempfile.TemporaryDirectory(prefix='lean-go-cli-') as directory:
        # Tool-owned paths may contain a Windows short-name alias.
        destination = Path(directory).resolve() / 'tool'
        shutil.copytree(source, destination, copy_function=shutil.copyfile)
        for path in [destination, *(path for path in destination.rglob('*') if path.is_dir())]:
            path.chmod(0o700)
        yield destination


def environment(root):
    temporary = root / '.cache/tmp'
    temporary.mkdir(parents=True, exist_ok=True)
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('LEAN_', 'LAKE_', 'PYTHON'))}
    env.update(CGO_ENABLED='0', GOTOOLCHAIN='local', GOWORK='off', GOENV='off',
               GOFLAGS='', GOPROXY='off', GOSUMDB='off', PYTHONDONTWRITEBYTECODE='1',
               GOPATH=str(root / '.cache/gopath'), GOMODCACHE=str(root / '.cache/modules'),
               GOCACHE=str(root / '.cache/go'), TMPDIR=str(temporary),
               TEMP=str(temporary), TMP=str(temporary), GOTMPDIR=str(temporary))
    env['ELAN_TOOLCHAIN'] = (root / 'producer/lean-toolchain').read_text().strip()
    return env


def absolute(path):
    # Normalize ordinary '..' paths, but leave symlink evidence for admission.
    path = path.absolute()
    if any(part.is_symlink() for part in [path, *path.parents]):
        return str(path)
    return os.path.abspath(path)


def protect_install(paths):
    installed = {Path(__file__).resolve().parent,
                 Path(metadata.distribution('lean-go-tools').locate_file('lean_go')).resolve()}
    for path in paths:
        if path is None:
            continue
        target = path.resolve()
        if any(target.is_relative_to(root) or root.is_relative_to(target) for root in installed):
            raise ValueError('output must not overlap the installed lean-go package')


def pipeline_arguments(args):
    if args.command == 'check-proofs':
        protect_install([args.output, args.evidence])
        mode = '--output' if args.output is not None else '--evidence'
        return 'check_proofs.py', ['--contract', absolute(args.contract),
                                   '--manifest', absolute(args.manifest), mode,
                                   absolute(args.output or args.evidence)]
    module_mode = any(value is not None for value in (args.source_dir, args.module, args.root))
    output = args.out if module_mode else args.out or Path('generated/normalizer')
    ir = args.ir_output if module_mode else args.ir_output or Path('testdata/normalizer.ir.json')
    protect_install([output, ir])
    command = []
    for flag, value in [('--source-dir', args.source_dir), ('--out', output), ('--ir-output', ir)]:
        if value is not None:
            command.extend([flag, absolute(value)])
    if args.module is not None:
        command.extend(['--module', args.module])
    for entry in args.root or []:
        command.extend(['--root', entry])
    if args.diagnostics != 'text':
        command.extend(['--diagnostics', args.diagnostics])
    return 'generate.py', command


def probe(label, command, root, env, validate):
    try:
        completed = subprocess.run(command, cwd=root, env=env, capture_output=True,
                                   text=True, timeout=30, check=False)
        if completed.returncode:
            raise ValueError(completed.stderr.strip() or completed.stdout.strip() or
                             f'exit status {completed.returncode}')
        validate(completed.stdout)
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(f'FAIL {label}: {error}')
        return False
    print(f'OK {label}: {completed.stdout.strip()}')
    return True


def doctor(root, env):
    pins = runpy.run_path(str(root / 'scripts/export.py'))
    expected_go = 'go' + next(line.split()[1] for line in (root / 'go.mod').read_text().splitlines()
                             if line.startswith('go '))
    def go_version(text):
        if not text.startswith(f'go version {expected_go} '):
            raise ValueError(f'expected {expected_go}; got {text.strip()}')
    def lean_version(text):
        try:
            pins['check_version'](text)
        except pins['ExportError'] as error:
            raise ValueError(str(error)) from error
    healthy = sys.version_info[:2] == (3, 15)
    print(f"{'OK' if healthy else 'FAIL'} Python: {sys.version.split()[0]} (requires 3.15)")
    for label, name, arguments, check in [
        ('Go', 'go', ['version'], go_version),
        ('Lean', 'lean', ['--version'], lean_version),
        ('Lake environment', 'lake', ['env', 'lean', '--version'], lean_version),
    ]:
        executable = shutil.which(name)
        if executable is None:
            print(f'FAIL {label}: {name} is missing from PATH; see the CLI installation guide')
            healthy = False
        else:
            healthy = probe(label, [executable, *arguments], root / 'producer', env, check) and healthy
    return 0 if healthy else 1



def diagnostic_formatter():
    formatter = metadata.distribution('lean-go-tools').locate_file('lean_go/_payload/scripts/diagnostics.py')
    return runpy.run_path(str(formatter))


def render_failure(error, mode):
    diagnostic_formatter()['emit'](error, mode)

def main(argv=None):
    args = parser().parse_args(argv)
    try:
        script, arguments = pipeline_arguments(args) if args.command != 'doctor' else (None, None)
        with workspace() as root:
            env = environment(root)
            if args.command == 'doctor':
                return doctor(root, env)
            command = [sys.executable, '-B', str(root / 'scripts' / script), *arguments]
            if getattr(args, 'diagnostics', 'text') == 'json':
                result = subprocess.run(command, cwd=root, env=env, check=False,
                                        capture_output=True, text=True)
                return diagnostic_formatter()['forward_process'](result)
            return subprocess.run(command, cwd=root, env=env, check=False).returncode
    except (OSError, ValueError) as error:
        render_failure(error, getattr(args, 'diagnostics', 'text'))
        return 1
    except Exception as error:
        diagnostic_formatter()['unexpected'](error, getattr(args, 'diagnostics', 'text'))
        return 1
    except KeyboardInterrupt:
        return 130
