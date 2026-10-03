"""A differential receipt must use the requested baseline, not a convenient binary."""

from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import minor


class BaselineIdentity(unittest.TestCase):
    def test_external_report_fixture_is_part_of_receipt_identity(self):
        hashes = minor.input_hashes()
        syft = "tests/packageevidence/fixtures/syft-1.52.0.json"
        self.assertEqual(hashes[syft], minor.previous.sha(minor.ROOT / syft))
        self.assertIn("scripts/declarations_release_smoke.py", hashes)

    def test_changed_binary_is_rejected_before_execution(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "binary"
            binary.write_bytes(b"different executable")
            with patch.object(minor.previous, "capture") as capture:
                with self.assertRaisesRegex(ValueError, "pinned SHA"):
                    minor.check_baseline(binary, "0" * 64, "1.0.1")
                capture.assert_not_called()

    def test_source_build_default_is_not_a_released_version(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "binary"
            binary.write_bytes(b"executable")
            with patch.object(minor.previous, "capture", return_value=(0, b"dircue 1.0.0-dev\n", b"")):
                with self.assertRaisesRegex(ValueError, "release version"):
                    minor.check_baseline(binary, minor.previous.sha(binary), "1.0.1")


if __name__ == "__main__":
    unittest.main()
