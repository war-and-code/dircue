#!/usr/bin/env python3
"""Differential checks on small, hash-pinned public parity corpora."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import urllib.request

import run as synthetic

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def write_verified(target, data, expected_sha, expected_size=None):
    if expected_size is not None and len(data) != expected_size:
        raise RuntimeError(f'{target}: expected {expected_size} bytes, got {len(data)}')
    actual_sha = sha256(data)
    if actual_sha != expected_sha:
        raise RuntimeError(f'{target}: SHA-256 {actual_sha}, expected {expected_sha}')
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)


def materialize_archive(spec, target, supplied_archive, downloads):
    used_supplied_archive = supplied_archive is not None
    archive = supplied_archive
    if archive is None:
        archive = downloads / (spec['id'] + '.tar.gz')
        print(f"Downloading {spec['id']} at {spec['revision']}", flush=True)
        urllib.request.urlretrieve(spec['archive_url'], archive)
    archive = archive.resolve()
    actual_sha = sha256(archive.read_bytes())
    if actual_sha != spec['archive_sha256']:
        raise RuntimeError(f"{archive}: SHA-256 {actual_sha}, expected {spec['archive_sha256']}")

    prefix = spec['archive_prefix']
    regular_files = 0
    skipped_nonregular = 0
    with tarfile.open(archive, 'r:gz') as bundle:
        for member in bundle:
            if member.name == prefix.rstrip('/') and member.isdir():
                continue
            if not member.name.startswith(prefix):
                raise RuntimeError(f"archive member outside expected prefix: {member.name}")
            relative_text = member.name[len(prefix):]
            if not relative_text:
                continue
            relative = Path(relative_text)
            if relative.is_absolute() or '..' in relative.parts:
                raise RuntimeError(f"unsafe archive member: {member.name}")
            if member.isdir():
                continue
            if not member.isfile():
                skipped_nonregular += 1
                continue
            extracted = bundle.extractfile(member)
            if extracted is None:
                raise RuntimeError(f"could not read archive member: {member.name}")
            output = target / relative
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_bytes(extracted.read())
            regular_files += 1

    license_spec = spec['repository_license']
    license_path = target / license_spec['path']
    if not license_path.is_file() or sha256(license_path.read_bytes()) != license_spec['sha256']:
        raise RuntimeError(f"{spec['id']}: license file hash mismatch")
    return {'archive_sha256': actual_sha, 'used_supplied_archive': used_supplied_archive,
            'regular_files': regular_files,
            'skipped_nonregular': skipped_nonregular}


def read_git_blob(git_directory, revision, file_spec):
    object_name = revision + ':' + file_spec['path']
    oid = subprocess.run(
        ['git', '--git-dir', str(git_directory), 'rev-parse', object_name],
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    if oid != file_spec['git_oid']:
        raise RuntimeError(f"{object_name}: Git object {oid}, expected {file_spec['git_oid']}")
    return subprocess.run(
        ['git', '--git-dir', str(git_directory), 'cat-file', 'blob', oid],
        check=True, capture_output=True,
    ).stdout


def materialize_blobs(spec, target, git_directory):
    if git_directory is not None:
        git_directory = git_directory.resolve()
        resolved = subprocess.run(
            ['git', '--git-dir', str(git_directory), 'rev-parse', spec['revision']],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        if resolved != spec['revision']:
            raise RuntimeError(f"kernel Git revision resolved to {resolved}, expected {spec['revision']}")

    for file_spec in spec['files']:
        if git_directory is not None:
            data = read_git_blob(git_directory, spec['revision'], file_spec)
        else:
            url = spec['raw_url_template'].format(revision=spec['revision'], path=file_spec['path'])
            print(f"Downloading {spec['id']}:{file_spec['path']}", flush=True)
            with urllib.request.urlopen(url) as response:
                data = response.read()
        write_verified(target / file_spec['path'], data, file_spec['sha256'], file_spec['bytes'])

    license_spec = spec['repository_license']
    if sha256((target / license_spec['path']).read_bytes()) != license_spec['sha256']:
        raise RuntimeError(f"{spec['id']}: license file hash mismatch")
    return {'used_local_git': git_directory is not None,
            'regular_files': len(spec['files']), 'skipped_nonregular': 0}


def initialize_fixture(directory):
    synthetic.git(directory, 'init', '-q', '--initial-branch=main')
    # The downloaded snapshot represents an upstream tracked tree; preserve
    # files even when its own .gitignore patterns match them.
    synthetic.git(directory, 'add', '--force', '--all')
    synthetic.git(directory, 'commit', '-q', '-m', 'Pinned public conformance corpus')
    return synthetic.git(directory, 'rev-parse', 'HEAD')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, default=HERE / 'public_corpus.json')
    parser.add_argument('--image', default=synthetic.IMAGE)
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--gitolite-archive', type=Path,
                        help='Reuse the pinned archive; SHA-256 is still required')
    parser.add_argument('--kernel-git', type=Path,
                        help='Read pinned blobs from a local bare repository instead of raw GitHub URLs')
    parser.add_argument('--corpus', action='append', help='Only run a named corpus (repeatable)')
    parser.add_argument('--output', type=Path, default=HERE / 'results' / 'public.json')
    args = parser.parse_args()

    manifest = json.loads(args.manifest.read_text())
    corpora = manifest['corpora']
    if args.corpus:
        selected = set(args.corpus)
        corpora = [corpus for corpus in corpora if corpus['id'] in selected]
        if {corpus['id'] for corpus in corpora} != selected:
            parser.error('unknown --corpus selection')

    with tempfile.TemporaryDirectory(prefix='dircue-public-conformance-') as temporary:
        work = Path(temporary)
        fixtures = work / 'fixtures'
        fixtures.mkdir()
        downloads = work / 'downloads'
        downloads.mkdir()
        receipts = []
        for spec in corpora:
            target = fixtures / spec['id']
            target.mkdir()
            if spec['source_kind'] == 'github_archive':
                source = materialize_archive(spec, target, args.gitolite_archive, downloads)
            elif spec['source_kind'] == 'git_blobs':
                source = materialize_blobs(spec, target, args.kernel_git)
            else:
                raise RuntimeError(f"unsupported source kind: {spec['source_kind']}")
            receipts.append({
                'id': spec['id'], 'repository_url': spec['repository_url'],
                'revision': spec['revision'],
                'repository_license': spec['repository_license'],
                'file_licenses': {
                    file_spec['path']: file_spec['spdx']
                    for file_spec in spec.get('files', [])
                },
                'source': source, 'fixture_sha256': synthetic.fixture_digest(target),
                'fixture_head': initialize_fixture(target),
            })

        binary = args.binary.resolve() if args.binary else work / 'dircue'
        if not args.binary:
            build = synthetic.command(['go', 'build', '-trimpath', '-o', binary, '.'], ROOT,
                                      dict(os.environ, GOWORK='off'))
            if build['exit_code']:
                raise RuntimeError(build)

        tasks = []
        modes = [('json-breakdown', ['--breakdown', '--json'], True),
                 ('strategies', ['--strategies'], False)]
        for spec in corpora:
            for mode, flags, is_json in modes:
                tasks.append({'id': spec['id'] + '/' + mode, 'corpus': spec['id'],
                              'mode': mode, 'flags': flags, 'json': is_json})
        task_manifest = work / 'tasks.json'
        task_manifest.write_text(json.dumps(tasks))
        ruby = '''require 'json'; require 'open3'; tasks=JSON.parse(File.read('/tasks.json')); results={}; tasks.each do |t|; root=File.join('/fixtures',t['corpus']); out,err,status=Open3.capture3('github-linguist',*t['flags'],root,chdir:root); results[t['id']]={exit_code:status.exitstatus,stdout:out,stderr:err}; end; puts JSON.generate(results)'''
        reference_run = synthetic.command([
            'docker', 'run', '--rm', '--network=none',
            '-v', str(fixtures) + ':/fixtures:ro',
            '-v', str(task_manifest) + ':/tasks.json:ro',
            args.image, 'ruby', '-e', ruby,
        ])
        if reference_run['exit_code']:
            raise RuntimeError(reference_run)
        references = json.loads(reference_run['stdout'])

        results = []
        for task in tasks:
            target = fixtures / task['corpus']
            actual = synthetic.command([binary, *task['flags'], target], cwd=target)
            reference = references[task['id']]
            passed, comparison = synthetic.compare(reference, actual, task['json'])
            if passed and reference['exit_code'] == 0 and reference['stderr'] != actual['stderr']:
                passed = False
                comparison = 'successful stderr differs'
            result = dict(task, status='PASS' if passed else 'FAIL', comparison=comparison,
                          reference=reference, actual=actual)
            results.append(result)
            print(result['status'], result['id'], flush=True)

        git_head = synthetic.command(['git', 'rev-parse', '--verify', 'HEAD'], ROOT)
        report = {
            'scope': 'Pinned public-corpus repository aggregation and strategy labels; not universal language equivalence.',
            'provenance': {
                'reference_image': args.image,
                'reference_version': '9.7.0',
                'reference_image_id': synthetic.command(
                    ['docker', 'image', 'inspect', args.image, '--format', '{{.Id}}']
                )['stdout'].strip(),
                'manifest_sha256': sha256(args.manifest.read_bytes()),
                'generator_sha256': sha256(Path(__file__).read_bytes()),
                'binary_sha256': sha256(binary.read_bytes()),
                'git_head': git_head['stdout'].strip() if git_head['exit_code'] == 0 else None,
                'go_version': synthetic.command(['go', 'version'])['stdout'].strip(),
                'corpora': receipts,
            },
            'summary': {
                'total': len(results),
                'passed': sum(result['status'] == 'PASS' for result in results),
                'failed': sum(result['status'] == 'FAIL' for result in results),
            },
            'results': results,
        }
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + '\n')
        markdown = [
            '# Pinned public parity corpus', '',
            report['scope'], '',
            '| Corpus | Revision | JSON breakdown | Strategy labels |',
            '|---|---|---:|---:|',
        ]
        by_id = {result['id']: result for result in results}
        for corpus in receipts:
            cid = corpus['id']
            markdown.append(
                f"| {cid} | `{corpus['revision']}` | {by_id[cid + '/json-breakdown']['status']} | {by_id[cid + '/strategies']['status']} |"
            )
        markdown += ['', '## Failures', '']
        markdown += [f"- {result['id']}: {result['comparison']}" for result in results if result['status'] == 'FAIL']
        args.output.with_suffix('.md').write_text('\n'.join(markdown) + '\n')
        print(json.dumps(report['summary']))
        return 1 if report['summary']['failed'] else 0


if __name__ == '__main__':
    raise SystemExit(main())
