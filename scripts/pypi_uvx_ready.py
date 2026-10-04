#!/usr/bin/env python3
"""Retry a fresh, exact-version uvx smoke while public PyPI views converge."""

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
import time

import pypi_index_ready


MAX_ATTEMPTS = 6
RETRY_DELAY = 5
UVX_TIMEOUT = 180
RETRY_DEADLINE = 5 * 60
MAX_DIAGNOSTIC_BYTES = 64 * 1024
PUBLIC_INDEX = 'https://pypi.org/simple'


class RetryableUvFailure(Exception):
    """A resolver or transport failure that is safe to retry before execution."""


def uv_environment(source=None):
    """Remove ambient package-index and tool configuration from uvx's environment."""
    source = os.environ if source is None else source
    return {key: value for key, value in source.items()
            if not key.upper().startswith(('UV_', 'PIP_'))}


def redact_diagnostic(data):
    """Redact URL userinfo before retaining subprocess output as CI diagnostics."""
    if isinstance(data, bytes):
        text = data.decode('utf-8', errors='replace')
    else:
        text = str(data)
    # The subprocess has no configured private index, but guard retained logs
    # against a future uv error that prints credentials from another source.
    text = re.sub(r'(?i)(https?://)[^/\s@]+@', r'\1<redacted>@', text)
    encoded = text.encode('utf-8', errors='replace')
    if len(encoded) > MAX_DIAGNOSTIC_BYTES:
        encoded = encoded[:MAX_DIAGNOSTIC_BYTES] + b'\n...[diagnostic truncated]\n'
    return encoded


def classify_uv_failure(version, returncode, stdout, stderr):
    """Classify only explicit uv pre-execution failures safe to retry."""
    # stdout means the command may have started the package executable. Never
    # retry it, even when stderr also contains a uv-looking diagnostic.
    if returncode != 2 or stdout:
        return
    output = stderr.decode('utf-8', errors='replace')
    lowered = output.lower()
    resolver_header = re.search(
        r'(?im)^\s*(?:×\s*)?(?:error:\s*)?no solution found when resolving dependencies\s*:',
        output)
    exact_missing = re.compile(
        r'\bno version of\s+dircue\s*==\s*' + re.escape(version) + r'(?![A-Za-z0-9_.+-])',
        re.IGNORECASE)
    if resolver_header and exact_missing.search(output) and 'hash' not in lowered:
        raise RetryableUvFailure('uv could not resolve the exact dircue version from its public index')

    # A resolver envelope or failed-download header alone can also describe a
    # bad hash. Require an explicit transport cause plus the public host.
    transport_cause = re.search(
        r'(?i)(?:request failed after\s+\d+\s+retries|dns lookup failed|name or service not known|'
        r'temporary failure in name resolution|connection refused|connection reset by peer|'
        r'network is unreachable|tls handshake failed|timed out|timeout)', output)
    uv_error_header = re.search(r'(?im)^\s*error:\s*', output)
    if uv_error_header and transport_cause and 'pypi.org' in lowered and 'hash' not in lowered:
        raise RetryableUvFailure('uv could not connect to public PyPI')


def save_attempt(directory, attempt, stdout=b'', stderr=b'', note=''):
    """Write bounded, sanitized diagnostics for one attempt."""
    folder = directory / f'attempt-{attempt}'
    folder.mkdir()
    (folder / 'stdout.txt').write_bytes(redact_diagnostic(stdout))
    (folder / 'stderr.txt').write_bytes(redact_diagnostic(stderr))
    (folder / 'result.txt').write_bytes(redact_diagnostic(note + '\n'))


