"""The intentional text discrepancy must not excuse unrelated output changes."""
import unittest
import run

class TerminalOracleTests(unittest.TestCase):
    def test_only_known_path_bytes_change(self):
        ref = {'exit_code': 0, 'stdout': '100.00% Python\n  tab\tname.py\n', 'stderr': ''}
        expected = run.terminal_path_oracle(ref, 'attrs-quoted', 'text-breakdown')
        self.assertEqual(expected['stdout'], '100.00% Python\n  tab\\tname.py\n')
        self.assertTrue(run.compare(expected, expected, False)[0])
        self.assertTrue(run.terminal_oracle_matches(expected, expected)[0])
        self.assertFalse(run.compare(expected, {**expected, 'stdout': '99.00% Python\n  tab\\tname.py\n'}, False)[0])
        self.assertFalse(run.compare(expected, ref, False)[0])
        self.assertFalse(run.terminal_oracle_matches(expected, {**expected, 'stderr': 'unexpected warning\n'})[0])
        self.assertIsNone(run.terminal_path_oracle(ref, 'unknown', 'text-breakdown'))
        self.assertIsNone(run.terminal_path_oracle(ref, 'attrs-quoted', 'json-breakdown'))
        self.assertIsNone(run.terminal_path_oracle({**ref, 'stdout': ''}, 'attrs-quoted', 'text-breakdown'))

if __name__ == '__main__': unittest.main()
