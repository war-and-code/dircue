"""Harness boundaries, independent of candidate behavior."""
import json
from pathlib import Path
import tempfile
import unittest
import run
import verify


class Captures(unittest.TestCase):
    def test_binary_capture_roundtrip(self):
        value = (17, b'\xff\xfe\x00\r\n', b'error\r\n')
        self.assertEqual(value, verify.decode(run.recorded(value)))

    def test_newline_and_exit_differences_survive(self):
        baseline = (0, b'{"a":1}\n', b'')
        for candidate in ((0, b'{"a":1}\r\n', b''), (1, b'{"a":1}\n', b''), (0, b'{"a":1}\n', b'warning')):
            self.assertNotEqual(verify.decode(run.recorded(baseline)), verify.decode(run.recorded(candidate)))

    def test_changed_capture_digest_rejected(self):
        record = run.recorded((0, b'original', b''))
        record['stdout'] = 'replacement'
        with self.assertRaises(AssertionError):
            verify.decode(record)

    def test_ambiguous_capture_rejected(self):
        record = run.recorded((0, b'original', b''))
        record['stdout_base64'] = 'b3JpZ2luYWw='
        with self.assertRaises(AssertionError):
            verify.decode(record)

    def test_jointly_empty_structure_rejected(self):
        result = (0, json.dumps({'structure': {'status': 'complete', 'parse_count': 0}}).encode(), b'')
        with self.assertRaises(AssertionError):
            run.check_reference('structure-breadth-01', result, [None] * 21)

    def test_native_fixture_is_separate_from_dirty_git_copy(self):
        legacy = run.load_legacy()
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            env, _, retained = legacy.fixture(base)
            matrix, languages = run.cases(base, env, retained, Path('/explicit/worker'))
            self.assertEqual(118, len(retained))
            self.assertEqual(209, len(matrix))
            self.assertEqual(20, len({row['language'] for row in languages}))
            self.assertFalse((base / 'structural-languages/.git').exists())
            self.assertTrue((base / 'structural-repo/.git').is_dir())
            self.assertNotEqual((base / 'structural-languages/Example.java').read_bytes(),
                                (base / 'structural-repo/Example.java').read_bytes())


if __name__ == '__main__':
    unittest.main()