def smoke(version, expected, output, fetch=pypi_index_ready.fetch_json,
          run=subprocess.run, sleep=time.sleep, monotonic=time.monotonic):
    """Check both public catalogs, then run a fresh exact-version uvx attempt."""
    if output.exists() or output.is_symlink():
        raise ValueError('diagnostics output directory must be fresh')
    output.mkdir(parents=True)
    deadline = monotonic() + RETRY_DEADLINE
    for attempt in range(1, MAX_ATTEMPTS + 1):
        stdout = stderr = b''
        # Catalog fetches have socket timeouts, not whole-response deadlines.
        # Check the cooperative budget between requests and cap uvx by the
        # remaining time. The workflow's job timeout supplies the wall cap.
        if deadline - monotonic() <= 2 * pypi_index_ready.REQUEST_TIMEOUT:
            raise ValueError(f'PyPI uvx smoke reached its {RETRY_DEADLINE}s global retry deadline')
        try:
            pypi_index_ready.validate_catalog(
                fetch(f'https://pypi.org/pypi/dircue/{version}/json'), version, expected, False)
            if deadline - monotonic() <= pypi_index_ready.REQUEST_TIMEOUT:
                raise ValueError('global retry deadline leaves no time for the second public catalog request')
            pypi_index_ready.validate_catalog(
                fetch('https://pypi.org/simple/dircue/'), version, expected, True)
        except pypi_index_ready.NotReady as error:
            note = f'public catalogs are not ready: {error}'
        except (ValueError, TypeError, OSError) as error:
            save_attempt(output, attempt, note=f'public catalog validation failed: {error}')
            raise
        else:
            command = ['uvx', '--no-cache', '--no-config', '--default-index', PUBLIC_INDEX,
                       '--from', f'dircue=={version}', 'dircue', '--version']
            remaining = deadline - monotonic()
            if remaining <= 0:
                save_attempt(output, attempt, note='global retry deadline expired before uvx execution')
                raise ValueError(f'PyPI uvx smoke reached its {RETRY_DEADLINE}s global retry deadline')
            try:
                result = run(command, env=uv_environment(), cwd=output,
                             timeout=min(UVX_TIMEOUT, remaining), capture_output=True, check=False)
            except subprocess.TimeoutExpired as error:
                # A timeout may mean a hung native executable after uvx has
                # started it, so it is intentionally never retried.
                save_attempt(output, attempt, error.stdout or b'', error.stderr or b'',
                             'uvx timed out (fatal; execution state is unknown)')
                raise ValueError(f'uvx attempt {attempt} timed out; execution state is unknown') from error
            except OSError as error:
                save_attempt(output, attempt, note=f'uvx could not be started ({type(error).__name__})')
                raise ValueError(f'uvx could not be started; see attempt-{attempt} diagnostics') from error
            stdout = result.stdout or b''
            stderr = result.stderr or b''
            if result.returncode == 0:
                expected_output = f'dircue {version}\n'.encode()
                if stdout != expected_output:
                    save_attempt(output, attempt, stdout, stderr,
                                 f'uvx exited successfully but reported an unexpected version; expected {expected_output!r}')
                    raise ValueError(f'uvx returned an unexpected executable version; see attempt-{attempt} diagnostics')
                save_attempt(output, attempt, stdout, stderr, 'success')
                print(f'Fresh public-PyPI uvx smoke passed for dircue {version}.', flush=True)
                return
            try:
                classify_uv_failure(version, result.returncode, stdout, stderr)
            except RetryableUvFailure as error:
                note = f'retryable resolver/transport failure: {error}; uv exit {result.returncode}'
            else:
                save_attempt(output, attempt, stdout, stderr,
                             f'uvx failed with exit {result.returncode}; not a recognized retryable resolver/transport failure')
                raise ValueError(f'uvx failed with exit {result.returncode}; see attempt-{attempt} diagnostics')

        save_attempt(output, attempt, stdout, stderr, note)
        print(f'PyPI uvx smoke attempt {attempt}/{MAX_ATTEMPTS}: {note}', file=sys.stderr, flush=True)
        if attempt == MAX_ATTEMPTS:
            raise ValueError(f'public PyPI did not provide a successful fresh uvx install within {MAX_ATTEMPTS} attempts')
        remaining = deadline - monotonic()
        if remaining <= 0:
            raise ValueError(f'PyPI uvx smoke reached its {RETRY_DEADLINE}s global retry deadline')
        sleep(min(RETRY_DELAY, remaining))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True,
                        help='fresh directory for bounded per-attempt diagnostics')
    args = parser.parse_args()
    try:
        expected = pypi_index_ready.expected_inventory(
            args.version, os.environ.get('EXPECTED_WHEELS_JSON', ''))
        smoke(args.version, expected, args.output.absolute())
    except (OSError, TypeError, ValueError) as error:
        print(f'PyPI uvx smoke failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
