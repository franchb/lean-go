"""Independent arithmetic model and opt-in complete mixed-width campaign."""
import os
from pathlib import Path
import tempfile
import unittest

import check_width_conversions as campaign


class WidthModelTests(unittest.TestCase):
    def test_boundary_oracle_and_sample_contract(self):
        self.assertEqual(campaign.model(4294967295, 4294967295, 18446744073709551615),
                         [4294967295, 4294967295, 18446744073709551615, 4294967295, 4294967295])
        self.assertEqual(campaign.model(2147483648, 1, 4294967296),
                         [2147483648, 0, 9223372036854775809, 1, 0])
        self.assertEqual(campaign.samples(), campaign.samples())
        for a, b, word in campaign.samples():
            self.assertTrue(0 <= a < 2**32 and 0 <= b < 2**32 and 0 <= word < 2**64)
        words = {word for _, _, word in campaign.samples()}
        self.assertTrue({0, 2**31, 2**32-1, 2**32, 2**32+1, 2**63, 2**64-1,
                         12297829382473034410, 6148914691236517205} <= words)

    @unittest.skipUnless(os.environ.get('LEAN_GO_DIFFERENTIAL') == '1', 'set LEAN_GO_DIFFERENTIAL=1 for native compilation')
    def test_native_raw_typed_and_observed_resource_thresholds(self):
        with tempfile.TemporaryDirectory(prefix='lean-go-widths-') as directory:
            report = campaign.check(Path(directory) / 'evidence')
            self.assertEqual(report['cases'], 206)
            self.assertEqual(report['budgets'], {'widen': [1, 2], 'narrow': [1, 2], 'low': [1, 2],
                                                'high': [3, 4], 'pack': [5, 6]})


if __name__ == '__main__':
    unittest.main()
