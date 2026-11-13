#!/usr/bin/env python3
"""Regenerate a copied clamp demo with an installed CLI and run it with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/clamp')
OUTPUT = (
    'clamp(7, 42) = 7\n'
    'clamp(100, 42) = 42\n'
    'clamp(18446744073709551615, 42) = 42\n'
)
SPEC = shared.Demo('clamp', 'Clamp', ('Clamp.clamp',), (
    ('ClampProofs.bounded', 'ClampContract.bounded'),
    ('ClampProofs.idempotent', 'ClampContract.idempotent'),
    ('ClampProofs.preserves', 'ClampContract.preserves'),
))


def artifacts(project):
    return shared.artifacts(project, SPEC.name)


def compare_artifacts(project, candidate):
    shared.compare_artifacts(project, candidate, SPEC.name)


def run_go_demo(project, go, environment, workspace):
    shared.run_go_demo(project, go, environment, workspace, OUTPUT, run)


def verify_demo(cli, workspace, environment):
    shared.verify_demo(cli, workspace, environment, ROOT / DEMO, SPEC,
                       run=run, pinned_go=pinned_go, consumer=run_go_demo)


def main():
    shared.main(verify_demo, SPEC.name)


if __name__ == '__main__':
    main()
