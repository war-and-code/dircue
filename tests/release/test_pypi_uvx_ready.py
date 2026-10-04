"""Bounded fresh uvx installation checks against the public PyPI catalogs."""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import pypi_index_ready
import pypi_release
import pypi_uvx_ready as ready


VERSION = '1.3.0'
EXPECTED = {name: 'a' * 64 for name in pypi_release.expected_wheels(VERSION)}


def catalog(simple=False, missing=0, mutation=None):
    entries = [{'filename': name,
                'hashes' if simple else 'digests': {'sha256': digest},
                'yanked': False}
               for name, digest in sorted(EXPECTED.items())[missing:]]
    if mutation == 'hash':
        entries[0]['hashes' if simple else 'digests']['sha256'] = 'b' * 64
    if mutation == 'malformed':
        entries[0] = None
    if simple:
        return {'meta': {'api-version': '1.4'}, 'files': entries}
    return {'info': {'name': 'dircue', 'version': VERSION}, 'urls': entries}


def responses(*, missing=0):
    queue = [catalog(False, missing), catalog(True, missing)]
    return lambda _url: queue.pop(0)


def success(command, stdout=None, stderr=b''):
    return subprocess.CompletedProcess(command, 0,
                                       f'dircue {VERSION}\n'.encode() if stdout is None else stdout,
                                       stderr)


