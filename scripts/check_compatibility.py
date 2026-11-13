#!/usr/bin/env python3
"""Compare an explicitly installed Lean candidate against the strict production baseline."""
import argparse
import math
from pathlib import Path
import sys

from compatibility_identity import HarnessError
from compatibility_runner import run
from export import ExportError


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline-bin', type=Path, required=True, help='pinned Lean installation or bin directory')
    parser.add_argument('--candidate-bin', type=Path, help='existing candidate installation or bin directory; no downloads')
    parser.add_argument('--artifacts', type=Path, required=True, help='new output directory outside checkout/toolchains')
    parser.add_argument('--case', help='focus a replay on one named corpus case')
    parser.add_argument('--timeout', type=float, default=300, help='per-command process-tree timeout in seconds')
    args = parser.parse_args(argv)
    if not math.isfinite(args.timeout) or args.timeout <= 0:
        parser.error('--timeout must be finite and positive')
    try:
        code, report = run(args, Path(__file__).resolve().parents[1])
    except (HarnessError, ExportError, OSError, ValueError) as error:
        print(f'Compatibility harness precondition failed: {error}', file=sys.stderr)
        return 2
    print(f'Compatibility report: {report}')
    return code


if __name__ == '__main__':
    sys.exit(main())
