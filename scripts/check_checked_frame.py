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


def verify(cli, manifests, workspace, env, *, write_evidence=False):
    contract = DEMOS / 'checkedframe-contract.json'
    tracked = FRAME / 'proof-evidence.json'
    output = workspace / 'checkedframe-evidence.json'
    command = [str(cli), 'check-proofs', '--contract', str(contract)]
    for name in COMPONENTS:
        command.extend(['--component-manifest', name, str(manifests / name / name / 'manifest.json')])
    run([*command, '--output', str(output)], workspace, env)
    data = json.loads(output.read_bytes())
    if data['lean_version'] != env.get('LEAN_GO_TOOLCHAIN', '4.36.0'):
        raise ExportError('unexpected compiler in composition evidence')
    expected = [('CheckedFrameProofs.' + claim, 'CheckedFrameContract.' + claim) for claim in CLAIMS]
    if (set(data['components']) != set(COMPONENTS) or
            [(c['name'], c['expected']) for c in data['claims']] != expected):
        raise ExportError('unexpected frame claims or bound components')
    if data['lean_version'] == '4.36.0':
        if write_evidence:
            tracked.write_bytes(output.read_bytes())
        elif output.read_bytes() != tracked.read_bytes():
            raise ExportError('checksummed frame composition evidence changed')
    elif write_evidence or data['claims'] != json.loads(tracked.read_bytes())['claims']:
        raise ExportError('composition claims differ from stable Lean')
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
