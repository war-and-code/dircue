#!/usr/bin/env python3
"""Validate and assemble release candidates; only `upload` creates a GitHub draft."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile

import release
import function_release_smoke
import declarations_release_smoke
import formats_release_smoke
import hotspots_release_smoke
import targeted_release_smoke
import context_release_smoke
import structural_worker_release as worker_release
import wheels
import wheel_release_smoke

ROOT = Path(__file__).resolve().parents[1]
PLATFORMS = tuple(worker_release.TARGETS)
MAX_ARCHIVE_BYTES = 1024 * 1024 * 1024


def digest(data):
    return hashlib.sha256(data).hexdigest()


def checked(condition, message):
    if not condition:
        raise ValueError(message)


def safe_name(name):
    parts = PurePosixPath(name).parts
    checked(bool(parts) and not name.startswith('/') and '\\' not in name and ':' not in name and
            all(part not in ('.', '..', '.git') for part in parts) and
            str(PurePosixPath(name)) == name and not any(ord(c) < 32 for c in name), 'unsafe archive path')
    return name


def read_archive(path):
    """Read bounded regular entries without extracting paths from an archive."""
    checked(path.is_file() and not path.is_symlink(), 'archive must be a regular file')
    checked(path.stat().st_size <= MAX_ARCHIVE_BYTES, 'compressed archive exceeds bound')
    entries, total = {}, 0

    def add(name, size, read):
        nonlocal total
        safe_name(name)
        checked(name not in entries, 'duplicate archive path')
        total += size
        checked(size >= 0 and total <= MAX_ARCHIVE_BYTES and len(entries) < 10000, 'archive contents exceed bounds')
        value = read()
        checked(len(value) == size, 'truncated archive entry')
        entries[name] = value

    if path.suffix == '.zip' or path.suffix == '.whl':
        with zipfile.ZipFile(path) as archive:
            for row in archive.infolist():
                checked(stat.S_IFMT(row.external_attr >> 16) in (0, stat.S_IFREG) and not row.is_dir(), 'nonregular archive entry')
                add(row.filename, row.file_size, lambda: archive.read(row))
    else:
        with tarfile.open(path, 'r:gz') as archive:
            for row in archive:
                checked(row.isfile(), 'nonregular archive entry')
                add(row.name, row.size, lambda: archive.extractfile(row).read())
    return entries


def checksums(raw):
    result = {}
    for line in raw.decode('utf-8').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        checked(match is not None, 'invalid checksum line')
        name = safe_name(match[2])
        checked(name not in result, 'duplicate checksum path')
        result[name] = match[1]
    return result


def validate_source(root, version, commit, notes):
    wheels.python_version(version)
    checked(re.fullmatch(r'[0-9a-f]{40}', commit) is not None, 'commit must be a full lowercase Git SHA-1')
    release.clean_revision(root, commit)
    tag_ref = 'refs/tags/v' + version
    tag_type = release.git(root, 'cat-file', '-t', tag_ref).decode().strip()
    checked(tag_type == 'tag', 'version tag must be annotated')
    tagged = release.git(root, 'rev-parse', '--verify', tag_ref + '^{commit}').decode().strip()
    checked(tagged == commit, 'existing version tag must point to the exact requested commit')
    safe_name(notes)
    checked(notes.endswith('.md'), 'release notes must be a committed Markdown file')
    data = release.git(root, 'show', commit + ':' + notes)
    checked(0 < len(data) <= 1024 * 1024 and b'\0' not in data, 'invalid or oversized release notes')
    data.decode('utf-8')
    return data


def github_context(commit):
    checked(os.environ.get('GITHUB_SERVER_URL') == 'https://github.com' and
            os.environ.get('GITHUB_API_URL') == 'https://api.github.com', 'only GitHub.com Actions hosts are supported')
    checked(os.environ.get('GH_HOST', 'github.com') == 'github.com', 'GH_HOST must identify github.com')
    checked(os.environ.get('GITHUB_EVENT_NAME') == 'workflow_dispatch', 'only manual dispatch may contact the release API')
    checked(os.environ.get('GITHUB_REF') == 'refs/heads/main', 'dispatch must use main')
    checked(os.environ.get('GITHUB_SHA') == commit, 'commit must equal the dispatched main revision')
    repo = os.environ.get('GITHUB_REPOSITORY', '')
    checked(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo) is not None, 'invalid repository identity')
    checked(bool(os.environ.get('GH_TOKEN')), 'GH_TOKEN is required')
    return repo


def api(path):
    request = urllib.request.Request('https://api.github.com/' + path, headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'], 'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28'})
    with urllib.request.urlopen(request, timeout=30) as response:
        data = response.read(8 * 1024 * 1024 + 1)
    checked(len(data) <= 8 * 1024 * 1024, 'GitHub response exceeds bound')
    return json.loads(data)


def ensure_remote_unused(repo, version, commit):
    # Listing includes authenticated drafts; absence from published tag lookup alone is insufficient.
    for page in range(1, 21):
        rows = api(f'repos/{repo}/releases?per_page=100&page={page}')
        checked(isinstance(rows, list), 'unexpected GitHub release response')
        checked(not any(row.get('tag_name') == 'v' + version for row in rows), 'version already has a draft or published release; refusing to overwrite it')
        if len(rows) < 100:
            break
    else:
        raise ValueError('release inventory exceeds bounded lookup; cannot establish unused version')
    obj = api(f'repos/{repo}/git/ref/tags/v{version}')['object']
    checked(obj.get('type') == 'tag', 'remote version tag must be annotated')
    for _ in range(8):
        if obj['type'] == 'commit':
            checked(obj['sha'] == commit, 'remote tag no longer matches requested commit')
            return
        checked(obj['type'] == 'tag' and re.fullmatch(r'[0-9a-f]{40}', obj['sha']), 'unexpected remote tag object')
        obj = api(f'repos/{repo}/git/tags/{obj["sha"]}')['object']
    raise ValueError('annotated tag nesting exceeds bound')


def verify_worker(path, platform, version, commit):
    payload = read_archive(path)
    sums = checksums(payload['SHA256SUMS'])
    checked(set(sums) == set(payload) - {'SHA256SUMS'}, 'worker checksum inventory mismatch')
    checked(all(digest(payload[name]) == value for name, value in sums.items()), 'worker payload checksum mismatch')
    prov = json.loads(payload['provenance.json'])
    checked(prov['version'] == version and prov['commit'] == commit and prov['source_dirty'] is False, 'worker release identity mismatch')
    checked(prov['platform'] == platform and prov['target'] == worker_release.TARGETS[platform] and
            prov['rust_toolchain'] == worker_release.TOOLCHAIN, 'worker toolchain/target mismatch')
    checked(prov['source_sha256'] == worker_release.source_hashes(), 'worker source input hashes differ from checkout')
    for name, expected in prov['source_sha256'].items():
        prefix = 'prototypes/structural/worker/'
        staged = 'source/' + name[len(prefix):] if name.startswith(prefix) else {'LICENSE': 'LICENSE', 'docs/STRUCTURE.md': 'README.md'}.get(name)
        if staged:
            checked(digest(payload[staged]) == expected, 'staged worker source mismatch')
    lock = worker_release.tomllib.loads(payload['source/Cargo.lock'].decode())
    registry = {(p['name'], p['version']): p['checksum'] for p in lock['package'] if p.get('source', '').startswith('registry+')}
    seen = set()
    for dependency in prov['dependencies']:
        key = dependency['name'], dependency['version']
        checked(key not in seen and key in registry, 'duplicate or unknown worker dependency')
        seen.add(key)
        checked(dependency['license'] and digest(payload[dependency['source_archive']]) == dependency['sha256'] == registry[key], 'worker dependency source/license mismatch')
    checked(seen == set(registry), 'worker dependency sources missing')
    checked(payload['THIRD_PARTY_NOTICES.txt'] and payload['LICENSE'], 'worker notices missing')
    binary = 'dircue-structural-worker' + ('.exe' if platform.startswith('windows') else '')
    checked(digest(payload[binary]) == prov['binary_sha256'], 'worker executable checksum mismatch')
    return prov, payload


def native_smoke(directory, platform, version, commit):
    provenance, _, _, loaded = wheels.load_release(directory / 'core')
    checked(provenance['git_revision'] == commit and provenance['version'] == version and len(loaded) == 1, 'native core identity mismatch')
    row, payload, _ = loaded[0]
    checked(row['os'] + '-' + row['arch'] == platform, 'native core target mismatch')
    suffix = '.zip' if platform.startswith('windows') else '.tar.gz'
    worker, worker_payload = verify_worker(directory / 'worker' / f'dircue-structural-worker_{version}_{platform}{suffix}', platform, version, commit)
    with tempfile.TemporaryDirectory(prefix='dircue-native-release-') as temp:
        folder = Path(temp)
        executable = 'dircue.exe' if platform.startswith('windows') else 'dircue'
        native_worker = 'dircue-structural-worker' + ('.exe' if platform.startswith('windows') else '')
        for name, data in ((executable, payload[executable]), (native_worker, worker_payload[native_worker])):
            (folder / name).write_bytes(data)
            (folder / name).chmod(0o755)
        result = subprocess.run([str(folder / executable), '--version'], capture_output=True, check=True)
        checked(result.stdout == f'dircue {version}\n'.encode() and not result.stderr, 'packaged core version mismatch')
        subprocess.run([sys.executable, str(ROOT / 'tests/structural_breadth/run.py'), '--candidate', str(folder / executable),
                        '--worker', str(folder / native_worker), '--output', str(directory / 'breadth.json')], check=True)
        if function_release_smoke.functions_required(version):
            receipt = function_release_smoke.run(folder / executable, folder / native_worker, version)
            (directory / 'functions.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
        if declarations_release_smoke.declarations_required(version):
            receipt = declarations_release_smoke.run(folder / executable, version)
            (directory / 'declarations.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
        if formats_release_smoke.required(version):
            receipt = formats_release_smoke.run(folder / executable, version)
            (directory / 'formats.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
            receipt = hotspots_release_smoke.run(folder / executable, folder / native_worker, version)
            (directory / 'hotspots.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
        if targeted_release_smoke.required(version):
            receipt = targeted_release_smoke.run(folder / executable, version)
            (directory / 'targeted.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
        if context_release_smoke.required(version):
            receipt = context_release_smoke.run(folder / executable, version, platform)
            (directory / 'context.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')


def assemble(input_dir, output, version, commit, notes):
    release.require_fresh(output)
    checked({p.name for p in input_dir.iterdir()} == {'candidate-' + p for p in PLATFORMS}, 'need exactly five native platform artifacts')
    assets, validations = {}, []
    def add(name, data):
        safe_name(name)
        checked('/' not in name and name not in assets, 'duplicate release asset name')
        assets[name] = data
    for platform in PLATFORMS:
        folder = input_dir / ('candidate-' + platform)
        prov, raw, _, loaded = wheels.load_release(folder / 'core')
        checked(prov['version'] == version and prov['git_revision'] == commit and len(loaded) == 1, 'core source/version mismatch')
        checked(prov['git_tree'] == release.git(ROOT, 'rev-parse', commit + '^{tree}').decode().strip(), 'core tree mismatch')
        core_row, payload, _ = loaded[0]
        checked(core_row['os'] + '-' + core_row['arch'] == platform, 'core platform mismatch')
        for name in release.PAYLOAD:
            checked(payload[name] == release.git(ROOT, 'show', commit + ':' + name), 'core committed documentation/license mismatch')
        add(core_row['name'], (folder / 'core' / core_row['name']).read_bytes())
        add('core-provenance-' + platform + '.json', raw)
        suffix = '.zip' if platform.startswith('windows') else '.tar.gz'
        name = f'dircue-structural-worker_{version}_{platform}{suffix}'
        wp, _ = verify_worker(folder / 'worker' / name, platform, version, commit)
        checked(checksums((folder / 'worker' / (name + '.sha256')).read_bytes()) == {name: digest((folder / 'worker' / name).read_bytes())}, 'worker external checksum mismatch')
        add(name, (folder / 'worker' / name).read_bytes())
        # Recreate deterministic wheels from the verified core payload, including RECORD and notices.
        with tempfile.TemporaryDirectory(prefix='dircue-wheel-verify-') as temp:
            expected = Path(temp) / 'wheels'
            receipt = wheels.package(folder / 'core', expected)
            checked({p.name for p in (folder / 'wheels').iterdir()} == {p.name for p in expected.iterdir()}, 'wheel inventory mismatch')
            actual_receipt = json.loads((folder / 'wheels/wheel-provenance.json').read_bytes())
            checked({k: v for k, v in actual_receipt.items() if k != 'wheels'} ==
                    {k: v for k, v in receipt.items() if k != 'wheels'}, 'wheel provenance identity mismatch')
            actual_rows = {wheel_row['name']: wheel_row for wheel_row in actual_receipt['wheels']}
            checked(len(actual_rows) == len(actual_receipt['wheels']) == len(receipt['wheels']), 'wheel provenance inventory mismatch')
            actual_sums = checksums((folder / 'wheels/SHA256SUMS').read_bytes())
            checked(set(actual_sums) == set(actual_rows), 'wheel checksum inventory mismatch')
            for wheel_row in receipt['wheels']:
                actual = actual_rows[wheel_row['name']]
                checked({k: v for k, v in actual.items() if k != 'sha256'} ==
                        {k: v for k, v in wheel_row.items() if k != 'sha256'}, 'wheel target/source provenance mismatch')
                actual_file = folder / 'wheels' / wheel_row['name']
                checked(digest(actual_file.read_bytes()) == actual['sha256'] == actual_sums[wheel_row['name']], 'wheel archive checksum mismatch')
                # Deflate output can differ across runner zlib versions; every uncompressed
                # entry, including RECORD, must still equal the deterministic package.
                checked(read_archive(actual_file) == read_archive(expected / wheel_row['name']), 'wheel payload differs from verified core')
                add(wheel_row['name'], actual_file.read_bytes())
            checked((folder / 'wheels/release-provenance.json').read_bytes() == raw, 'wheel embedded core provenance mismatch')
            add('wheel-provenance-' + platform + '.json', (folder / 'wheels/wheel-provenance.json').read_bytes())
        smoke = json.loads((folder / 'breadth.json').read_bytes())
        checked(smoke['passed'] is True and smoke['fixture_count'] == 21 and smoke['language_count'] == 20 and
                smoke['candidate_sha256'] == core_row['binary_sha256'] and smoke['worker_sha256'] == wp['binary_sha256'], 'native smoke identity/coverage mismatch')
        checked(smoke['harness_sha256'] == digest((ROOT / 'tests/structural_breadth/run.py').read_bytes()) and
                smoke['manifest_sha256'] == digest((ROOT / 'tests/structural_breadth/fixtures.json').read_bytes()), 'smoke test source mismatch')
        add('native-smoke-' + platform + '.json', (folder / 'breadth.json').read_bytes())
        if function_release_smoke.functions_required(version):
            raw_functions = (folder / 'functions.json').read_bytes()
            function_release_smoke.validate_receipt(json.loads(raw_functions), version, core_row['binary_sha256'], wp['binary_sha256'])
            add('function-smoke-' + platform + '.json', raw_functions)
        if declarations_release_smoke.declarations_required(version):
            raw_declarations = (folder / 'declarations.json').read_bytes()
            declarations_release_smoke.validate_receipt(json.loads(raw_declarations), version, core_row['binary_sha256'])
            add('declarations-smoke-' + platform + '.json', raw_declarations)
        validation = {'platform': platform, 'core_sha256': core_row['binary_sha256'], 'worker_sha256': wp['binary_sha256'], 'wheels': len(receipt['wheels'])}
        if formats_release_smoke.required(version):
            formats = json.loads((folder / 'formats.json').read_bytes())
            hotspots = json.loads((folder / 'hotspots.json').read_bytes())
            formats_release_smoke.validate_receipt(formats, version, core_row['binary_sha256'])
            hotspots_release_smoke.validate_receipt(hotspots, version, core_row['binary_sha256'], wp['binary_sha256'])
            validation['formats'] = formats
            validation['hotspots'] = hotspots
        if targeted_release_smoke.required(version):
            raw_targeted = (folder / 'targeted.json').read_bytes()
            targeted = json.loads(raw_targeted)
            targeted_release_smoke.validate_receipt(targeted, version, core_row['binary_sha256'])
            add('targeted-smoke-' + platform + '.json', raw_targeted)
            validation['targeted'] = targeted
        if context_release_smoke.required(version):
            raw_context = (folder / 'context.json').read_bytes()
            context = json.loads(raw_context)
            context_release_smoke.validate_receipt(context, version, core_row['binary_sha256'], platform)
            add('context-smoke-' + platform + '.json', raw_context)
            validation['context'] = context
        if declarations_release_smoke.declarations_required(version):
            tag = wheels.PLATFORMS[(core_row['os'], core_row['arch'])][0]
            native_wheels = [row for row in actual_receipt['wheels'] if row['platform'] == tag]
            checked(len(native_wheels) == 1, 'native wheel proof target is missing or duplicated')
            native_wheel = native_wheels[0]
            launcher = json.loads((folder / 'wheel-launcher.json').read_bytes())
            wheel_release_smoke.validate_receipt(launcher, version, platform, core_row['binary_sha256'],
                                                native_wheel['name'], native_wheel['sha256'])
            # Preserve native installation evidence inside the existing receipt,
            # without creating another downloadable asset or altering core proof.
            validation['wheel_launcher'] = launcher
        validations.append(validation)
    checked(sum(row['wheels'] for row in validations) == 7, 'expected seven platform wheels')
    receipt = {'schema_version': '1.0.0', 'version': version, 'commit': commit, 'notes_sha256': digest(notes),
               'platforms': validations, 'workflow_sha256': digest((ROOT / '.github/workflows/release-candidate.yml').read_bytes()),
               'assembler_sha256': digest(Path(__file__).read_bytes()), 'draft_only': True,
               'assets': {name: digest(data) for name, data in sorted(assets.items())}}
    add('release-candidate.json', (json.dumps(receipt, indent=2) + '\n').encode())
    add('SHA256SUMS', ''.join(digest(data) + '  ' + name + '\n' for name, data in sorted(assets.items())).encode())
    output.mkdir(parents=True, exist_ok=False)
    for name, data in assets.items():
        (output / name).write_bytes(data)
    return receipt


def upload(output, version, commit, notes):
    repo = github_context(commit)
    ensure_remote_unused(repo, version, commit)
    receipt = json.loads((output / 'release-candidate.json').read_bytes())
    checked(receipt['version'] == version and receipt['commit'] == commit and receipt['notes_sha256'] == digest(notes) and receipt['draft_only'] is True, 'draft receipt identity mismatch')
    sums = checksums((output / 'SHA256SUMS').read_bytes())
    checked(set(sums) == {p.name for p in output.iterdir()} - {'SHA256SUMS'}, 'upload asset inventory mismatch')
    checked(all(not (output / name).is_symlink() and digest((output / name).read_bytes()) == checksum for name, checksum in sums.items()), 'upload asset checksum mismatch')
    checked({k: v for k, v in sums.items() if k != 'release-candidate.json'} == receipt['assets'], 'receipt asset inventory mismatch')
    with tempfile.TemporaryDirectory(prefix='dircue-release-notes-') as temp:
        path = Path(temp) / 'notes.md'
        path.write_bytes(notes)
        subprocess.run(['gh', 'release', 'create', 'v' + version, '--repo', repo, '--draft', '--verify-tag',
                        '--title', 'dircue ' + version, '--notes-file', str(path),
                        *[str(output / name) for name in sorted(sums)], str(output / 'SHA256SUMS')], check=True, env=dict(os.environ, GH_HOST='github.com'))
    verify_uploaded_draft(output, repo, version)


def verify_uploaded_draft(output, repo, version):
    command = ['gh', 'release', 'view', 'v' + version, '--repo', repo, '--json', 'isDraft,tagName']
    def require_draft():
        state = json.loads(subprocess.check_output(command, env=dict(os.environ, GH_HOST='github.com')))
        checked(state.get('isDraft') is True and state.get('tagName') == 'v' + version, 'release is no longer the expected draft')
    require_draft()
    expected = {file.name: digest(file.read_bytes()) for file in output.iterdir()}
    with tempfile.TemporaryDirectory(prefix='dircue-download-check-') as temp:
        folder = Path(temp)
        subprocess.run(['gh', 'release', 'download', 'v' + version, '--repo', repo, '--dir', str(folder)], check=True, env=dict(os.environ, GH_HOST='github.com'))
        checked({file.name for file in folder.iterdir()} == set(expected), 'downloaded release asset inventory mismatch')
        checked(all(file.is_file() and not file.is_symlink() and digest(file.read_bytes()) == expected[file.name]
                    for file in folder.iterdir()), 'downloaded release asset checksum mismatch')
    require_draft()
    receipt = {'version': version, 'draft': True, 'downloaded_assets_sha256': expected}
    (output.parent / 'download-verification.json').write_text(json.dumps(receipt, indent=2) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=('validate', 'smoke', 'assemble', 'upload'))
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--notes', required=True)
    parser.add_argument('--github', action='store_true', help='read-only GitHub checks; requires manual-dispatch context')
    parser.add_argument('--platform', choices=PLATFORMS)
    parser.add_argument('--input', type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    try:
        notes = validate_source(ROOT, args.version, args.commit, args.notes)
        if args.github:
            ensure_remote_unused(github_context(args.commit), args.version, args.commit)
        if args.operation == 'smoke':
            checked(args.input is not None and args.platform is not None, 'smoke needs --input and --platform')
            native_smoke(args.input, args.platform, args.version, args.commit)
        elif args.operation == 'assemble':
            checked(args.input is not None and args.output is not None, 'assemble needs --input and --output')
            assemble(args.input, args.output, args.version, args.commit, notes)
        elif args.operation == 'upload':
            checked(args.output is not None, 'upload needs --output')
            upload(args.output, args.version, args.commit, notes)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, 'draft release: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
