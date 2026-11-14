#!/usr/bin/env python3
"""Regenerate a copied bytes demo with an installed CLI and run it with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/bytes')
OUTPUT = (
    'LE(0x12345678) = [78 56 34 12] -> 0x12345678\n'
    'BE(0x12345678) = [12 34 56 78] -> 0x12345678\n'
)
SPEC = shared.Demo('bytes', 'ByteCodec', ('ByteCodec.decodeBE', 'ByteCodec.decodeLE', 'ByteCodec.encodeBE', 'ByteCodec.encodeLE'), (
    ('ByteCodecProofs.decodeLE_encodeLE', 'ByteCodecContract.decodeLE_encodeLE'),
    ('ByteCodecProofs.decodeBE_encodeBE', 'ByteCodecContract.decodeBE_encodeBE'),
    ('ByteCodecProofs.encodeLE_decodeLE', 'ByteCodecContract.encodeLE_decodeLE'),
    ('ByteCodecProofs.encodeBE_decodeBE', 'ByteCodecContract.encodeBE_decodeBE'),
    ('ByteCodecProofs.encodeLE_b0', 'ByteCodecContract.encodeLE_b0'),
    ('ByteCodecProofs.encodeLE_b1', 'ByteCodecContract.encodeLE_b1'),
    ('ByteCodecProofs.encodeLE_b2', 'ByteCodecContract.encodeLE_b2'),
    ('ByteCodecProofs.encodeLE_b3', 'ByteCodecContract.encodeLE_b3'),
    ('ByteCodecProofs.encodeBE_b0', 'ByteCodecContract.encodeBE_b0'),
    ('ByteCodecProofs.encodeBE_b1', 'ByteCodecContract.encodeBE_b1'),
    ('ByteCodecProofs.encodeBE_b2', 'ByteCodecContract.encodeBE_b2'),
    ('ByteCodecProofs.encodeBE_b3', 'ByteCodecContract.encodeBE_b3'),
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
