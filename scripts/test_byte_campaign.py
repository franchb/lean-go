"""Independent arithmetic oracle and opt-in native byte campaign."""
import os
from pathlib import Path
import tempfile
import unittest
import check_bytes as campaign

class ByteCampaignTests(unittest.TestCase):
    def test_model_and_samples(self):
        self.assertEqual(campaign.model(255, 8, 4294967295),
                         [7, 247, 248, 255, 8, 255, 247, 0, 255, 0, 0, 0, 255, 255, 255, 1, 7, 255, 255])
        self.assertEqual(campaign.samples(), campaign.samples())
        for a, b, word in campaign.samples():
            self.assertTrue(0 <= a < 256 and 0 <= b < 256 and 0 <= word < 2**32)
        self.assertEqual(len(campaign.samples()), 353)

    @unittest.skipUnless(os.environ.get('LEAN_GO_DIFFERENTIAL') == '1', 'set LEAN_GO_DIFFERENTIAL=1 for native compilation')
    def test_native_raw_typed_and_resource_thresholds(self):
        with tempfile.TemporaryDirectory(prefix='lean-go-bytes-') as directory:
            report = campaign.check(Path(directory) / 'evidence')
            self.assertEqual(report['status'], 'passed')
            self.assertEqual(report['cases'], 353)
            self.assertEqual(report['budgets'], [17, 18])

if __name__ == '__main__':
    unittest.main()
