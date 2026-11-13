#!/usr/bin/env python3
"""Exercise the development source-proof-to-Go walkthrough and print a JSON receipt."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import random
import shutil
import sys
import tempfile

from export import ExportError, parse_json, run
from export_module import check_python
from proof_contract import digest, load_contract, read_regular, snapshot

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = 'examples/proofs/clamp/proof-contract.json'
GO_VERSION = 'go1.27.4'
CANDIDATE = '0.1.0a6.dev0'


def base_environment():
    return dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local', GOWORK='off',
                GOENV='off', GOFLAGS='')


def pinned_go(root, environment):
    go = shutil.which('go')
    if go is None:
        raise ExportError('Go 1.27.4 must be available on PATH')
    info = parse_json(run([go, 'env', '-json', 'GOVERSION', 'GOROOT'], root, environment))
    if (not isinstance(info, dict) or info.get('GOVERSION') != GO_VERSION or
            not isinstance(info.get('GOROOT'), str)):
        raise ExportError('release walkthrough requires Go 1.27.4')
    executable = Path(info['GOROOT']) / 'bin' / ('go.exe' if os.name == 'nt' else 'go')
    if not executable.is_file():
        raise ExportError('pinned Go installation has no Go executable')
    return executable.resolve()


def copy_fixture(root, destination):
    contract = load_contract(root / FIXTURE)
    project = snapshot(contract)
    if contract.module != 'Clamp' or contract.roots != ('Clamp.clamp',):
        raise ExportError('release walkthrough fixture no longer selects Clamp.clamp')
    for directory, modules in [(contract.source_dir, project.runtime),
                               (contract.proof_dir, project.proofs)]:
        for relative, body in modules.values():
            target = destination / directory.relative_to(contract.path.parent) / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(body)
    target = destination / 'proof-contract.json'
    target.write_bytes(contract.raw)
    return load_contract(target)


def samples():
    bounds = [0, 1, 31, (1 << 32) - 1, (1 << 53) + 1, 1 << 63, (1 << 64) - 1]
    pairs = [(value, limit) for value in bounds for limit in bounds]
    rng = random.Random(35027)
    pairs.extend((rng.getrandbits(64), rng.getrandbits(64)) for _ in range(128))
    return [(value, limit, min(value, limit)) for value, limit in pairs]


def consumer_source(cases):
    rows = ',\n'.join(f'{{{value}, {limit}, {want}}}' for value, limit, want in cases)
    return ('''package main
import (
 "fmt"
 "os/exec"
 "runtime"
 clamp "example.com/proved-clamp/clamp"
)
func main() {
 if runtime.Version() != "go1.27.4" { panic("expected Go 1.27.4") }
 for _, tool := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
  if _, err := exec.LookPath(tool); err == nil { panic("producer executable available: " + tool) }
 }
 cases := [][3]uint64{
''' + rows + ''',
 }
 for _, test := range cases {
  got, err := clamp.RunTyped(test[0], test[1], clamp.DefaultLimits())
  if err != nil || got != test[2] { panic("clamp result differs from independent model") }
 }
 fmt.Printf("clamp Go-only consumer: %d cases OK\\n", len(cases))
}
''').encode()


def run_consumer(workspace, package, go, environment):
    consumer = workspace / 'consumer'
    consumer.mkdir()
    # Copy only deployment sources, with no producer tools or project configuration.
    (consumer / 'clamp').mkdir()
    for name in ('api.go', 'control.go', 'runtime.go', 'schema.go'):
        (consumer / 'clamp' / name).write_bytes(read_regular(package / name))
    (consumer / 'go.mod').write_text('module example.com/proved-clamp\n\ngo 1.27.4\n')
    cases = samples()
    source = consumer_source(cases)
    (consumer / 'main.go').write_bytes(source)
    isolated = dict(environment, PATH=str(go.parent), GOPROXY='off', GOSUMDB='off',
                    GOPRIVATE='', GONOPROXY='', GONOSUMDB='',
                    GOPATH=str(workspace / 'gopath'), GOMODCACHE=str(workspace / 'modules'),
                    GOCACHE=str(workspace / 'go-cache'))
    output = run([str(go), 'run', '-buildvcs=false', '-mod=readonly', '.'], consumer, isolated)
    if output != f'clamp Go-only consumer: {len(cases)} cases OK\n':
        raise ExportError('release consumer returned an unexpected result')
    return len(cases), digest(source)


def verify(root=ROOT):
    check_python()
    environment = base_environment()
    go = pinned_go(root, environment)
    with tempfile.TemporaryDirectory(prefix='lean-go-release-walkthrough-') as directory:
        workspace = Path(directory)
        contract = copy_fixture(root, workspace / 'project')
        package, ir = workspace / 'generated', workspace / 'clamp.ir.json'
        proof = workspace / 'proof-evidence.json'
        run([sys.executable, str(root / 'scripts/generate.py'), '--source-dir', str(contract.source_dir),
             '--module', contract.module, '--root', contract.roots[0],
             '--out', str(package), '--ir-output', str(ir)], root, environment)
        checker = [sys.executable, str(root / 'scripts/check_proofs.py'),
                   '--contract', str(contract.path), '--manifest', str(package / 'manifest.json')]
        run(checker + ['--output', str(proof)], root, environment)
        run(checker + ['--evidence', str(proof)], root, environment)
        proof_bytes = read_regular(proof)
        evidence = parse_json(proof_bytes)
        case_count, consumer_hash = run_consumer(workspace, package, go, environment)
        return {
            'schema': 'lean-go-release-walkthrough/v1', 'candidate': CANDIDATE,
            'status': 'passed', 'go_version': GO_VERSION,
            'python_version': '.'.join(str(part) for part in sys.version_info[:3]),
            'lean_version': evidence['lean_version'], 'lean_commit': evidence['lean_commit'],
            'module': contract.module, 'roots': list(contract.roots),
            'claims': evidence['claims'], 'consumer_cases': case_count,
            'contract_sha256': evidence['contract_sha256'],
            'project_inputs': evidence['project_inputs'], 'tool_inputs': evidence['tool_inputs'],
            'generated_manifest_sha256': evidence['generated_manifest_sha256'],
            'proof_evidence_sha256': digest(proof_bytes), 'ir_sha256': digest(read_regular(ir)),
            'verifier_sha256': digest(read_regular(root / 'scripts/check_release_candidate.py')),
            'consumer_source_sha256': consumer_hash,
            'assurance': evidence['assurance'],
        }


def main(argv=None):
    argparse.ArgumentParser(description=__doc__).parse_args(argv)
    print(json.dumps(verify(), sort_keys=True, indent=2, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ExportError, OSError) as exc:
        print(f'release walkthrough failed: {exc}', file=sys.stderr)
        sys.exit(1)
