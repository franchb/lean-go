"""Real pinned-compiler diagnostics, including admission/crash separation."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import export_module as producer
from module_diagnostics import DiagnosticError
import test_local_admission as local

ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean diagnostics')
class LeanDiagnosticsTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.workspace = Path(self.temporary.name)
        self.source = self.workspace / 'source'
        self.source.mkdir()
        self.output = self.workspace / 'program.json'
        self.output.write_bytes(b'previous IR')

    def reject(self, body, module='Example', root='run'):
        (self.source / (module + '.lean')).write_bytes(body.encode())
        with self.assertRaises(DiagnosticError) as caught:
            producer.export_module(ROOT, self.source, module, root, self.output)
        self.assertEqual(self.output.read_bytes(), b'previous IR')
        return caught.exception.diagnostic

    def test_source_audit_names_imported_foreign_dependency(self):
        (self.source / 'Helper.lean').write_bytes(
            'module\r\npublic section\r\n@[extern "lean_go_untrusted"] opaque foreign : UInt32 → UInt32\r\n'.encode())
        item = self.reject('module\npublic import Helper\npublic def run (x : UInt32) : UInt32 := foreign x\n')
        self.assertEqual((item['stage'], item['code'], item['declaration']),
                         ('admission', 'source_audit', 'foreign'))
        self.assertIn('extern source dependency: foreign', item['message'])
        self.assertEqual(item['location']['path'], 'Helper.lean')
        self.assertEqual(item['location']['start']['line'], 3)
        self.assertEqual(item['location']['declaration'], 'foreign')

    def test_polymorphic_signature_is_explicit_type_rejection(self):
        item = self.reject('def run {α : Type} (x : α) : α := x\n')
        self.assertEqual((item['stage'], item['code'], item['declaration']),
                         ('admission', 'unsupported_type', 'run'))
        self.assertIn('Unsupported first-order monomorphic type', item['message'])
        self.assertEqual(item['location']['start']['column'], 4)
        self.assertIsNone(item['compiler_form'])

    def test_unsupported_constructor_field_has_its_known_declaration_range(self):
        item = self.reject('inductive Choice where\n'
                           '  | number (x : UInt32)\n'
                           '  | fn (f : UInt32 → UInt32)\n'
                           'def run (x : Choice) : Choice := x\n')
        self.assertEqual((item['stage'], item['code'], item['declaration']),
                         ('admission', 'unsupported_type', 'Choice.fn'))
        self.assertEqual(item['location']['path'], 'Example.lean')
        self.assertEqual(item['location']['declaration'], 'Choice.fn')
        self.assertEqual(item['location']['start']['line'], 3)
        self.assertEqual(item['location']['start']['column'], 4)

    def test_syntax_failure_has_original_location_and_is_not_admission(self):
        item = self.reject('def run (x : UInt32) : UInt32 := )\n')
        self.assertEqual((item['stage'], item['code']), ('build', 'producer_failed'))
        self.assertIsNone(item['declaration'])
        self.assertEqual(item['location']['path'], 'Example.lean')
        self.assertEqual(item['location']['start']['line'], 1)

    def test_actual_encoder_control_and_value_rejections(self):
        probes = [
            ('encode (.unreach (.const ``UInt32 [])) [] [] "UInt32"',
             'unsupported_lcnf_control', 'LCNF.Code.unreach'),
            ('encode (.let { fvarId := ⟨`x⟩, binderName := `x, type := .const ``UInt32 [], value := .erased } (.return ⟨`x⟩)) [] [] "UInt32"',
             'unsupported_lcnf_value', 'LCNF.LetValue.erased'),
        ]
        for action, code, form in probes:
            harness = ('import LeanGo.Module.Export\nimport Example\nopen Lean LeanGo.Module Lean.Compiler.LCNF\n'
                       'run_cmd Lean.Elab.Command.liftCoreM do\n'
                       '  discard <| reportAdmission <| withDeclaration `run do\n'
                       f'    let (result, _) ← ({action}).run {{}}\n    return result\n')
            with self.subTest(form=form), patch.object(producer, 'export_harness', return_value=harness):
                item = self.reject('def run (x : UInt32) : UInt32 := x\n')
            self.assertEqual((item['stage'], item['code'], item['compiler_form']), ('admission', code, form))
            self.assertEqual(item['declaration'], 'run')

    def test_generated_helper_uses_validated_owner_and_unknown_stays_null(self):
        body = 'namespace Local\n' + local.ADD + local.SCALAR + 'end Local\n'
        for name, expected in [('Local.run._lam_0', 'Local.run'), ('Unknown.run._lam_0', None)]:
            harness = ('import LeanGo.Module.Export\nimport Local\nopen Lean LeanGo.Module\n'
                       'run_cmd Lean.Elab.Command.liftCoreM do\n'
                       f'  discard <| reportAdmission <| withDeclaration `{name} do\n'
                       '    reject "unsupported_lcnf_control" "Use first-order calls." m!"test encoder rejection"\n')
            with self.subTest(name=name), patch.object(producer, 'export_harness', return_value=harness):
                item = self.reject(body, module='Local', root='Local.run')
            self.assertEqual(item['declaration'], name)
            if expected is None:
                self.assertIsNone(item['location'])
            else:
                self.assertEqual(item['location']['kind'], 'source_owner')
                self.assertEqual(item['location']['declaration'], expected)

    def test_script_json_failure_is_one_document_and_preserves_outputs(self):
        (self.source / 'Example.lean').write_text('def run {α : Type} (x : α) : α := x\n')
        package = self.workspace / 'package'
        package.mkdir()
        (package / 'previous.go').write_text('previous package')
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/generate.py'), '--diagnostics=json',
            '--source-dir', str(self.source), '--module', 'Example', '--root', 'run',
            '--out', str(package), '--ir-output', str(self.output)], text=True, capture_output=True)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual(result.stdout, '')
        report = json.loads(result.stderr)
        self.assertEqual(report['diagnostics'][0]['code'], 'unsupported_type')
        self.assertEqual(self.output.read_bytes(), b'previous IR')
        self.assertEqual((package / 'previous.go').read_text(), 'previous package')


if __name__ == '__main__':
    unittest.main()
