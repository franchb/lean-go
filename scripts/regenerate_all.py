#!/usr/bin/env python3
"""Regenerate every tracked module artifact through the real pipeline.

Each package, IR fixture and proof-evidence file is first produced in a fresh
temporary tree with scripts/generate.py and scripts/check_proofs.py. Tracked
copies are replaced only after every artifact succeeded; the command then lists
the artifacts whose bytes changed. The pilot normalizer is not covered.
"""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from check_checked_frame import COMPONENTS as FRAME_COMPONENTS, FRAME
from check_toolchain_demos import DEMOS
from export import ExportError
from export_module import check_python

ROOT = Path(__file__).resolve().parents[1]


@dataclass(frozen=True)
class Package:
    """One generated package, its IR fixture and optional bound proof evidence."""
    package: str
    ir: str
    source_dir: str
    module: str
    roots: tuple[str, ...]
    contract: str | None = None
    evidence: str | None = None


def fixture(name, module, roots, source_dir='examples/modules', **proofs):
    return Package(f'generated/{name}', f'testdata/{name}.ir.json', source_dir, module, roots, **proofs)


def demo(spec, directory):
    base = directory.as_posix()
    return Package(f'{base}/{spec.name}', f'{base}/{spec.name}.ir.json', f'{base}/source',
                   spec.module, spec.roots, f'{base}/proof-contract.json', f'{base}/proof-evidence.json')


PACKAGES = (
    fixture('choice', 'Choice.Transform', ('Choice.rotate',)),
    fixture('counters', 'Counters', ('Counters.advance', 'Counters.initial', 'Counters.read', 'Counters.total')),
    fixture('joins', 'Joins', ('Joins.update',)),
    fixture('local', 'Local', ('Local.run',)),
    fixture('numbers', 'Numbers', ('Numbers.compute',)),
    fixture('records', 'Records', ('Records.update',)),
    fixture('reduced', 'Reduced', ('Reduced.run',)),
    fixture('trees', 'Trees', ('Trees.mirror',)),
    fixture('wide', 'Wide', ('Wide.compute',)),
    fixture('quota', 'Quota', ('Quota.create', 'Quota.release', 'Quota.remaining', 'Quota.reserve'),
            source_dir='libraries/quota/source', contract='libraries/quota/proof-contract.json',
            evidence='libraries/quota/proof-evidence.json'),
    *(demo(module.SPEC, module.DEMO) for module in DEMOS),
)
FRAME_CONTRACT = 'examples/proofs/checkedframe-contract.json'
FRAME_EVIDENCE = (FRAME / 'proof-evidence.json').relative_to(ROOT).as_posix()


def run(command, label):
    result = subprocess.run(command, cwd=ROOT, check=False, text=True, capture_output=True)
    if result.returncode:
        raise ExportError(f'{label} failed:\n{result.stderr.strip() or result.stdout.strip()}')


def python(script, *args):
    return [sys.executable, str(ROOT / 'scripts' / script), *args]


def regenerate(spec, stage):
    command = python('generate.py', '--source-dir', spec.source_dir, '--module', spec.module,
                     '--out', str(stage / spec.package), '--ir-output', str(stage / spec.ir))
    for root in spec.roots:
        command.extend(['--root', root])
    run(command, spec.package)
    if spec.contract:
        (stage / spec.evidence).parent.mkdir(parents=True, exist_ok=True)
        run(python('check_proofs.py', '--contract', spec.contract,
                   '--manifest', str(stage / spec.package / 'manifest.json'),
                   '--output', str(stage / spec.evidence)), spec.evidence)
    print(f'regenerated {spec.package}', flush=True)


def check_frame(stage):
    components = {spec.package.rsplit('/', 1)[1]: spec for spec in PACKAGES}
    command = python('check_proofs.py', '--contract', FRAME_CONTRACT)
    for name in FRAME_COMPONENTS:
        command.extend(['--component-manifest', name,
                        str(stage / components[name].package / 'manifest.json')])
    (stage / FRAME_EVIDENCE).parent.mkdir(parents=True, exist_ok=True)
    run([*command, '--output', str(stage / FRAME_EVIDENCE)], FRAME_EVIDENCE)
    print(f'regenerated {FRAME_EVIDENCE}', flush=True)


def tree(path):
    if path.is_file():
        return {'': path.read_bytes()}
    if not path.exists():
        return None
    return {item.relative_to(path).as_posix(): item.read_bytes()
            for item in sorted(path.rglob('*')) if item.is_file()}


def replace(relative, stage):
    fresh, tracked = stage / relative, ROOT / relative
    if tree(fresh) == tree(tracked):
        return False
    if fresh.is_dir():
        if tracked.exists():
            shutil.rmtree(tracked)
        shutil.copytree(fresh, tracked)
    else:
        shutil.copyfile(fresh, tracked)
    return True


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--jobs', type=int, default=2, help='concurrent regenerations (default 2)')
    args = parser.parse_args(argv)
    check_python()
    with tempfile.TemporaryDirectory(prefix='lean-go-regenerate-') as directory:
        stage = Path(directory)
        with ThreadPoolExecutor(max(1, args.jobs)) as pool:
            for future in [pool.submit(regenerate, spec, stage) for spec in PACKAGES]:
                future.result()
        check_frame(stage)
        artifacts = [path for spec in PACKAGES
                     for path in (spec.package, spec.ir, spec.evidence) if path]
        artifacts.append(FRAME_EVIDENCE)
        changed = [path for path in artifacts if replace(path, stage)]
    for path in changed:
        print(f'changed {path}')
    print(f'{len(changed)} of {len(artifacts)} artifacts changed')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ExportError, OSError) as exc:
        print(f'regeneration failed; tracked artifacts unchanged: {exc}', file=sys.stderr)
        sys.exit(1)
