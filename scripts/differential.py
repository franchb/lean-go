#!/usr/bin/env python3
"""Generate, compare, replay and shrink small Lean-to-Go program cases."""
import argparse
import json
from pathlib import Path
import sys
import tempfile
import time

from differential_backend import Backend, BackendError
from differential_cases import compare, load_case, reduce_case, save_case
from differential_model import generate, inputs, render_lean
from export import ExportError
from export_module import check_python


def bounded_int(lower, upper):
    def parse(value):
        number = int(value)
        if not lower <= number <= upper:
            raise argparse.ArgumentTypeError(f'must be in {lower}..{upper}')
        return number
    return parse


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument('--seed', type=bounded_int(0, 2 ** 64 - 1), action='append',
                        help='repeatable program seed (default: 0 and 1)')
    result.add_argument('--width', type=int, choices=(32, 64), action='append',
                        help='repeatable unsigned width (default: both)')
    result.add_argument('--depth', type=bounded_int(0, 4), default=3)
    result.add_argument('--cases', type=bounded_int(1, 4096), default=32,
                        help='total input pairs per program, including boundaries')
    result.add_argument('--artifacts', type=Path, default=Path('.differential'),
                        help='parent for persistent, uniquely named campaign artifacts')
    result.add_argument('--replay', type=Path, help='replay a saved case using its full AST and inputs')
    result.add_argument('--shrink', action='store_true', help='reduce the first semantic mismatch')
    result.add_argument('--shrink-attempts', type=bounded_int(0, 1000), default=20)
    result.add_argument('--timeout', type=bounded_int(1, 3600), default=300,
                        help='per-command timeout in seconds')
    return result


def selected_cases(args):
    if args.replay:
        if args.seed is not None or args.width is not None:
            raise ValueError('--replay cannot be combined with --seed or --width')
        yield load_case(args.replay)
        return
    for seed in args.seed if args.seed is not None else (0, 1):
        for width in args.width if args.width is not None else (32, 64):
            yield generate(seed, width, args.depth), inputs(seed, width, args.cases)


def run_case(program, pairs, directory, backend, args):
    directory.mkdir()
    save_case(directory / 'case.json', program, pairs)
    (directory / 'Generated.lean').write_text(render_lean(program), encoding='utf-8')
    try:
        observation = compare(program, pairs, backend.check(program, pairs))
    except (BackendError, ValueError, OSError) as error:
        save_case(directory / 'failure.json', program, pairs, error=error)
        return {'status': 'error', 'error': str(error), 'case': str(directory / 'failure.json')}
    if not observation.signature:
        save_case(directory / 'case.json', program, pairs, observation)
        return {'status': 'passed', 'inputs': len(pairs)}
    failure = directory / 'failure.json'
    save_case(failure, program, pairs, observation)
    result = {'status': 'mismatch', 'channels': observation.signature, 'case': str(failure)}
    if args.shrink:
        def check(candidate, values):
            return compare(candidate, values, backend.check(candidate, values))
        reduced = reduce_case(program, pairs, observation, check, args.shrink_attempts)
        path = directory / 'minimized.json'
        save_case(path, reduced.program, reduced.inputs, reduced.observation)
        (directory / 'Minimized.lean').write_text(render_lean(reduced.program), encoding='utf-8')
        result.update(minimized=str(path), attempts=reduced.attempts,
                      rejected_errors=reduced.rejected_errors)
    return result


def main(argv=None):
    args = parser().parse_args(argv)
    check_python()
    cases = list(selected_cases(args))
    args.artifacts.mkdir(parents=True, exist_ok=True)
    campaign = Path(tempfile.mkdtemp(prefix='campaign-', dir=args.artifacts.resolve()))
    repo = Path(__file__).resolve().parents[1]
    backend = Backend(repo, campaign / 'backend', timeout=args.timeout)
    started = time.monotonic()
    results = []
    for index, (program, pairs) in enumerate(cases):
        result = run_case(program, pairs, campaign / f'case-{index:04d}', backend, args)
        result.update(seed=program.seed, width=program.width)
        results.append(result)
        print(json.dumps(result), flush=True)
        if result['status'] != 'passed':
            break
    report = {'schema': 'lean-go-differential-campaign/v1', 'results': results,
              'elapsed_seconds': round(time.monotonic() - started, 3),
              'planned_programs': len(cases), 'completed_programs': len(results)}
    (campaign / 'report.json').write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
    print(f'Artifacts: {campaign}', flush=True)
    return int(any(item['status'] != 'passed' for item in results))


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, BackendError, ExportError) as error:
        print(f'differential campaign failed: {error}', file=sys.stderr)
        sys.exit(1)
