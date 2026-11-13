#!/usr/bin/env python3
"""Regenerate a copied words demo with an installed CLI and run it with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/words')
OUTPUT = (
    'widen(0x80000001) = 0x0000000080000001\n'
    'pack(0x80000001, 0xffffffff) = 0x80000001ffffffff\n'
    'high(0x80000001ffffffff) = 0x80000001\n'
    'low(0x80000001ffffffff) = 0xffffffff\n'
    'narrow(0x80000001ffffffff) = 0xffffffff\n'
)
SPEC = shared.Demo('words', 'Words', ('Words.high', 'Words.low', 'Words.narrow', 'Words.pack', 'Words.widen'), (
    ('WordsProofs.widen_preserves', 'WordsContract.widen_preserves'),
    ('WordsProofs.narrow_truncates', 'WordsContract.narrow_truncates'),
    ('WordsProofs.narrow_widen', 'WordsContract.narrow_widen'),
    ('WordsProofs.widen_narrow', 'WordsContract.widen_narrow'),
    ('WordsProofs.low_pack', 'WordsContract.low_pack'),
    ('WordsProofs.high_pack', 'WordsContract.high_pack'),
    ('WordsProofs.pack_unpack', 'WordsContract.pack_unpack'),
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
