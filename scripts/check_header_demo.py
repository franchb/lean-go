#!/usr/bin/env python3
"""Regenerate the header codec, check its source proofs and deploy with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/header')
OUTPUT = ('[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]\n'
          '[02 80 00 00] -> unsupported version\n'
          '[01 04 00 00] -> reserved flags\n')
CLAIMS = ('parse_success_iff', 'parse_unsupported_version_iff', 'parse_reserved_flags_iff',
          'parsed_length_bound', 'encode_success_iff', 'encode_reserved_flags_iff',
          'encode_length_overflow_iff', 'parse_encode_roundtrip', 'encode_parse_roundtrip')
SPEC = shared.Demo('header', 'HeaderCodec', ('HeaderCodec.encode', 'HeaderCodec.parse'),
                   tuple((f'HeaderCodecProofs.{name}', f'HeaderCodecContract.{name}') for name in CLAIMS))


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
