"""Portable composition contracts binding one Lean proof project to several packages."""
from dataclasses import dataclass
from pathlib import Path

from export import ExportError, parse_json
from export_module import MAX_FILES, MAX_SOURCE_BYTES, source_snapshot, validate_name
from proof_contract import (Contract, MAX_CONTRACT_BYTES, ProjectSnapshot, claim_names, digest,
                            load_contract, project_directory, project_path, read_regular,
                            snapshot, verify_manifest)


@dataclass(frozen=True)
class Composition:
    path: Path
    raw: bytes
    proof_dir: Path
    components: dict[str, Contract]
    claims: tuple[tuple[str, str], ...]


def load_composition(path):
    path = Path(path).absolute()
    raw = read_regular(path, MAX_CONTRACT_BYTES)
    doc = parse_json(raw)
    if (not isinstance(doc, dict) or set(doc) != {'schema', 'components', 'proof_dir', 'claims'}
            or doc['schema'] != 'lean-go-proof-contract/v2'):
        raise ExportError('expected a v2 composition contract with components, proof_dir and claims')
    specs = doc['components']
    if not isinstance(specs, dict) or not 2 <= len(specs) <= 64:
        raise ExportError('a composition requires between 2 and 64 named components')
    components = {validate_name(name): load_contract(project_path(path.parent, file))
                  for name, file in sorted(specs.items())}
    if len({c.path for c in components.values()}) != len(components):
        raise ExportError('duplicate component contract')
    return Composition(path, raw, project_directory(path.parent, doc['proof_dir']), components,
                       claim_names(doc['claims']))


def manifest_bindings(contract, pairs):
    paths = {name: Path(path).absolute() for name, path in pairs}
    if len(paths) != len(pairs) or paths.keys() != contract.components.keys():
        raise ExportError('provide exactly one manifest for each named component')
    if len(set(paths.values())) != len(paths):
        raise ExportError('duplicate component manifest path')
    return paths


def composition_inputs(contract, bindings, compiler):
    combined = ProjectSnapshot({}, source_snapshot(contract.proof_dir))
    count = len(combined.proofs)
    size = sum(len(data) for _, data in combined.proofs.values())
    identities = {}
    for name, component in contract.components.items():
        project = snapshot(component)
        if project.modules.keys() & combined.modules.keys():
            raise ExportError(f'overlapping Lean module names in composition: {name}')
        count += len(project.modules)
        size += sum(len(data) for _, data in project.modules.values())
        if count > MAX_FILES or size > MAX_SOURCE_BYTES:
            raise ExportError('combined proof project exceeds file or byte limit')
        manifest = verify_manifest(bindings[name], component, project, compiler)
        identities[name] = {'contract_sha256': digest(component.raw),
                            'generated_manifest_sha256': digest(manifest),
                            'module': component.module, 'roots': list(component.roots)}
        combined.runtime.update(project.runtime)
        combined.proofs.update(project.proofs)
    return combined, identities