class PyPIUvxReadinessTests(unittest.TestCase):
    def test_stale_catalog_then_current_catalog_runs_fresh_uvx_once(self):
        replies = [catalog(False, missing=1), catalog(False), catalog(True)]
        fetched, commands, sleeps = [], [], []

        def fetch(url):
            fetched.append(url)
            return replies.pop(0)

        def run(command, **options):
            commands.append((command, options))
            return success(command)

        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'diagnostics'
            ready.smoke(VERSION, EXPECTED, output, fetch=fetch, run=run, sleep=sleeps.append)
            self.assertCountEqual([p.name for p in output.iterdir()], ['attempt-1', 'attempt-2'])
            self.assertIn('missing', (output / 'attempt-1/result.txt').read_text())
            self.assertEqual((output / 'attempt-2/result.txt').read_text(), 'success\n')

        self.assertEqual(len(fetched), 3)
        self.assertEqual(len(commands), 1)
        command, options = commands[0]
        self.assertEqual(command, ['uvx', '--no-cache', '--no-config', '--default-index',
                                   'https://pypi.org/simple', '--from', 'dircue==1.3.0',
                                   'dircue', '--version'])
        self.assertEqual(options['timeout'], ready.UVX_TIMEOUT)
        self.assertTrue(options['capture_output'])
        self.assertEqual(sleeps, [ready.RETRY_DELAY])

    def test_permanent_catalog_unavailability_is_bounded(self):
        calls, sleeps = [], []

        def fetch(url):
            calls.append(url)
            return catalog(url.endswith('/simple/dircue/'), missing=1)

        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'diagnostics'
            with self.assertRaisesRegex(ValueError, 'within 6 attempts'):
                ready.smoke(VERSION, EXPECTED, output, fetch=fetch,
                            run=lambda *_args, **_kwargs: self.fail('uvx must not run'),
                            sleep=sleeps.append)
            self.assertEqual(len(list(output.iterdir())), ready.MAX_ATTEMPTS)
        self.assertEqual(len(calls), ready.MAX_ATTEMPTS)
        self.assertEqual(len(sleeps), ready.MAX_ATTEMPTS - 1)

    def test_transport_failure_retries_then_succeeds(self):
        calls, sleeps = [], []

        def run(command, **_options):
            calls.append(command)
            if len(calls) == 1:
                return subprocess.CompletedProcess(
                    command, 2, b'', b'error: Request failed after 3 retries: https://pypi.org/simple/dircue/')
            return success(command)

        with tempfile.TemporaryDirectory() as temporary:
            ready.smoke(VERSION, EXPECTED, Path(temporary) / 'diagnostics',
                        fetch=lambda url: catalog(url.endswith('/simple/dircue/')),
                        run=run, sleep=sleeps.append)
        self.assertEqual(len(calls), 2)
        self.assertEqual(sleeps, [ready.RETRY_DELAY])

    def test_only_exact_missing_pinned_version_is_retryable(self):
        retry = (b'error: No solution found when resolving dependencies:\n'
                 b'Because there is no version of dircue==1.3.0 and you require it.')
        with self.assertRaises(ready.RetryableUvFailure):
            ready.classify_uv_failure(VERSION, 2, b'', retry)
        for failure in (
                retry.replace(b'1.3.0', b'1.2.9'),
                retry.replace(b'1.3.0', b'1.3.0.1'),
                b'error: No solution found when resolving dependencies: incompatible dependency',
                b'error: No solution found when resolving dependencies:\n'
                b'Because there is no version of dircue==1.3.0 and you require it. wheel hash mismatch',
                b'error: Request failed after 3 retries: https://other.example/simple',
                b'dircue exited with an application error'):
            with self.subTest(failure=failure):
                self.assertIsNone(ready.classify_uv_failure(VERSION, 2, b'', failure))
        with self.assertRaises(ready.RetryableUvFailure):
            ready.classify_uv_failure(
                VERSION, 2, b'', b'error: Request failed after 3 retries: https://pypi.org/simple')
        for returncode, stdout, stderr in (
                (1, b'', retry), (126, b'', retry), (127, b'', retry), (-9, b'', retry),
                (2, b'dircue 1.2.9\n', retry)):
            with self.subTest(returncode=returncode, stdout=stdout):
                self.assertIsNone(ready.classify_uv_failure(VERSION, returncode, stdout, stderr))
        self.assertIsNone(ready.classify_uv_failure(
            VERSION, 2, b'', b'error: Failed to download https://pypi.org/simple/dircue/'))

    def test_wrong_catalog_hash_or_malformed_catalog_fails_without_retry(self):
        for simple, change in ((False, 'hash'), (True, 'hash'), (False, 'malformed')):
            calls, sleeps = [], []

            def fetch(url):
                calls.append(url)
                return catalog(url.endswith('/simple/dircue/'),
                               mutation=change if simple == url.endswith('/simple/dircue/') else None)

            with tempfile.TemporaryDirectory() as temporary:
                with self.subTest(simple=simple, mutation=change), self.assertRaises(ValueError):
                    ready.smoke(VERSION, EXPECTED, Path(temporary) / 'diagnostics',
                                fetch=fetch, run=lambda *_args, **_kwargs: self.fail('bad catalog reached uvx'),
                                sleep=sleeps.append)
            self.assertEqual(len(calls), 1 if not simple else 2)
            self.assertEqual(sleeps, [])

    def test_wrong_reported_version_unknown_executable_failure_and_timeout_are_fatal(self):
        cases = (
            lambda command: success(command, stdout=b'dircue 1.2.9\n'),
            lambda command: subprocess.CompletedProcess(command, 1, b'', b'No solution found for unrelated conflict'),
        )
        for result in cases:
            calls, sleeps = [], []

            def run(command, **_options):
                calls.append(command)
                return result(command)

            with tempfile.TemporaryDirectory() as temporary, self.subTest(result=result):
                with self.assertRaisesRegex(ValueError, 'unexpected executable version|exit 1'):
                    ready.smoke(VERSION, EXPECTED, Path(temporary) / 'diagnostics',
                                fetch=lambda url: catalog(url.endswith('/simple/dircue/')),
                                run=run, sleep=sleeps.append)
            self.assertEqual(len(calls), 1)
            self.assertEqual(sleeps, [])

        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError, 'execution state is unknown'):
                ready.smoke(VERSION, EXPECTED, Path(temporary) / 'diagnostics',
                            fetch=lambda url: catalog(url.endswith('/simple/dircue/')),
                            run=lambda *_args, **_kwargs: (_ for _ in ()).throw(
                                subprocess.TimeoutExpired('uvx', ready.UVX_TIMEOUT)),
                            sleep=lambda _: self.fail('timeouts must not retry'))

    def test_uvx_command_environment_is_pinned_and_diagnostics_are_bounded(self):
        poisoned = {'PATH': '/bin', 'UV_INDEX_URL': 'https://user:secret@private.example/simple',
                    'UV_CONFIG_FILE': '/tmp/evil.toml', 'UV_EXTRA_INDEX_URL': 'https://secret.example',
                    'PIP_INDEX_URL': 'https://private.example/simple', 'HOME': '/tmp'}
        self.assertEqual(ready.uv_environment(poisoned), {'PATH': '/bin', 'HOME': '/tmp'})
        logged = ready.redact_diagnostic(b'https://user:secret@example.test/path')
        self.assertNotIn(b'secret', logged)
        self.assertLessEqual(len(ready.redact_diagnostic(b'x' * 100000)),
                             ready.MAX_DIAGNOSTIC_BYTES + len(b'\n...[diagnostic truncated]\n'))

    def test_output_directory_must_be_fresh(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'existing'
            output.mkdir()
            with self.assertRaisesRegex(ValueError, 'must be fresh'):
                ready.smoke(VERSION, EXPECTED, output)

    def test_launch_error_keeps_diagnostics_without_exposing_error_details(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'diagnostics'
            with self.assertRaisesRegex(ValueError, 'could not be started'):
                ready.smoke(VERSION, EXPECTED, output,
                            fetch=lambda url: catalog(url.endswith('/simple/dircue/')),
                            run=lambda *_args, **_kwargs: (_ for _ in ()).throw(
                                FileNotFoundError('secret-token-in-os-message')))
            result = (output / 'attempt-1/result.txt').read_text()
            self.assertIn('FileNotFoundError', result)
            self.assertNotIn('secret-token', result)

    def test_global_monotonic_deadline_stops_retry_loop(self):
        now = [0.0]
        calls = []

        def run(command, **_options):
            calls.append(command)
            now[0] += 52
            return subprocess.CompletedProcess(
                command, 2, b'', b'error: No solution found when resolving dependencies:\n'
                b'Because there is no version of dircue==1.3.0 and you require it.')

        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'diagnostics'
            with self.assertRaisesRegex(ValueError, 'global retry deadline'):
                ready.smoke(VERSION, EXPECTED, output,
                            fetch=lambda url: catalog(url.endswith('/simple/dircue/')),
                            run=run, sleep=lambda duration: now.__setitem__(0, now[0] + duration),
                            monotonic=lambda: now[0])
            self.assertLessEqual(len(calls), ready.MAX_ATTEMPTS)
            self.assertEqual(len(calls), 5)

    def test_inventory_uses_existing_exact_seven_wheel_validation(self):
        self.assertEqual(pypi_index_ready.expected_inventory(VERSION, json.dumps(EXPECTED)), EXPECTED)
        with self.assertRaises(ValueError):
            pypi_index_ready.expected_inventory(VERSION, '{}')


if __name__ == '__main__':
    unittest.main()
