#!/usr/bin/env python3
"""Regenerate a copied flags demo with an installed CLI and run it with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/flags')
OUTPUT = (
    'set(0x00000001, 0x80000002) = 0x80000003\n'
    'contains(0x80000003, 0x80000002) = true\n'
    'clear(0x80000003, 0x00000002) = 0x80000001\n'
    'toggle(0x80000001, 0x80000000) = 0x00000001\n'
)
SPEC = shared.Demo('flags', 'Flags', ('Flags.clear', 'Flags.contains', 'Flags.set', 'Flags.toggle'), (
    ('FlagsProofs.clear_idempotent', 'FlagsContract.clear_idempotent'),
    ('FlagsProofs.clear_untouched', 'FlagsContract.clear_untouched'),
    ('FlagsProofs.cleared_bits_absent', 'FlagsContract.cleared_bits_absent'),
    ('FlagsProofs.contains_set', 'FlagsContract.contains_set'),
    ('FlagsProofs.set_idempotent', 'FlagsContract.set_idempotent'),
    ('FlagsProofs.set_untouched', 'FlagsContract.set_untouched'),
    ('FlagsProofs.toggle_involution', 'FlagsContract.toggle_involution'),
    ('FlagsProofs.toggle_untouched', 'FlagsContract.toggle_untouched'),
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
