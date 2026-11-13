"""Fail-closed fixture outcomes and canonical compiler comparison evidence."""
from __future__ import annotations

import difflib
import json

from diagnostics import ADMISSION_CODES, SCHEMA, valid_diagnostic
from export import ExportError, parse_json
from compatibility_identity import HarnessError

IDENTITY_FIELDS = frozenset({'inputs', 'lean_version', 'lean_commit'})
PASS_STATUSES = {'accepted', 'expected_rejection'}


def extraction_outcome(result, expected_code=None):
    """Only the exact mapped admission protocol can satisfy a negative case."""
    if result.returncode == 0:
        return {'status': 'unexpected_acceptance' if expected_code else 'extracted'}
    try:
        report = parse_json(result.stderr)
        if (result.returncode != 1 or result.stdout or not isinstance(report, dict) or
                set(report) != {'schema', 'success', 'diagnostics'} or
                report['schema'] != SCHEMA or report['success'] is not False or
                not isinstance(report['diagnostics'], list) or len(report['diagnostics']) != 1 or
                not valid_diagnostic(report['diagnostics'][0], mapped=True)):
            raise ValueError('invalid failure protocol')
        item = report['diagnostics'][0]
    except (ExportError, ValueError, TypeError):
        return {'status': 'extraction_protocol_failed'}
    if item['stage'] != 'admission':
        return {'status': 'extraction_failed', 'diagnostic': item}
    matched = expected_code is not None and item['code'] == expected_code
    return {'status': 'expected_rejection' if matched else 'admission_mismatch', 'diagnostic': item}


def canonical_ir(program):
    """Exclude only top-level source/toolchain identity; keep all semantic evidence."""
    if (not isinstance(program, dict) or not IDENTITY_FIELDS <= program.keys() or
            not isinstance(program['inputs'], dict) or not isinstance(program.get('functions'), list)):
        raise HarnessError('invalid successful IR envelope')
    return json.dumps({key: value for key, value in program.items() if key not in IDENTITY_FIELDS},
                      sort_keys=True, ensure_ascii=False, indent=2) + '\n'


def ir_shapes(program, expected):
    """Require the compiler forms claimed by a fixture to survive optimization."""
    functions = {item['name'] for item in program['functions']}
    ops = set()
    calls = set()

    def visit(value):
        if isinstance(value, dict):
            if 'op' in value:
                ops.add(value['op'])
            if value.get('kind') == 'call':
                calls.add(value['name'])
            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)

    visit(program['functions'])
    missing = []
    for suffix in expected.get('called_function_suffixes', []):
        if not any(name.endswith(suffix) and name in calls for name in functions):
            missing.append('called function ending ' + suffix)
    missing.extend('control ' + op for op in expected.get('ops', []) if op not in ops)
    return missing


def compare_cases(baseline, candidate):
    differences = []
    for name, before in baseline.items():
        after = candidate[name]
        if after['status'] not in PASS_STATUSES:
            differences.append({'case': name, 'kind': after['status']})
        elif before['status'] != after['status']:
            differences.append({'case': name, 'kind': 'outcome_changed'})
        elif before.get('canonical_ir_sha256') != after.get('canonical_ir_sha256'):
            differences.append({'case': name, 'kind': 'canonical_ir_drift'})
    return differences


def ir_diff(before, after):
    return ''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                                      fromfile='baseline/program.canonical.json',
                                      tofile='candidate/program.canonical.json'))


def validate_cases(value):
    if not isinstance(value, list) or not value:
        raise HarnessError('corpus manifest must contain cases')
    seen = set()
    for case in value:
        if (not isinstance(case, dict) or set(case) - {'name', 'module', 'roots', 'reject', 'shapes'} or
                not {'name', 'module', 'roots'} <= case.keys()):
            raise HarnessError('invalid corpus case fields')
        name = case['name']
        if not isinstance(name, str) or not name or not all(c in 'abcdefghijklmnopqrstuvwxyz0123456789_-' for c in name) or name in seen:
            raise HarnessError('invalid or duplicate corpus name')
        seen.add(name)
        from export_module import select_roots, validate_name
        if not isinstance(case['module'], str):
            raise HarnessError('corpus module must be a string')
        validate_name(case['module'])
        if not isinstance(case['roots'], list):
            raise HarnessError('corpus roots must be a list')
        select_roots(case['roots'])
        if 'reject' in case and (not isinstance(case['reject'], str) or case['reject'] not in ADMISSION_CODES):
            raise HarnessError('negative fixture requires an explicit admission code')
        if 'shapes' in case:
            shapes = case['shapes']
            if (not isinstance(shapes, dict) or set(shapes) - {'ops', 'called_function_suffixes'} or
                    any(not isinstance(items, list) or not all(isinstance(item, str) for item in items)
                        for items in shapes.values())):
                raise HarnessError('invalid fixture shape requirements')
    return value
