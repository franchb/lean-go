#!/usr/bin/env python3
"""Regenerate CRC-16/XMODEM, check its Lean claims and run the Go streaming example."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/crc16')
OUTPUT = 'CRC-16/XMODEM("123456789") = 0x31C3\n'
SPEC = shared.Demo('crc16', 'Crc16', ('Crc16.update', 'Crc16.updateByte'), (
    ('Crc16Proofs.update_byte_spec', 'Crc16Contract.update_byte_spec'),
    ('Crc16Proofs.chunks_spec', 'Crc16Contract.chunks_spec'),
), extra_go_files=('checksum/checksum.go',))


def run_go_demo(project, go, environment, workspace):
    shared.run_go_demo(project, go, environment, workspace, OUTPUT, run)


def verify_demo(cli, workspace, environment):
    shared.verify_demo(cli, workspace, environment, ROOT / DEMO, SPEC,
                       run=run, pinned_go=pinned_go, consumer=run_go_demo)


if __name__ == '__main__':
    shared.main(verify_demo, SPEC.name)
