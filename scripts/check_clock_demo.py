#!/usr/bin/env python3
"""Regenerate a copied clock demo with an installed CLI and run it with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/clock')
OUTPUT = (
    '0 s = 0 h 0 min 0 s = 0 s\n'
    '3661 s = 1 h 1 min 1 s = 3661 s\n'
    '86399 s = 23 h 59 min 59 s = 86399 s\n'
    '4294967295 s = 1193046 h 28 min 15 s = 4294967295 s\n'
)
SPEC = shared.Demo('clock', 'Clock', ('Clock.join', 'Clock.split'), (
    ('ClockProofs.minutesBelow60', 'ClockContract.minutesBelow60'),
    ('ClockProofs.recombines', 'ClockContract.recombines'),
    ('ClockProofs.secondsBelow60', 'ClockContract.secondsBelow60'),
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
