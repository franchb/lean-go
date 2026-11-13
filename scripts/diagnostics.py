"""Versioned diagnostics shared by the scripts and the installed CLI boundary.

This module has no pipeline imports: the installed CLI can load its trusted copy
with runpy before it creates a mutable build workspace.
"""
from contextlib import contextmanager
import json
import re
import sys

SCHEMA = 'lean-go-diagnostics/v1'
ADMISSION_CODES = {'source_audit', 'unsupported_type', 'unsupported_signature',
                   'unsupported_lcnf_value', 'unsupported_lcnf_control'}
FIELDS = {'code', 'stage', 'message', 'declaration', 'compiler_form', 'location', 'hint'}


def diagnostic(message, *, stage='input', code='invalid_input', declaration=None,
               compiler_form=None, location=None, hint=None):
    return dict(code=code, stage=stage, message=str(message), declaration=declaration,
                compiler_form=compiler_form, location=location, hint=hint)


@contextmanager
def stage(name, code, hint=None):
    """Enrich an existing exception without changing its type or rejection reason."""
    try:
        yield
    except Exception as error:
        if not hasattr(error, 'diagnostic'):
            error.diagnostic = diagnostic(error, stage=name, code=code, hint=hint)
        raise


def report(error):
    item = getattr(error, 'diagnostic', None) or diagnostic(error)
    return {'schema': SCHEMA, 'success': False, 'diagnostics': [item]}


def emit(error, mode='text'):
    result = report(error)
    if mode == 'json':
        print(json.dumps(result, ensure_ascii=False, separators=(',', ':')), file=sys.stderr)
        return
    for item in result['diagnostics']:
        where = ''
        if item['location'] is not None:
            location = item['location']
            start = location['start']
            where = f"{location['path']}:{start['line']}:{start['column']}: "
        declaration = f" ({item['declaration']})" if item['declaration'] else ''
        form = f"; {item['compiler_form']}" if item['compiler_form'] else ''
        print(f"{where}{item['stage']} [{item['code']}{form}]{declaration}: {item['message']}", file=sys.stderr)
        if item['hint']:
            print(f"Hint: {item['hint']}", file=sys.stderr)


def valid_position(value, *, mapped):
    fields = {'line', 'column', 'utf16_column', 'byte_offset'} if mapped else {'line', 'column'}
    return (isinstance(value, dict) and set(value) == fields and
            all(type(value[key]) is int and value[key] >= (1 if key == 'line' else 0)
                for key in fields))


def valid_location(value, *, mapped):
    if value is None:
        return True
    path_key = 'path' if mapped else 'module'
    fields = {path_key, 'kind', 'declaration', 'start', 'end'}
    return (isinstance(value, dict) and set(value) == fields and
            isinstance(value[path_key], str) and isinstance(value['kind'], str) and
            value['kind'] in {'declaration', 'source_owner', 'compiler'} and
            (value['declaration'] is None or isinstance(value['declaration'], str)) and
            all(valid_position(value[key], mapped=mapped) for key in ('start', 'end')))


def valid_diagnostic(item, *, mapped):
    if not isinstance(item, dict) or set(item) != FIELDS:
        return False
    if not isinstance(item['code'], str) or re.fullmatch(r'[a-z][a-z0-9_]*', item['code']) is None:
        return False
    if not isinstance(item['stage'], str) or item['stage'] not in {'input', 'toolchain', 'build', 'admission', 'emission'}:
        return False
    if item['stage'] == 'admission' and item['code'] not in ADMISSION_CODES:
        return False
    return (isinstance(item['message'], str) and
            all(item[key] is None or isinstance(item[key], str)
                for key in ('declaration', 'compiler_form', 'hint')) and
            valid_location(item['location'], mapped=mapped))


def producer_diagnostic(value):
    """Only the exact producer protocol may label an error an admission rejection."""
    if not isinstance(value, dict) or set(value) != {'schema', 'diagnostic'} or value['schema'] != SCHEMA:
        raise ValueError('invalid producer diagnostic envelope')
    item = value['diagnostic']
    if not valid_diagnostic(item, mapped=False) or item['stage'] != 'admission' or not isinstance(item['hint'], str):
        raise ValueError('invalid producer diagnostic fields or classification')
    return dict(item)


def map_location(location, sources):
    if location is None or not valid_location(location, mapped=False) or location['module'] not in sources:
        return None
    relative, mapping = sources[location['module']]
    positions = {}
    for key in ('start', 'end'):
        raw = location[key]
        positions[key] = mapping.position(raw['line'], raw['column'])
        if positions[key] is None:
            return None
    if positions['end']['byte_offset'] < positions['start']['byte_offset']:
        return None
    return {'path': relative, 'kind': location['kind'],
            'declaration': location.get('declaration'), **positions}


def compiler_location(message, sources, workspace):
    """Match only explicit Lean/Lake position prefixes in the fresh workspace."""
    from pathlib import Path
    for module, (relative, _) in sources.items():
        absolute = str(workspace).rstrip('/\\') + '/' + relative
        paths = {relative, relative.replace('/', '\\'), absolute, absolute.replace('/', '\\')}
        # Path construction on the current host must also handle native separators.
        paths.add(str(Path(workspace) / relative))
        for path in paths:
            prefix = r'(?:^|\n)(?P<lake>error: )?' + re.escape(path)
            for match in re.finditer(prefix + r':(\d+):(\d+): (?P<direct>error: )?', message):
                if not match['lake'] and not match['direct']:
                    continue
                point = {'line': int(match[2]), 'column': int(match[3])}
                return map_location({'module': module, 'kind': 'compiler', 'declaration': None,
                                     'start': point, 'end': point}, sources)
    return None


def requested_mode(argv):
    """Match argparse's last explicit mode; the parser still owns validation."""
    for index in range(len(argv) - 1, -1, -1):
        value = argv[index]
        if value in {'--diagnostics=text', '--diagnostics=json'}:
            return value.partition('=')[2]
        if value == '--diagnostics' and index + 1 < len(argv) and argv[index + 1] in {'text', 'json'}:
            return argv[index + 1]
    return 'text'


def forward_process(result):
    """Validate a child report, retaining a structured fallback for child crashes."""
    if result.returncode == 0:
        print(result.stdout or '', end='')
        return 0
    try:
        value = json.loads(result.stderr)
        if (not isinstance(value, dict) or set(value) != {'schema', 'success', 'diagnostics'} or
                value.get('schema') != SCHEMA or value.get('success') is not False):
            raise ValueError('invalid child report')
        items = value.get('diagnostics')
        if not isinstance(items, list) or len(items) != 1 or not valid_diagnostic(items[0], mapped=True):
            raise ValueError('invalid child diagnostic')
    except (ValueError, TypeError):
        error = RuntimeError(result.stderr or result.stdout or f'pipeline exited with status {result.returncode}')
        error.diagnostic = diagnostic(error, stage='build', code='pipeline_failed',
            hint='The pipeline did not return a diagnostic report; inspect the failure and installed tools.')
        emit(error, 'json')
    else:
        print(json.dumps(value, ensure_ascii=False, separators=(',', ':')), file=sys.stderr)
    return result.returncode


def unexpected(error, mode):
    """Last-resort CLI boundary: an implementation exception is never admission."""
    error.diagnostic = diagnostic(error, stage='build', code='pipeline_failed',
        hint='The pipeline failed unexpectedly; report this error with the pinned tool versions.')
    emit(error, mode)
