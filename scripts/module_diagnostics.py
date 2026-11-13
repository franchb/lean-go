"""Interpret the fresh module producer's explicit failure protocol."""
from export import ExportError, parse_json
from diagnostics import compiler_location, diagnostic, map_location, producer_diagnostic


class DiagnosticError(ExportError):
    def __init__(self, item):
        super().__init__(item['message'])
        self.diagnostic = item


def run_compiler(run, command, workspace, env, sources, *, admission=False):
    protocol = workspace / 'lean-go-diagnostic.json'
    if admission:
        protocol.unlink(missing_ok=True)
    try:
        return run(command, workspace, env)
    except ExportError as error:
        if admission and protocol.is_file():
            try:
                if protocol.stat().st_size > 1024 * 1024:
                    raise ValueError('producer diagnostic exceeds limit')
                item = producer_diagnostic(parse_json(protocol.read_bytes()))
                item['location'] = map_location(item['location'], sources)
            except (OSError, ValueError, ExportError) as invalid:
                raise DiagnosticError(diagnostic(f'invalid producer diagnostic: {invalid}',
                    stage='build', code='producer_protocol')) from error
            raise DiagnosticError(item) from error
        raise DiagnosticError(diagnostic(str(error), stage='build', code='producer_failed',
            location=compiler_location(str(error), sources, workspace),
            hint='Check the Lean build error and the pinned toolchain; this is not an admission rejection.')) from error
