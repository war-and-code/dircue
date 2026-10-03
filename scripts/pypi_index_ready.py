#!/usr/bin/env python3
"""Wait for public PyPI catalogs and a uv lock to contain every verified wheel."""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import tomllib
from urllib.error import HTTPError, URLError
from urllib.parse import unquote, urlsplit
from urllib.request import Request, urlopen

import pypi_release


MAX_RESPONSE_BYTES = 8 * 1024 * 1024
MAX_ATTEMPTS = 6
RETRY_DELAY = 5
REQUEST_TIMEOUT = 10
LOCK_TIMEOUT = 45


class NotReady(Exception):
    """An incomplete public view can be retried without publishing again."""


def expected_inventory(version, raw):
    expected = json.loads(raw)
    names = set(pypi_release.expected_wheels(version))
    if not isinstance(expected, dict) or set(expected) != names or any(
            not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{64}', value)
            for value in expected.values()):
        raise ValueError('expected inventory must contain the seven verified wheel hashes')
    return expected


def fetch_json(url):
    request = Request(url, headers={
        'Accept': 'application/vnd.pypi.simple.v1+json, application/json',
        'Cache-Control': 'no-cache',
        'User-Agent': 'dircue-release-verification',
    })
    try:
        with urlopen(request, timeout=REQUEST_TIMEOUT) as response:
            data = response.read(MAX_RESPONSE_BYTES + 1)
    except HTTPError as error:
        if error.code == 404 or error.code == 429 or 500 <= error.code <= 599:
            raise NotReady(f'public catalog returned HTTP {error.code}') from error
        raise ValueError(f'public catalog returned HTTP {error.code}') from error
    except (URLError, TimeoutError) as error:
        raise NotReady('public catalog request failed or timed out') from error
    if len(data) > MAX_RESPONSE_BYTES:
        raise ValueError('public catalog exceeded the response byte limit')
    return json.loads(data)


def validate_catalog(catalog, version, expected, simple):
    if not isinstance(catalog, dict):
        raise ValueError('public catalog is not an object')
    if simple:
        meta = catalog.get('meta')
        if not isinstance(meta, dict) or not str(meta.get('api-version', '')).startswith('1.'):
            raise ValueError('unsupported Simple API version')
        entries = catalog.get('files')
    else:
        info = catalog.get('info')
        if not isinstance(info, dict) or info.get('name') != 'dircue' or info.get('version') != version:
            raise ValueError('public catalog package identity differs')
        entries = catalog.get('urls')
    if not isinstance(entries, list):
        raise ValueError('public catalog has no file list')
    found = {}
    prefix = f'dircue-{version}-'
    for entry in entries:
        if not isinstance(entry, dict) or not isinstance(entry.get('filename'), str):
            raise ValueError('public catalog contains an invalid file entry')
        name = entry['filename']
        if not (name.startswith(prefix) or name in (f'dircue-{version}.tar.gz', f'dircue-{version}.zip')):
            if simple:
                continue  # Simple API lists all versions of the package.
            raise ValueError('version catalog contains an unexpected release file')
        if name not in expected or name in found:
            raise ValueError('public catalog has an unexpected or duplicate release file')
        hashes = entry.get('hashes' if simple else 'digests')
        if not isinstance(hashes, dict) or hashes.get('sha256') != expected[name]:
            raise ValueError(f'public wheel hash differs: {name}')
        if entry.get('yanked', False):
            raise ValueError(f'public wheel was yanked: {name}')
        found[name] = expected[name]
    missing = sorted(set(expected) - set(found))
    if missing:
        raise NotReady(f'public catalog is missing {len(missing)} verified wheel(s): ' + ', '.join(missing))


def validate_lock(lock, version, expected):
    if not isinstance(lock, dict):
        raise ValueError('lock is not an object')
    packages = lock.get('package', [])
    if not isinstance(packages, list) or any(not isinstance(package, dict) for package in packages):
        raise ValueError('lock has an invalid package list')
    matches = [package for package in packages
               if package.get('name') == 'dircue']
    if len(matches) != 1 or matches[0].get('version') != version or matches[0].get('source') != {
            'registry': 'https://pypi.org/simple'}:
        raise ValueError('lock does not bind the expected dircue version to public PyPI')
    found = {}
    wheels = matches[0].get('wheels', [])
    if not isinstance(wheels, list) or any(not isinstance(wheel, dict) or not isinstance(wheel.get('url'), str) for wheel in wheels):
        raise ValueError('lock has an invalid wheel list')
    for wheel in wheels:
        url = urlsplit(wheel.get('url', ''))
        name = unquote(url.path.rsplit('/', 1)[-1])
        if url.scheme != 'https' or url.netloc != 'files.pythonhosted.org' or name not in expected or name in found:
            raise ValueError('lock contains an unexpected wheel URL or duplicate file')
        if wheel.get('hash') != 'sha256:' + expected[name]:
            raise ValueError(f'lock wheel hash differs: {name}')
        found[name] = expected[name]
    if set(found) != set(expected):
        raise NotReady(f'lock contains {len(found)} of {len(expected)} verified wheels')


def lock_ready(version, expected, directory, fetch=fetch_json, run=subprocess.run, sleep=time.sleep):
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            validate_catalog(fetch(f'https://pypi.org/pypi/dircue/{version}/json'), version, expected, False)
            validate_catalog(fetch('https://pypi.org/simple/dircue/'), version, expected, True)
            # A previous incomplete lock must not be reused on the next attempt.
            (directory / 'uv.lock').unlink(missing_ok=True)
            result = run(['uv', 'lock', '--no-cache', '--no-config',
                          '--index-url', 'https://pypi.org/simple'], cwd=directory,
                         timeout=LOCK_TIMEOUT, check=False)
            if result.returncode:
                raise NotReady(f'uv lock failed with exit {result.returncode}')
            with (directory / 'uv.lock').open('rb') as stream:
                validate_lock(tomllib.load(stream), version, expected)
            print(f'Public catalogs and lock contain all {len(expected)} verified wheels.', flush=True)
            return
        except (NotReady, subprocess.TimeoutExpired) as error:
            print(f'PyPI readiness attempt {attempt}/{MAX_ATTEMPTS}: {error}', file=sys.stderr, flush=True)
            if attempt == MAX_ATTEMPTS:
                raise ValueError('PyPI propagation did not complete within the retry budget') from error
            sleep(RETRY_DELAY)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--directory', type=Path, required=True)
    arguments = parser.parse_args()
    try:
        expected = expected_inventory(arguments.version, os.environ.get('EXPECTED_WHEELS_JSON', ''))
        lock_ready(arguments.version, expected, arguments.directory)
    except (ValueError, OSError) as error:
        print(f'PyPI readiness failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
