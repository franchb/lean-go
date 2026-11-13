"""Opt-in real compiler rejection tests: LEAN_GO_MODULES=1 uv run --locked python ..."""
import os
from pathlib import Path
import tempfile
import unittest

import export_module as producer
from test_record_admission import ELABORATION_FAILURE


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean admission builds')
class RealModuleAdmissionTests(unittest.TestCase):
    def test_rejected_runtime_boundaries_preserve_output(self):
        cases = {
            'custom_compiler_pass': ('open Lean.Compiler.LCNF\n'
                                     '@[cpass] meta def noOpPass : PassInstaller where\n'
                                     '  phase := .base\n  install passes := pure passes\n'
                                     'def run (b : Bool) : Bool := b\n', '[Cc]ustom compiler passes'),
            'late_compiler_pass': ('def run (b : Bool) : Bool := b\n'
                                  'open Lean.Compiler.LCNF\n'
                                  '@[cpass] meta def noOpPass : PassInstaller where\n'
                                  '  phase := .base\n  install passes := pure passes\n',
                                  '[Cc]ustom compiler passes'),
            'extern': ('@[extern "lean_go_untrusted"] opaque impl : Bool → Bool\n'
                       'def run (x : Bool) : Bool := impl x\n', 'External function body|extern|opaque'),
            'late_extern': ('def run (x : Bool) : Bool := x\n'
                            'attribute [extern "lean_go_untrusted"] run\n', 'extern'),
            'implemented_by': ('def impl (_ : Bool) : Bool := false\n'
                               '@[implemented_by impl] def original (_ : Bool) : Bool := true\n'
                               'def run (x : Bool) : Bool := original x\n', 'implemented_by'),
            'late_implemented_by': ('def replacement (_ : Bool) : Bool := false\n'
                                    'def run (x : Bool) : Bool := x\n'
                                    'attribute [implemented_by replacement] run\n', 'implemented_by'),
            'late_csimp': ('def run (x : Bool) : Bool := x\n'
                           'def replacement (_ : Bool) : Bool := false\n'
                           'theorem forged : run = replacement := by sorry\n'
                           'attribute [csimp] forged\n', 'rewrite|axiom|sorry'),
            'unsafe': ('unsafe def run (_ : Bool) : Bool := true\n', 'safety mismatch|unsafe'),
            'partial': ('partial def run (x : Bool) : Bool := run x\n', 'safety mismatch|unsafe|partial|recursive implementation'),
            'recursive_override': ('@[noinline] def f (_ : Bool) : Bool := true\n'
                                   'partial def f._unsafe_rec (b : Bool) : Bool :=\n'
                                   '  match b with | true => false | false => f._unsafe_rec true\n'
                                   'def run (b : Bool) : Bool := f b\n',
                                   'recursive implementation|repeated compilation'),
            'polymorphic': ('def run {α : Type} (x : α) : α := x\n', 'polymorph|type|Type|universe'),
            'division': ('def run (x : UInt32) : UInt32 := x / 3\n', 'primitive|Primitive|Unsupported|unsupported|extern source dependency|provenance'),
            'sorry_proof': ('theorem forged : False := by sorry\n'
                            'def run (_ : Bool) : Bool := False.elim forged\n',
                            'axiom|proof|Unsupported|unsupported|Missing pure|missing pure'),
            'local_csimp': ('def original (_ : Bool) : Bool := true\n'
                            'def replacement (_ : Bool) : Bool := false\n'
                            'theorem forged : original = replacement := by sorry\n'
                            'section\nattribute [local csimp] forged\n'
                            '@[noinline] def run (x : Bool) : Bool := original x\nend\n',
                            'axiom|sorry|rewrite|proof'),
            'scoped_csimp': ('def original (_ : Bool) : Bool := true\n'
                             'def replacement (_ : Bool) : Bool := false\n'
                             'theorem forged : original = replacement := by sorry\n'
                             'namespace RewriteScope\nattribute [scoped csimp] forged\nend RewriteScope\n'
                             'open scoped RewriteScope\n'
                             '@[noinline] def run (x : Bool) : Bool := original x\n',
                             'axiom|sorry|rewrite|proof'),
        }
        root = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix='lean-go-admission-') as temporary:
            directory = Path(temporary)
            for name, (body, diagnostic) in cases.items():
                with self.subTest(case=name):
                    source = directory / name
                    source.mkdir()
                    (source / 'Admission.lean').write_text('namespace Admission\n' + body + '\nend Admission\n')
                    output = directory / (name + '.json')
                    output.write_bytes(b'original output\n')
                    with self.assertRaisesRegex(producer.ExportError, diagnostic) as rejected:
                        producer.export_module(root, source, 'Admission', 'Admission.run', output)
                    self.assertNotRegex(str(rejected.exception), ELABORATION_FAILURE)
                    self.assertEqual(output.read_bytes(), b'original output\n')


if __name__ == '__main__':
    unittest.main()
