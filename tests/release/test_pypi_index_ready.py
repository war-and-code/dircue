"""An incomplete PyPI view must not become the shared installation lock."""

import contextlib
from http.client import IncompleteRead
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import pypi_index_ready as ready
import pypi_release


VERSION = '1.2.0'
EXPECTED = {name: 'a' * 64 for name in pypi_release.expected_wheels(VERSION)}


def catalog(simple=False, missing=0):
    entries = [{'filename': name, 'hashes' if simple else 'digests': {'sha256': digest}, 'yanked': False}
               for name, digest in sorted(EXPECTED.items())[missing:]]
    if simple:
        return {'meta': {'api-version': '1.4'}, 'files': entries}
    return {'info': {'name': 'dircue', 'version': VERSION}, 'urls': entries}


def lock(missing=0):
    return {'package': [{'name': 'dircue', 'version': VERSION,
                         'source': {'registry': 'https://pypi.org/simple'},
                         'wheels': [{'url': 'https://files.pythonhosted.org/packages/' + name,
                                     'hash': 'sha256:' + digest}
                                    for name, digest in sorted(EXPECTED.items())[missing:]]}]}


def write_lock(directory, missing=0):
    package = lock(missing)['package'][0]
    lines = ['[[package]]', 'name = "dircue"', f'version = "{VERSION}"',
             'source = { registry = "https://pypi.org/simple" }', 'wheels = [']
    lines.extend('  { url = ' + json.dumps(w['url']) + ', hash = ' + json.dumps(w['hash']) + ' },'
                 for w in package['wheels'])
    (directory / 'uv.lock').write_text('\n'.join(lines + [']']))


