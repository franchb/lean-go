#!/usr/bin/env python3
"""Regenerate the UInt16 wire codec and verify its proofs and Go-only consumer."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/wire16')
OUTPUT = ('0xabcd -> [ab cd] -> 0xabcd\n'
          '[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]\n'
          '[02 80 00 00] -> unsupported version\n'
          '[01 04 00 00] -> reserved flags\n')
CLAIMS = ('encode_be_bytes', 'decode_be_value', 'decode_encode_be', 'encode_decode_be',
          'parse_success_iff', 'parse_unsupported_version_iff', 'parse_reserved_flags_iff',
          'encode_success_iff', 'encode_reserved_flags_iff',
          'parse_encode_roundtrip', 'encode_parse_roundtrip', 'need_more_iff', 'complete_iff')
SPEC = shared.Demo('wire16', 'Wire16',
                   ('Wire16.classify', 'Wire16.decodeBE', 'Wire16.encode', 'Wire16.encodeBE',
                    'Wire16.parse'),
                   tuple((f'Wire16Proofs.{name}', f'Wire16Contract.{name}') for name in CLAIMS),
                   extra_go_files=('frame/frame.go', 'frame/frame_test.go',
                                   'frame/append_test.go', 'frame/roundtrip_test.go'))


def artifacts(project):
    return shared.artifacts(project, SPEC.name)


def compare_artifacts(project, candidate):
    shared.compare_artifacts(project, candidate, SPEC.name)


def run_go_demo(project, go, environment, workspace):
    shared.run_go_demo(project, go, environment, workspace, OUTPUT, run)


def verify_demo(cli, workspace, environment):
    shared.verify_demo(cli, workspace, environment, ROOT / DEMO, SPEC,
                       run=run, pinned_go=pinned_go, consumer=run_go_demo)


if __name__ == '__main__':
    shared.main(verify_demo, SPEC.name)
