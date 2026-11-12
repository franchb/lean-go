#!/usr/bin/env python3
"""Check quota using the reusable source-proof contract workflow."""
from __future__ import annotations

import argparse
from pathlib import Path
import sys

import check_proofs
from export import ExportError


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--manifest', type=Path, help='generated package manifest to bind')
    parser.add_argument('--output', type=Path, help='write freshly checked evidence')
    args = parser.parse_args(argv)
    root = args.root.absolute()
    manifest = args.manifest if args.manifest else root / 'generated/quota/manifest.json'
    output = args.output if args.output else root / 'libraries/quota/proof-evidence.json'
    return check_proofs.main([
        '--contract', str(root / 'libraries/quota/proof-contract.json'),
        '--manifest', str(manifest),
        '--output' if args.output else '--evidence', str(output),
    ])


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ExportError, OSError) as exc:
        print(f'quota proof check failed: {exc}', file=sys.stderr)
        sys.exit(1)
