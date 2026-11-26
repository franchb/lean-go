from pathlib import Path
import tempfile
import unittest

import export_module as producer
import validate_module

ROOT = Path(__file__).resolve().parents[1]


class ValidationWorkspaceTests(unittest.TestCase):
    def test_validation_inputs_cover_the_library_and_leave_export_inputs_alone(self):
        library = {path.relative_to(ROOT).as_posix()
                   for path in (ROOT / 'producer/LeanGo').rglob('Validate*.lean')}
        library |= {path.relative_to(ROOT).as_posix()
                    for path in (ROOT / 'producer/LeanGo/Validate').rglob('*.lean')}
        self.assertEqual(library, {name for name in validate_module.VALIDATION_INPUTS
                                   if name.endswith('.lean')})
        # Validation never changes what an ordinary export hashes into its manifest.
        self.assertFalse(set(validate_module.VALIDATION_INPUTS) & set(producer.PRODUCER_INPUTS))

    def test_validation_harness(self):
        for roots in (['A.f'], ['A.f', 'A.g']):
            with self.subTest(roots=roots):
                lines = validate_module.harness('A', roots, 1000, 2000, False).splitlines()
                self.assertEqual(lines[:4], ['import LeanGo.Module.Export', 'import LeanGo.Validate',
                                             'import LeanGo.Validate.Command', 'import A'])
                self.assertEqual(len(lines), 5)
                self.assertEqual(lines[4], f'#lean_go_validate "ir.json" [{", ".join(roots)}] "A" '
                                           'report "validation.json" heartbeats 1000 timeout_ms 2000')
        text = validate_module.harness('A', ['A.f'], 1, 1, True, ['A.g', 'A.h']).rstrip()
        self.assertTrue(text.endswith('strict skip "A.g A.h"'))

    def test_last_attempt_names_the_obligation_in_progress(self):
        with tempfile.TemporaryDirectory() as directory:
            progress = Path(directory) / 'p'
            self.assertIsNone(validate_module.last_attempt(progress))
            progress.write_text('0 ms obligations\n1 ms proving A.f\n5 ms A.f: proved in 4 ms\n'
                                '6 ms proving A.g\n')
            self.assertEqual(validate_module.last_attempt(progress), 'A.g')
            progress.write_text('0 ms proving A.f\n5 ms A.f: failed in 4 ms\n9 ms whole-program theorems\n')
            self.assertIsNone(validate_module.last_attempt(progress))


if __name__ == '__main__':
    unittest.main()
