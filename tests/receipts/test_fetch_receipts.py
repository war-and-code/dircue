"""Offline tests for the authenticated fallback in scripts/fetch_receipts.py."""
import importlib.util
import io
import json
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("fetch_receipts", ROOT / "scripts" / "fetch_receipts.py")
fetch_receipts = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fetch_receipts)

URL = "https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/a__b.json"


class FakeResponse(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False


class FetchFallbackTests(unittest.TestCase):
    def setUp(self):
        fetch_receipts._token_cache.clear()
        fetch_receipts._asset_cache.clear()

    def test_public_url_is_used_when_it_works(self):
        with mock.patch.object(fetch_receipts.urllib.request, "urlopen", return_value=FakeResponse(b"data")) as urlopen:
            self.assertEqual(fetch_receipts.fetch_bytes(URL), b"data")
        self.assertEqual(urlopen.call_count, 1)

    def test_refused_public_url_retries_through_the_api_without_leaking_the_token(self):
        calls = []

        def urlopen(req, timeout=None):
            calls.append(req)
            if len(calls) == 1:
                raise urllib.error.HTTPError(URL, 404, "Not Found", {}, None)
            if len(calls) == 2:
                return FakeResponse(json.dumps({"assets": [{"name": "a__b.json", "url": "https://api.github.com/assets/7"}]}).encode())
            return FakeResponse(b"private")

        with mock.patch.dict(fetch_receipts.os.environ, {"GH_TOKEN": "t0ken"}), \
                mock.patch.object(fetch_receipts.urllib.request, "urlopen", side_effect=urlopen):
            self.assertEqual(fetch_receipts.fetch_bytes(URL), b"private")
        asset_request = calls[2]
        self.assertEqual(asset_request.full_url, "https://api.github.com/assets/7")
        self.assertEqual(asset_request.get_header("Accept"), "application/octet-stream")
        # The token is sent to the API only, never to the redirected storage URL.
        self.assertNotIn("Authorization", asset_request.headers)
        self.assertEqual(asset_request.unredirected_hdrs.get("Authorization"), "Bearer t0ken")

    def test_without_a_token_the_original_error_is_raised(self):
        error = urllib.error.HTTPError(URL, 404, "Not Found", {}, None)
        with mock.patch.dict(fetch_receipts.os.environ, {"GH_TOKEN": "", "GITHUB_TOKEN": ""}), \
                mock.patch.object(fetch_receipts.subprocess, "run", side_effect=OSError("no gh")), \
                mock.patch.object(fetch_receipts.urllib.request, "urlopen", side_effect=error):
            with self.assertRaises(urllib.error.HTTPError):
                fetch_receipts.fetch_bytes(URL)


if __name__ == "__main__":
    unittest.main()
