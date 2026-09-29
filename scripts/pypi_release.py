#!/usr/bin/env python3
"""Check GitHub Release wheels before publishing their exact bytes to PyPI."""

import argparse
import base64
import csv
import hashlib
import io
import json
from email.parser import BytesParser
from email.policy import default
from pathlib import Path
import re
import sys
import zipfile

import draft_release
import wheels


MAX_WHEEL_BYTES = 128 * 1024 * 1024
MAX_MANIFEST_BYTES = 1024 * 1024
MAX_CANDIDATE_BYTES = 8 * 1024 * 1024


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def expected_wheels(version):
    package_version = wheels.python_version(version)
    return {
        f'dircue-{package_version}-py3-none-{tag}.whl': (os_name, arch, tag)
        for (os_name, arch), tags in wheels.PLATFORMS.items()
        for tag in tags
    }


def regular_file(path, limit):
    require(path.is_file() and not path.is_symlink(), f'{path.name} is not a regular file')
    require(0 < path.stat().st_size <= limit, f'{path.name} has an invalid size')
    return path.read_bytes()


def verify_record(entries, record):
    rows = list(csv.reader(io.StringIO(entries[record].decode('utf-8'))))
    require(len(rows) == len(entries), 'wheel RECORD entry count differs')
    seen = set()
    for row in rows:
        require(len(row) == 3 and row[0] in entries and row[0] not in seen,
                'wheel RECORD has an invalid or duplicate entry')
        seen.add(row[0])
        name, digest, size = row
        if name == record:
            require((digest, size) == ('', ''), 'wheel RECORD self-entry is invalid')
        else:
            expected = base64.urlsafe_b64encode(hashlib.sha256(entries[name]).digest()).rstrip(b'=').decode()
            require(digest == 'sha256=' + expected and size == str(len(entries[name])),
                    f'wheel RECORD mismatch: {name}')
    require(seen == set(entries), 'wheel RECORD omits a file')


def verify_wheel(path, version, commit, target, core_sha, sums):
    os_name, arch, tag = target
    info = f'dircue-{wheels.python_version(version)}.dist-info'
    binary_name = 'dircue.exe' if os_name == 'windows' else 'dircue'
    binary_path = 'dircue/bin/' + binary_name
    expected_entries = {
        'dircue/__init__.py', 'dircue/__main__.py', binary_path,
        *(info + '/' + name for name in (
            'METADATA', 'WHEEL', 'RECORD', 'bundled-binary.json', 'entry_points.txt',
            'licenses/LICENSE', 'licenses/THIRD_PARTY_NOTICES.md', 'release-provenance.json')),
    }
    entries = draft_release.read_archive(path)
    require(set(entries) == expected_entries, f'{path.name} has an unexpected wheel payload')
    verify_record(entries, info + '/RECORD')

    metadata = BytesParser(policy=default).parsebytes(entries[info + '/METADATA'])
    require(metadata['Name'] == 'dircue' and metadata['Version'] == wheels.python_version(version),
            f'{path.name} package identity differs')
    require(metadata['Requires-Python'] == '>=3.10' and metadata['License-Expression'] == 'MIT',
            f'{path.name} Python or license metadata differs')
    require(entries[info + '/WHEEL'].decode('utf-8').splitlines()[-1] == 'Tag: py3-none-' + tag,
            f'{path.name} platform tag differs')
    require(entries[info + '/entry_points.txt'] ==
            b'[console_scripts]\ndircue = dircue:main\ndirq = dircue:main\n',
            f'{path.name} console scripts differ')
    require(entries[info + '/licenses/LICENSE'] and
            entries[info + '/licenses/THIRD_PARTY_NOTICES.md'],
            f'{path.name} license payload is missing')
    require(sha256(entries[binary_path]) == core_sha,
            f'{path.name} bundled executable differs from release candidate')
    wheels.validate_binary(entries[binary_path], (os_name, arch))

    row = json.loads(entries[info + '/bundled-binary.json'])
    require((row.get('os'), row.get('arch'), row.get('binary_sha256')) ==
            (os_name, arch, core_sha), f'{path.name} embedded binary identity differs')
    require(row.get('sha256') == sums.get(row.get('name')),
            f'{path.name} source archive checksum differs')
    provenance = json.loads(entries[info + '/release-provenance.json'])
    require(provenance.get('version') == version and provenance.get('git_revision') == commit,
            f'{path.name} embedded source identity differs')
    require(row in provenance.get('archives', []),
            f'{path.name} embedded archive identity differs')


def verify(directory, version, commit):
    require(re.fullmatch(r'[0-9a-f]{40}', commit) is not None, 'commit must be a full Git SHA-1')
    names = expected_wheels(version)
    require(len(names) == 7, 'unexpected wheel platform inventory')
    extras = {'SHA256SUMS', 'SHA256SUMS.sigstore.json', 'release-candidate.json'}
    require({entry.name for entry in directory.iterdir()} == set(names) | extras,
            'publication directory must contain exactly seven wheels and three release records')

    sums = draft_release.checksums(regular_file(directory / 'SHA256SUMS', MAX_MANIFEST_BYTES))
    regular_file(directory / 'SHA256SUMS.sigstore.json', MAX_MANIFEST_BYTES)
    candidate_bytes = regular_file(directory / 'release-candidate.json', MAX_CANDIDATE_BYTES)
    require(sums.get('release-candidate.json') == sha256(candidate_bytes),
            'release candidate checksum differs')
    candidate = json.loads(candidate_bytes)
    require(candidate.get('version') == version and candidate.get('commit') == commit,
            'release candidate version or commit differs')

    platform_rows = candidate.get('platforms')
    require(isinstance(platform_rows, list), 'release candidate has no platform inventory')
    platforms = {}
    for row in platform_rows:
        require(isinstance(row, dict) and isinstance(row.get('platform'), str),
                'invalid release candidate platform')
        require(row['platform'] not in platforms, 'duplicate release candidate platform')
        platforms[row['platform']] = row
    expected_platforms = {f'{os_name}-{arch}' for os_name, arch in wheels.PLATFORMS}
    require(set(platforms) == expected_platforms, 'release candidate platform inventory differs')
    require(sum(row.get('wheels', 0) for row in platform_rows) == 7,
            'release candidate wheel count differs')

    result = {}
    for name, target in sorted(names.items()):
        os_name, arch, _ = target
        platform_row = platforms[f'{os_name}-{arch}']
        require(platform_row.get('wheels') == len(wheels.PLATFORMS[(os_name, arch)]),
                'release candidate platform wheel count differs')
        core_sha = platform_row.get('core_sha256')
        require(isinstance(core_sha, str) and re.fullmatch(r'[0-9a-f]{64}', core_sha),
                'release candidate core checksum is invalid')
        payload = regular_file(directory / name, MAX_WHEEL_BYTES)
        require(sha256(payload) == sums.get(name), f'{name} checksum differs')
        verify_wheel(directory / name, version, commit, target, core_sha, sums)
        result[name] = sums[name]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    try:
        result = verify(args.directory, args.version, args.commit)
    except (ValueError, KeyError, OSError, TypeError, UnicodeError, zipfile.BadZipFile) as error:
        parser.error(str(error))
    print(json.dumps({'version': args.version, 'commit': args.commit, 'wheels': result},
                     sort_keys=True, indent=2))


if __name__ == '__main__':
    main()
