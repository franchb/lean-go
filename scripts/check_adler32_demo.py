#!/usr/bin/env python3
"""Regenerate Adler-32, check its Lean claims and run the Go-only oracle."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/adler32')
OUTPUT = 'Adler-32("Wikipedia") = 0x11E60398\n'
SPEC = shared.Demo('adler32', 'Adler32', ('Adler32.checksum', 'Adler32.initial', 'Adler32.update'), (
    ('Adler32Proofs.chunks_spec', 'Adler32Contract.chunks_spec'),
    ('Adler32Proofs.step_spec', 'Adler32Contract.step_spec'),
))


def run_go_demo(project, go, environment, workspace):
    shared.run_go_demo(project, go, environment, workspace, OUTPUT, run)


def verify_demo(cli, workspace, environment):
    shared.verify_demo(cli, workspace, environment, ROOT / DEMO, SPEC,
                       run=run, pinned_go=pinned_go, consumer=run_go_demo)


if __name__ == '__main__':
    shared.main(verify_demo, SPEC.name)