class PyPIReadinessTests(unittest.TestCase):
    def test_expected_hashes_must_cover_exactly_the_release_platforms(self):
        self.assertEqual(ready.expected_inventory(VERSION, json.dumps(EXPECTED)), EXPECTED)
        for wrong in ({}, list(EXPECTED), dict(EXPECTED, extra='a' * 64),
                      {name: 'wrong' for name in EXPECTED}):
            with self.subTest(wrong=wrong), self.assertRaises(ValueError):
                ready.expected_inventory(VERSION, json.dumps(wrong))

    def test_both_public_catalogs_must_contain_all_seven_hashes(self):
        for simple in (False, True):
            ready.validate_catalog(catalog(simple), VERSION, EXPECTED, simple)
            with self.assertRaises(ready.NotReady):
                ready.validate_catalog(catalog(simple, missing=2), VERSION, EXPECTED, simple)
            for mutation in ('hash', 'duplicate', 'unexpected', 'yanked'):
                data = catalog(simple)
                entries = data['files' if simple else 'urls']
                if mutation == 'hash':
                    entries[0]['hashes' if simple else 'digests']['sha256'] = 'b' * 64
                elif mutation == 'duplicate':
                    entries.append(entries[0])
                elif mutation == 'unexpected':
                    entries.append({'filename': f'dircue-{VERSION}.tar.gz'})
                else:
                    entries[0]['yanked'] = True
                with self.subTest(simple=simple, mutation=mutation), self.assertRaises(ValueError):
                    ready.validate_catalog(data, VERSION, EXPECTED, simple)
        data = catalog(True)
        data['files'].append({'filename': 'dircue-0.1.0-py3-none-win_amd64.whl'})
        ready.validate_catalog(data, VERSION, EXPECTED, True)

    def test_wrong_or_malformed_catalog_is_fatal(self):
        for data in (None, {}, {'info': {'name': 'wrong', 'version': VERSION}, 'urls': []}):
            with self.subTest(data=data), self.assertRaises(ValueError):
                ready.validate_catalog(data, VERSION, EXPECTED, False)
        for data in ({}, {'meta': {'api-version': '2.0'}, 'files': []},
                     {'meta': {'api-version': '1.0'}, 'files': [None]}):
            with self.subTest(data=data), self.assertRaises(ValueError):
                ready.validate_catalog(data, VERSION, EXPECTED, True)

    def test_lock_inventory_hash_origin_and_version_are_checked(self):
        ready.validate_lock(lock(), VERSION, EXPECTED)
        with self.assertRaises(ready.NotReady):
            ready.validate_lock(lock(2), VERSION, EXPECTED)
        for mutation in ('hash', 'origin', 'version', 'duplicate', 'registry'):
            data = lock()
            package = data['package'][0]
            if mutation == 'hash':
                package['wheels'][0]['hash'] = 'sha256:' + 'b' * 64
            elif mutation == 'origin':
                package['wheels'][0]['url'] = package['wheels'][0]['url'].replace('files.pythonhosted.org', 'other.example')
            elif mutation == 'version':
                package['version'] = '0.0.0'
            elif mutation == 'duplicate':
                package['wheels'].append(package['wheels'][0])
            else:
                package['source']['registry'] = 'https://other.example/simple'
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                ready.validate_lock(data, VERSION, EXPECTED)

    def test_incomplete_catalog_then_incomplete_lock_then_success(self):
        responses = iter([catalog(False, 2), catalog(), catalog(True), catalog(), catalog(True)])
        sleeps, calls = [], []
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            def run(command, **options):
                self.assertFalse((directory / 'uv.lock').exists())
                self.assertEqual(command[-2:], ['--index-url', 'https://pypi.org/simple'])
                self.assertEqual(options['timeout'], ready.LOCK_TIMEOUT)
                self.assertEqual(options['cwd'], directory)
                calls.append(command)
                write_lock(directory, missing=2 if len(calls) == 1 else 0)
                return subprocess.CompletedProcess(command, 0)
            with contextlib.redirect_stderr(io.StringIO()), contextlib.redirect_stdout(io.StringIO()):
                ready.lock_ready(VERSION, EXPECTED, directory,
                                 fetch=lambda _: next(responses), run=run, sleep=sleeps.append)
        self.assertEqual(len(calls), 2)
        self.assertEqual(sleeps, [ready.RETRY_DELAY, ready.RETRY_DELAY])

    def test_permanent_incompleteness_and_transport_timeout_stop(self):
        for failure in (ready.NotReady('missing wheels'), ready.NotReady('request timed out')):
            attempts, sleeps = [], []
            def fetch(_):
                attempts.append(1)
                raise failure
            with tempfile.TemporaryDirectory() as temporary, contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaisesRegex(ValueError, 'retry budget'):
                    ready.lock_ready(VERSION, EXPECTED, Path(temporary), fetch=fetch,
                                     run=lambda *_args, **_kwargs: self.fail('must not resolve an incomplete index'),
                                     sleep=sleeps.append)
            self.assertEqual(len(attempts), ready.MAX_ATTEMPTS)
            self.assertEqual(len(sleeps), ready.MAX_ATTEMPTS - 1)

    def test_failed_resolver_is_retried_without_hiding_output(self):
        calls = []
        def run(command, **options):
            self.assertNotIn('capture_output', options)
            self.assertNotIn('stdout', options)
            calls.append(1)
            return subprocess.CompletedProcess(command, 1)
        with tempfile.TemporaryDirectory() as temporary, contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaisesRegex(ValueError, 'retry budget'):
                ready.lock_ready(VERSION, EXPECTED, Path(temporary),
                                 fetch=lambda url: catalog(url.endswith('/simple/dircue/')),
                                 run=run, sleep=lambda _: None)
        self.assertEqual(len(calls), ready.MAX_ATTEMPTS)

    def test_request_timeout_status_and_byte_bound(self):
        for error in (
                URLError('network failure'), TimeoutError(),
                ConnectionResetError('connection reset'), HTTPError('url', 404, '', {}, None)):
            with patch.object(ready, 'urlopen', side_effect=error), self.assertRaises(ready.NotReady):
                ready.fetch_json('https://pypi.org/simple/dircue/')
        with patch.object(ready, 'urlopen', side_effect=HTTPError('url', 403, '', {}, None)), self.assertRaises(ValueError):
            ready.fetch_json('https://pypi.org/simple/dircue/')
        with patch.object(ready, 'urlopen') as opening:
            opening.return_value.__enter__.return_value.read.return_value = b'x' * (ready.MAX_RESPONSE_BYTES + 1)
            with self.assertRaisesRegex(ValueError, 'byte limit'):
                ready.fetch_json('https://pypi.org/simple/dircue/')
            self.assertEqual(opening.call_args.kwargs['timeout'], ready.REQUEST_TIMEOUT)

        # A body read happens after urlopen succeeds; urllib can surface a bare
        # connection reset here rather than wrapping it in URLError.
        with patch.object(ready, 'urlopen') as opening:
            response = opening.return_value.__enter__.return_value
            for failure in (ConnectionResetError('connection reset during body'), IncompleteRead(b'{', 10)):
                response.read.side_effect = failure
                with self.subTest(failure=type(failure).__name__), self.assertRaises(ready.NotReady):
                    ready.fetch_json('https://pypi.org/simple/dircue/')

    def test_workflow_binds_readiness_to_verified_release_hashes(self):
        workflow = (ROOT / '.github/workflows/publish-pypi.yml').read_text()
        lock_job = workflow.split('  lock-smoke:', 1)[1].split('  install-smoke:', 1)[0]
        self.assertIn('needs: [verify, publish]', lock_job)
        self.assertIn('EXPECTED_WHEELS_JSON: ${{ needs.verify.outputs.wheel_hashes }}', lock_job)
        self.assertIn('python scripts/pypi_index_ready.py', lock_job)
        self.assertNotIn('uv lock --no-cache', lock_job)
        self.assertIn('wheel_hashes: ${{ steps.manifest.outputs.wheel_hashes }}', workflow)
        # Six attempts fit inside the job deadline even if all operations time out.
        upper_bound = ready.MAX_ATTEMPTS * (2 * ready.REQUEST_TIMEOUT + ready.LOCK_TIMEOUT)
        upper_bound += (ready.MAX_ATTEMPTS - 1) * ready.RETRY_DELAY
        self.assertLess(upper_bound, 8 * 60)


if __name__ == '__main__':
    unittest.main()
