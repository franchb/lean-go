#!/usr/bin/env python3
"""Run `#lean_go_validate` over every tracked module program; write a coverage table.

For each program (the fixtures and proof demos of regenerate_all.PACKAGES and
the three wire-format examples) this runs scripts/validate_module.py on the
tracked source, checks that the IR it exports is byte-identical to the
tracked IR, and collects the validation report. The output directory (default:
a fresh temporary directory, never inside the checkout) receives one
`<name>.validation.json` per program, `coverage.json` and `coverage.md`.

    uv run --locked python scripts/validation_coverage.py --out-dir /tmp/coverage

`--work-dir` keeps one Lake workspace per program between runs, so a rerun
rebuilds incrementally.
"""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import sys
import tempfile
import time

from export import ExportError
from export_module import check_python
from regenerate_all import PACKAGES
import validate_module

ROOT = Path(__file__).resolve().parents[1]
FORMATS = ('dns', 'header', 'udp')


def programs():
    result = []
    for spec in PACKAGES:
        name = Path(spec.ir).name.removesuffix('.ir.json')
        result.append((name, spec.source_dir, spec.module, list(spec.roots), spec.ir))
    for name in FORMATS:
        base = f'examples/formats/{name}'
        tracked = json.loads((ROOT / base / f'{name}.ir.json').read_text())
        result.append((f'format-{name}', f'{base}/source', tracked['module'], tracked['roots'],
                       f'{base}/{name}.ir.json'))
    return result


def run_one(entry, out_dir, work_dir, heartbeats, timeout_ms):
    name, source_dir, module, roots, tracked = entry
    ir = out_dir / f'{name}.ir.json'
    report = out_dir / f'{name}.validation.json'
    workspace = work_dir / name if work_dir else None
    start = time.monotonic()
    row = {'program': name, 'source': source_dir, 'module': module, 'roots': roots, 'ir': tracked}
    data = None
    for attempt in range(2):
        try:
            data = validate_module.validate_module(ROOT, ROOT / source_dir, module, roots, ir, report,
                                                   heartbeats=heartbeats, timeout_ms=timeout_ms,
                                                   workspace=workspace)
            break
        except (ExportError, OSError) as exc:
            # Exit 137 is the container's memory limit killing `lean`, not a
            # verdict on the proofs: retry once, then report it as such.
            killed = '(137)' in str(exc)
            row.update(status='killed_for_memory' if killed else 'error', error=str(exc)[:2000],
                       attempts=attempt + 1, wall_s=round(time.monotonic() - start, 1))
            if not killed:
                return row
    if data is None:
        return row
    row.pop('error', None)
    row['wall_s'] = round(time.monotonic() - start, 1)
    row['ir_identical_to_tracked'] = ir.read_bytes() == (ROOT / tracked).read_bytes()
    summary = data.get('summary', {})
    row.update(status=data.get('status'), ir_file_matches=data.get('ir_file_matches'),
               functions=summary.get('functions'), proved=summary.get('proved'),
               failed=summary.get('failed'), not_attempted=summary.get('not_attempted'),
               roots_total=summary.get('roots'), roots_proved=summary.get('roots_proved'),
               validation_ms=summary.get('time_ms'),
               unproved=[{'name': f['name'], 'kind': f['kind'], 'status': f['status'],
                          'reason': f['reason'][:600]}
                         for f in data.get('functions', []) if f['status'] != 'proved'])
    return row


def table(rows):
    lines = ['| Program | Functions proved / total | Roots with whole-program theorem | Validation time | IR identical |',
             '|---|---|---|---|---|']
    for row in rows:
        if row['status'] != 'completed':
            lines.append(f"| {row['program']} | {row['status']} | – | – | – |")
            continue
        seconds = row['validation_ms'] / 1000
        lines.append(f"| {row['program']} | {row['proved']} / {row['functions']} | "
                     f"{row['roots_proved']} / {row['roots_total']} | {seconds:.1f} s | "
                     f"{'yes' if row['ir_identical_to_tracked'] else 'NO'} |")
    return '\n'.join(lines) + '\n'


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--out-dir', type=Path)
    parser.add_argument('--work-dir', type=Path)
    parser.add_argument('--jobs', type=int, default=1)
    parser.add_argument('--only', action='append', help='restrict to these program names')
    parser.add_argument('--heartbeats', type=int, default=validate_module.DEFAULT_HEARTBEATS)
    parser.add_argument('--timeout-ms', type=int, default=validate_module.DEFAULT_TIMEOUT_MS)
    args = parser.parse_args(argv)
    check_python()
    out_dir = args.out_dir or Path(tempfile.mkdtemp(prefix='lean-go-validation-coverage-'))
    out_dir = out_dir.absolute()
    if out_dir.is_relative_to(ROOT):
        raise ExportError('write coverage output outside the checkout')
    out_dir.mkdir(parents=True, exist_ok=True)
    selected = [p for p in programs() if not args.only or p[0] in args.only]
    with ThreadPoolExecutor(max(1, args.jobs)) as pool:
        rows = list(pool.map(lambda p: run_one(p, out_dir, args.work_dir, args.heartbeats,
                                               args.timeout_ms), selected))
    for row in rows:
        print(f"{row['program']}: {row['status']} "
              f"{row.get('proved')}/{row.get('functions')} roots {row.get('roots_proved')}/{row.get('roots_total')}",
              flush=True)
    budget = {'heartbeats': args.heartbeats, 'timeout_ms': args.timeout_ms}
    (out_dir / 'coverage.json').write_text(json.dumps({'budget': budget, 'programs': rows},
                                                       indent=1, sort_keys=True) + '\n')
    (out_dir / 'coverage.md').write_text(table(rows))
    print(table(rows))
    print(f'reports in {out_dir}')
    return 0 if all(row['status'] == 'completed' for row in rows) else 1


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ExportError, OSError) as exc:
        print(f'validation coverage failed: {exc}', file=sys.stderr)
        sys.exit(1)
