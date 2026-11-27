"""Binding and freshness regressions for proofs spanning generated packages."""
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

import check_proofs
from composition_contract import load_composition, manifest_bindings
from export import ExportError


class CompositionTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve()
        source = Path(__file__).resolve().parents[1] / 'examples/proofs'
        self.contract = self.root / 'contract.json'
        shutil.copyfile(source / 'checkedframe-contract.json', self.contract)
        shutil.copytree(source / 'checkedframe/proof', self.root / 'checkedframe/proof')
        self.manifests = {}
        for name in ('crc16', 'wire16'):
            dest = self.root / name
            for sub in ('source', 'proof', name):
                shutil.copytree(source / name / sub, dest / sub)
            shutil.copyfile(source / name / 'proof-contract.json', dest / 'proof-contract.json')
            self.manifests[name] = dest / name / 'manifest.json'
        self.output = self.root / 'evidence.json'
        self.env = mock.patch.dict(os.environ, LEAN_GO_TOOLCHAIN='4.36.0')
        self.env.start()
        self.addCleanup(self.env.stop)
        self.prove = mock.patch.object(check_proofs, 'prove', return_value=[])
        self.check = self.prove.start()
        self.addCleanup(self.prove.stop)
        # Rebuilding the packages needs Lean; these protocol tests stand in for it.
        rebuild = mock.patch.object(check_proofs, 'rebuild_package')
        self.rebuild = rebuild.start()
        self.addCleanup(rebuild.stop)

    def run_check(self, mode='--output', output=None):
        args = ['--contract', str(self.contract), mode, str(output or self.output)]
        for name, path in self.manifests.items():
            args.extend(['--component-manifest', name, str(path)])
        check_proofs.main(args)

    def test_check_once_and_detect_stale_manifest_on_either_side(self):
        self.run_check()
        self.assertEqual(self.check.call_count, 1)
        data = json.loads(self.output.read_bytes())
        self.assertEqual(set(data['components']), set(self.manifests))
        self.manifests = dict(reversed(self.manifests.items()))
        self.run_check('--evidence')
        for path in self.manifests.values():
            original = path.read_bytes()
            path.write_bytes(original + b' ')
            with self.assertRaisesRegex(ExportError, 'stale'):
                self.run_check('--evidence')
            path.write_bytes(original)

    def test_reject_invalid_bindings_and_mismatched_sources(self):
        contract = load_composition(self.contract)
        pairs = list(self.manifests.items())
        for bad in (pairs[:1], pairs + pairs[:1], pairs + [('extra', self.output)],
                    [(name, pairs[0][1]) for name, _ in pairs]):
            with self.assertRaises(ExportError):
                manifest_bindings(contract, bad)
        self.manifests['crc16'], self.manifests['wire16'] = self.manifests['wire16'], self.manifests['crc16']
        with self.assertRaises(ExportError):
            self.run_check()
        self.check.assert_not_called()

    def test_source_collision_and_protected_outputs(self):
        for path in (self.contract, self.root / 'checkedframe/proof/output', self.manifests['wire16'],
                     self.root / 'crc16/proof-contract.json', self.root / 'crc16/source/output'):
            with self.assertRaises(ExportError):
                self.run_check(output=path)
        (self.root / 'checkedframe/proof/Crc16.lean').write_text('-- duplicate module')
        with self.assertRaisesRegex(ExportError, 'overlap'):
            self.run_check()
        self.check.assert_not_called()

    def test_change_during_proof_does_not_publish(self):
        self.output.write_bytes(b'prior evidence')
        def change(*_):
            path = self.manifests['crc16'].parent / 'api.go'
            path.write_bytes(path.read_bytes() + b' ')
            return []
        self.check.side_effect = change
        with self.assertRaisesRegex(ExportError, 'generated file hash'):
            self.run_check()
        self.assertEqual(self.output.read_bytes(), b'prior evidence')


if __name__ == '__main__':
    unittest.main()
