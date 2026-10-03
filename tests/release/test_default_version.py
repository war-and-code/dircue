"""Keep local make builds on the same release line as direct Go builds."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class LocalBuildVersion(unittest.TestCase):
    def test_make_and_go_defaults_share_the_release_version(self):
        make = re.search(r'^VERSION\s*\?=\s*([^\s]+)', (ROOT / 'Makefile').read_text(), re.MULTILINE)
        go = re.search(r'const defaultVersion = "([^"]+)"', (ROOT / 'internal/cli/version.go').read_text())
        self.assertIsNotNone(make)
        self.assertIsNotNone(go)
        # A finalized source version can coexist with the development Makefile
        # suffix; the major/minor/patch release identity must still agree.
        self.assertEqual(make.group(1).removesuffix('-dev'), go.group(1).removesuffix('-dev'))


if __name__ == '__main__':
    unittest.main()
