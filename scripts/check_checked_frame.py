"""Check the frame composition against the existing Wire16 and CRC-16 manifests."""
import argparse
import json
from pathlib import Path
import shutil
import sys
import tempfile

from check_release_candidate import base_environment
from export import ExportError, run
from export_module import check_python

DEMOS = Path(__file__).resolve().parents[1] / 'examples/proofs'
FRAME = DEMOS / 'checkedframe'
COMPONENTS = ('wire16', 'crc16')
CLAIMS = ('accepts_reference_crc', 'roundtrip')
OUTPUT = 'wire 010200093132333435363738395744\npayload 123456789\n'


def proof_project(target, workspace):
    # The target's runtime snapshot must exactly match its generated manifest.
    # Include the other component as a proof dependency, using its original source.
    project = workspace / ('frame-proof-' + target)
    project.mkdir()
    shutil.copytree(DEMOS / target / 'source', project / 'source')
    proof = project / 'proof'
    shutil.copytree(FRAME / 'proof', proof)
    for name in COMPONENTS:
        directories = [DEMOS / name / 'proof']
        if name != target:
            directories.append(DEMOS / name / 'source')
        for directory in directories:
            for source in directory.glob('*.lean'):
                destination = proof / source.name
                if destination.exists():
                    raise ExportError(f'duplicate composition module: {source.stem}')
                shutil.copyfile(source, destination)
    contract = json.loads((DEMOS / target / 'proof-contract.json').read_bytes())
    contract['claims'] = [{'theorem': 'CheckedFrameProofs.' + claim,
                           'expected': 'CheckedFrameContract.' + claim} for claim in CLAIMS]
    contract_path = project / 'proof-contract.json'
    contract_path.write_text(json.dumps(contract, indent=2) + '\n', encoding='utf-8', newline='\n')
    return project, contract_path


def verify(cli, manifests, workspace, env, *, write_evidence=False):
    results = {}
    for target in COMPONENTS:
        project, contract_path = proof_project(target, workspace)
        manifest = manifests / target / target / 'manifest.json'
        output = project / 'evidence.json'
        run([str(cli), 'check-proofs', '--contract', str(contract_path), '--manifest', str(manifest),
             '--output', str(output)], project, env)
        results[target] = output.read_bytes()
        data = json.loads(results[target])
        if data['lean_version'] != env.get('LEAN_GO_TOOLCHAIN', '4.36.0'):
            raise ExportError('unexpected Lean version in composition evidence')
        tracked = FRAME / 'evidence' / (target + '.json')
        if data['lean_version'] == '4.36.0':
            if not write_evidence and tracked.read_bytes() != results[target]:
                raise ExportError(f'{target}: composition evidence changed')
        elif write_evidence or data['claims'] != json.loads(tracked.read_bytes())['claims']:
            raise ExportError(f'{target}: composition claims differ from stable Lean')
    if write_evidence:
        for name, data in results.items():
            (FRAME / 'evidence' / (name + '.json')).write_bytes(data)
    print('Checked frame: round trip with arbitrary suffix and independent CRC acceptance '
          'proved against both component manifests', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cli', default='lean-go')
    parser.add_argument('--write-evidence', action='store_true')
    args = parser.parse_args()
    try:
        check_python()
        cli = shutil.which(args.cli)
        if cli is None:
            raise ExportError('install the checkout CLI before checking the frame composition')
        with tempfile.TemporaryDirectory(prefix='lean-go-frame-proof-') as tmp:
            verify(Path(cli).resolve(), DEMOS, Path(tmp).resolve(), base_environment(),
                   write_evidence=args.write_evidence)
    except (OSError, ExportError) as exc:
        sys.exit(f'checked frame failed: {exc}')
