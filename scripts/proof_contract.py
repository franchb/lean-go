"""Strict project contracts and generated-package binding for source proofs."""
from __future__ import annotations

from dataclasses import dataclass
import hashlib
from pathlib import Path, PurePosixPath

from export import COMMIT, ExportError, parse_json
from export_module import (MAX_FILES, MAX_SOURCE_BYTES, select_roots,
                           source_snapshot, validate_name)

SCHEMA = 'lean-go-proof-contract/v1'
MAX_DOCUMENT_BYTES = 4 * 1024 * 1024
MAX_CONTRACT_BYTES = 64 * 1024
MAX_CLAIMS = 64
GENERATED = frozenset({'api.go', 'control.go', 'runtime.go', 'schema.go'})
FIELDS = frozenset({'schema', 'source_dir', 'proof_dir', 'module', 'roots', 'claims'})


@dataclass(frozen=True)
class Contract:
    path: Path
    raw: bytes
    source_dir: Path
    proof_dir: Path
    module: str
    roots: tuple[str, ...]
    claims: tuple[tuple[str, str], ...]

@dataclass
class ProjectSnapshot:
    runtime: dict[str, tuple[str, bytes]]
    proofs: dict[str, tuple[str, bytes]]

    @property
    def modules(self):
        return self.runtime | self.proofs

    @property
    def inputs(self):
        return {prefix + '/' + relative: data
                for prefix, modules in [('source', self.runtime), ('proof', self.proofs)]
                for relative, data in modules.values()}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read_regular(path, limit=MAX_DOCUMENT_BYTES):
    path = Path(path).absolute()
    try:
        resolved = path.resolve()
    except ValueError as exc:
        raise ExportError('invalid input path') from exc
    if resolved != path or not path.is_file():
        raise ExportError(f'missing, noncanonical or symlinked input: {path.name}')
    if path.stat().st_size > limit:
        raise ExportError(f'input exceeds byte limit: {path.name}')
    data = path.read_bytes()
    if len(data) > limit:
        raise ExportError(f'input exceeds byte limit: {path.name}')
    return data


def project_directory(root, value):
    if (not isinstance(value, str) or not value or value.startswith('/') or
            any(char in '\\:<>"|?*' or ord(char) < 32 or ord(char) == 127 or
                0xD800 <= ord(char) <= 0xDFFF for char in value) or
            any(part in {'', '.', '..'} or part.endswith((' ', '.')) for part in value.split('/'))):
        raise ExportError('contract directories must be canonical portable relative paths')
    relative = PurePosixPath(value)
    directory = root.joinpath(*relative.parts)
    if directory.resolve() != directory or not directory.is_dir():
        raise ExportError('contract directory is missing or contains a symlink')
    return directory


def claim_names(value):
    if not isinstance(value, list) or not 1 <= len(value) <= MAX_CLAIMS:
        raise ExportError(f'expected between 1 and {MAX_CLAIMS} claims')
    pairs = []
    for claim in value:
        if not isinstance(claim, dict) or set(claim) != {'theorem', 'expected'}:
            raise ExportError('claim requires exactly theorem and expected names')
        names = claim['theorem'], claim['expected']
        if not all(isinstance(name, str) for name in names):
            raise ExportError('claim names must be strings')
        pairs.append(tuple(validate_name(name) for name in names))
    if len({name for name, _ in pairs}) != len(pairs):
        raise ExportError('duplicate theorem claims are not allowed')
    return tuple(sorted(pairs))


def load_contract(path):
    path = Path(path).absolute()
    raw = read_regular(path, MAX_CONTRACT_BYTES)
    value = parse_json(raw)
    if not isinstance(value, dict) or set(value) != FIELDS or value['schema'] != SCHEMA:
        raise ExportError('unexpected proof contract schema or fields')
    if not isinstance(value['module'], str) or not isinstance(value['roots'], list):
        raise ExportError('contract module must be a name and roots must be a list')
    module = validate_name(value['module'])
    roots = tuple(select_roots(value['roots']))
    claims = claim_names(value['claims'])
    source = project_directory(path.parent, value['source_dir'])
    proofs = project_directory(path.parent, value['proof_dir'])
    if source.is_relative_to(proofs) or proofs.is_relative_to(source):
        raise ExportError('runtime and proof directories must be disjoint')
    return Contract(path, raw, source, proofs, module, roots, claims)


def snapshot(contract):
    runtime, proofs = source_snapshot(contract.source_dir), source_snapshot(contract.proof_dir)
    if contract.module not in runtime:
        raise ExportError('configured runtime module does not exist')
    if runtime.keys() & proofs.keys():
        raise ExportError('runtime and proof module names overlap')
    project = ProjectSnapshot(runtime, proofs)
    if (len(project.modules) > MAX_FILES or
            sum(len(data) for _, data in project.modules.values()) > MAX_SOURCE_BYTES):
        raise ExportError('combined proof project exceeds file or byte limit')
    return project


def verify_manifest(path, contract, project):
    path = Path(path).absolute()
    if path.name in GENERATED:
        raise ExportError('manifest must be separate from generated Go files')
    raw = read_regular(path)
    manifest = parse_json(raw)
    multi = len(contract.roots) > 1
    version, profile = ('v7', 'v5') if multi else ('v6', 'v4')
    if not isinstance(manifest, dict) or manifest.get('schema') != 'lean-go-manifest/' + version:
        raise ExportError('generated manifest schema does not match root count')
    program = manifest.get('program')
    selection, forbidden = ('roots', 'root') if multi else ('root', 'roots')
    expected = dict(schema='lean-go-ir/' + version, profile='pure-module/' + profile,
                    stage='pure-mono', module=contract.module,
                    lean_version='4.35.0', lean_commit=COMMIT)
    expected[selection] = list(contract.roots) if multi else contract.roots[0]
    if (not isinstance(program, dict) or forbidden in program or
            any(program.get(key) != value for key, value in expected.items())):
        raise ExportError('generated program, roots or Lean pin differ from contract')
    inputs = program.get('inputs')
    runtime = {'source/' + relative: digest(data) for relative, data in project.runtime.values()}
    if (not isinstance(inputs, dict) or
            {name: value for name, value in inputs.items() if name.startswith('source/')} != runtime):
        raise ExportError('runtime source inventory or hashes differ from generated manifest')
    files = manifest.get('generated_files')
    if not isinstance(files, dict) or set(files) != GENERATED:
        raise ExportError('generated file inventory differs')
    if {entry.name for entry in path.parent.iterdir()} != GENERATED | {path.name}:
        raise ExportError('generated directory inventory differs')
    for name in sorted(GENERATED):
        if files[name] != digest(read_regular(path.parent / name)):
            raise ExportError(f'generated file hash differs: {name}')
    return raw
