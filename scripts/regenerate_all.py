#!/usr/bin/env python3
"""Regenerate every tracked module artifact through the real pipeline.

This is a thin wrapper over `lean-go regenerate` (internal/artifacts), which
produces each package, IR fixture, proof-evidence file and wire-format example
in a fresh temporary tree, replaces tracked copies only after every artifact
succeeded and lists the artifacts whose bytes changed.
"""
from __future__ import annotations

import argparse
from pathlib import Path
import sys

from export_module import check_python
from generate import go_cli

ROOT = Path(__file__).resolve().parents[1]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--jobs', type=int, default=2, help='concurrent regenerations (default 2)')
    args = parser.parse_args(argv)
    check_python()
    return go_cli(['regenerate', '--jobs', str(max(1, args.jobs)), '--repo', str(ROOT)], ROOT)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError) as exc:
        print(f'regeneration failed; tracked artifacts unchanged: {exc}', file=sys.stderr)
        sys.exit(1)
