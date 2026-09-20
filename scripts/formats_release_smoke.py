#!/usr/bin/env python3
"""Exercise bounded format observations through a real packaged console command."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import wheels

ROOT = Path(__file__).resolve().parents[1]
CHECKS = {'core_version', 'default_omission', 'default_repeat_exact', 'one_eight_workers',
          'standalone_combined_parity', 'default_fields_unchanged', 'authored_format_facts',
          'prefix_is_not_complete_validation', 'misleading_extension', 'archive_header_only',
          'malformed_content_qualified', 'depth_bound', 'no_payload_disclosure', 'read_accounting',
          'file_count_limit', 'input_byte_budget', 'oracle_mutations_rejected', 'utf8_prefix_boundaries',
          'saved_report_compare_without_source'}
# Deliberately invalid containers exercise recognition without decompression.
FIXTURES = {
    'empty': b'',
    'plain.txt': b'ordinary UTF-8 text\n',
    'misleading.xml': b'{"items":[1,true,null]}\n',
    'valid.xml': b'<root><item>DIRCUE_FORMAT_PRIVATE_PAYLOAD</item></root>\n',
    'broken.xml': b'<root><item></root>',
    'multiple.xml': b'<one/><two/>',
    'doctype.xml': b'<!DOCTYPE root SYSTEM "file:///DIRCUE_FORMAT_PRIVATE_PAYLOAD"><root/>',
    'valid.json': b'{"duplicate":1,"duplicate":2,"items":[true,null]}',
    'broken.json': b'{"items":[1,}',
    'surrogate.json': b'"\\ud800"',
    'deep.json': b'[' * 65 + b'0' + b']' * 65,
    'deep.xml': b'<a>' * 65 + b'</a>' * 65,
    'header.zip': b'PK\x03\x04\xff\xff\xff\xff',
    'header.gz': b'\x1f\x8b\x08\xff\xff\xff',
    'header.pdf': b'%PDF-this-is-only-a-header',
    'binary.dat': b'\x00\xff\x80\x01',
    'dos.dll': b'MZ' + b'\x00' * 62,
    'long.xml': b'<root>' + b'x' * (128 << 10) + b'</root>',
    'long.json': b'[' + b'0,' * (48 << 10) + b'0]',
    'utf8-cut.dat': b'x' * ((64 << 10) - 2) + b'\xe2\x82\xac',
    'invalid-tail.dat': b'x' * ((64 << 10) - 1) + b'\xffz',
    'overlong-tail.dat': b'x' * ((64 << 10) - 2) + b'\xc0\xafz',
    'continuation-tail.dat': b'x' * ((64 << 10) - 1) + b'\x80z',
}
FACTS = {'files': len(FIXTURES), 'prefix_files': ['long.json', 'long.xml'],
         'not_fully_validated': ['broken.json', 'broken.xml', 'deep.json', 'deep.xml',
                                 'doctype.xml', 'multiple.xml', 'surrogate.json'],
         'invalid_text_prefixes': ['continuation-tail.dat', 'invalid-tail.dat', 'overlong-tail.dat'],
         'archive_expansion': False, 'payload_disclosed': False}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def required(version):
    wheels.python_version(version)
    return tuple(int(part) for part in version.split('-')[0].split('.')) >= (0, 6, 0)


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def source_inputs():
    return {p.relative_to(ROOT).as_posix(): sha(p.read_bytes())
            for p in (Path(__file__).resolve(), ROOT / 'scripts/wheels.py')}


def fixture_inputs():
    return {name: sha(data) for name, data in sorted(FIXTURES.items())}


def validate_receipt(receipt, version, candidate_sha256):
    require(isinstance(receipt, dict) and required(version), 'format proof requires release 0.6 or later')
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('version') == version and
            receipt.get('passed') is True, 'format proof version/status mismatch')
    require(valid_digest(candidate_sha256) and receipt.get('candidate_sha256') == candidate_sha256,
            'format proof executable identity mismatch')
    require(receipt.get('source_sha256') == source_inputs() and receipt.get('fixture_sha256') == fixture_inputs(),
            'format proof input identity mismatch')
    require(receipt.get('checks') == sorted(CHECKS) and receipt.get('observed_facts') == FACTS,
            'format proof coverage mismatch')
    digests = receipt.get('stdout_sha256')
    require(isinstance(digests, dict) and set(digests) == {'default_languages', 'default_all', 'formats', 'combined', 'self_compare'} and
            all(valid_digest(value) for value in digests.values()), 'format proof output identity missing')
    require(receipt.get('worker_required') is False and receipt.get('external_tools_required') is False and receipt.get('source_removed_before_compare') is True,
            'format proof execution scope mismatch')
    return receipt


def output(command):
    completed = subprocess.run([str(part) for part in command], capture_output=True, timeout=120)
    require(completed.returncode == 0 and not completed.stderr,
            f'format smoke command failed: exit={completed.returncode}, stderr SHA256={sha(completed.stderr)}')
    return completed.stdout


def evidence(row, format_name, basis):
    return any(value.get('format') == format_name and value.get('basis') == basis for value in row['evidence'])


def check_report(report):
    require(report['provider'] == 'dircue-formats' and report['provider_version'] == '1.0.0' and
            report['status'] == 'complete', 'format report provider/status differs')
    require(report['scope']['archive_expansion'] is False, 'format scan expanded an archive')
    observations = report['observations']
    require([row['path'] for row in observations] == sorted(FIXTURES), 'format fixture selection differs')
    rows = {row['path']: row for row in observations}
    for name, row in rows.items():
        require(row['bytes'] == len(FIXTURES[name]) and row['bytes_read'] <= (64 << 10) + 1,
                'format byte bound differs')
    coverage = report['coverage']
    require(coverage['selected_files'] == coverage['inspected_files'] == coverage['retained_observations'] == len(FIXTURES) and
            coverage['omitted_files'] == 0 and coverage['selected_bytes'] == sum(map(len, FIXTURES.values())) and
            coverage['inspected_bytes'] == sum(row['bytes_read'] for row in observations), 'format accounting differs')
    require(coverage['prefix_reads'] == 6 and coverage['complete_reads'] == len(FIXTURES) - 6,
            'format read-scope accounting differs')
    require(evidence(rows['empty'], 'empty', 'complete_validation') and
            evidence(rows['plain.txt'], 'text', 'complete_validation') and
            evidence(rows['valid.xml'], 'xml', 'complete_validation') and
            evidence(rows['valid.json'], 'json', 'complete_validation'), 'authored valid format facts differ')
    require(evidence(rows['misleading.xml'], 'xml', 'extension_hint') and
            evidence(rows['misleading.xml'], 'json', 'complete_validation') and
            not evidence(rows['misleading.xml'], 'xml', 'complete_validation'), 'extension hint was treated as content validation')
    for name in FACTS['prefix_files']:
        format_name = name.rsplit('.', 1)[1]
        require(evidence(rows[name], format_name, 'parsed_prefix') and
                not any(value['basis'] == 'complete_validation' for value in rows[name]['evidence']),
                'prefix was missing or presented as complete validation')
    for name in FACTS['not_fully_validated']:
        format_name = name.rsplit('.', 1)[1]
        require(not evidence(rows[name], format_name, 'complete_validation') and rows[name]['diagnostics'],
                'malformed or unsupported syntax was unqualified')
    for name, format_name in [('header.zip', 'zip'), ('header.gz', 'gzip'), ('header.pdf', 'pdf')]:
        require(evidence(rows[name], format_name, 'signature_match') and
                not evidence(rows[name], format_name, 'complete_validation'), 'header was treated as container validation')
    require(evidence(rows['dos.dll'], 'dos-executable', 'signature_match') and
            not evidence(rows['dos.dll'], 'pe', 'signature_match'), 'MZ alone was treated as PE signature')
    require(not evidence(rows['binary.dat'], 'text', 'complete_validation'), 'binary was validated as text')
    require(evidence(rows['utf8-cut.dat'], 'text', 'parsed_prefix'), 'valid incomplete UTF-8 boundary was rejected')
    for name in FACTS['invalid_text_prefixes']:
        require(not any(value['format'] == 'text' and value['basis'] != 'extension_hint' for value in rows[name]['evidence']),
                'intrinsically invalid terminal UTF-8 bytes were silently trimmed')
    require(b'DIRCUE_FORMAT_PRIVATE_PAYLOAD' not in json.dumps(report).encode(), 'format report disclosed inspected payload')


def check_mutations(report):
    # Deliberate incorrect observations must fail the independent authored oracle.
    def append_complete_xml(value):
        next(row for row in value['observations'] if row['path'] == 'long.xml')['evidence'].append(
            {'format': 'xml', 'basis': 'complete_validation'})
    def bless_broken_json(value):
        next(row for row in value['observations'] if row['path'] == 'broken.json')['evidence'].append(
            {'format': 'json', 'basis': 'complete_validation'})
    mutations = [append_complete_xml, bless_broken_json,
                 lambda value: value['coverage'].update(inspected_bytes=0),
                 lambda value: value['scope'].update(archive_expansion=True)]
    for mutate in mutations:
        changed = copy.deepcopy(report)
        mutate(changed)
        try:
            check_report(changed)
        except ValueError:
            continue
        raise ValueError('format oracle failed to reject a planted false observation')


def check_resource_limits(candidate):
    with tempfile.TemporaryDirectory(prefix='dircue format file cap ') as temporary:
        root = Path(temporary)
        for number in reversed(range(4097)):
            (root / f'{number:05}.txt').write_bytes(b'')
        report = json.loads(output([candidate, 'analyze', 'formats', '--source', 'directory', '--json', root]))['formats']
        require(report['status'] == 'partial' and report['coverage']['selected_files'] == 4097 and
                report['coverage']['inspected_files'] == 4096 and report['coverage']['omitted_files'] == 1 and
                report['omissions'].get('file_count_limit') == 1 and
                [row['path'] for row in report['observations']] == [f'{i:05}.txt' for i in range(4096)],
                'format file cap did not retain deterministic first paths')
    with tempfile.TemporaryDirectory(prefix='dircue format byte cap ') as temporary:
        root = Path(temporary)
        data = b'x' * ((64 << 10) + 2)
        for number in range(513):
            (root / f'{number:05}.txt').write_bytes(data)
        report = json.loads(output([candidate, 'analyze', 'formats', '--source', 'directory', '--json', root]))['formats']
        coverage = report['coverage']
        require(report['status'] == 'partial' and coverage['selected_files'] == 513 and
                coverage['inspected_files'] == 512 and coverage['omitted_files'] == 1 and
                coverage['inspected_bytes'] == 32 << 20 and report['omissions'].get('input_byte_limit') == 1 and
                all(row['bytes_read'] <= (64 << 10) + 1 for row in report['observations']),
                'format cumulative byte budget or charged lookahead differs')


def run(candidate, version):
    require(required(version), 'format smoke requires release 0.6 or later')
    candidate = candidate.resolve()
    require(output([candidate, '--version']) == f'dircue {version}\n'.encode(), 'format core version differs')
    receipt = {'schema_version': '1.0.0', 'version': version, 'candidate_sha256': sha(candidate.read_bytes()),
               'source_sha256': source_inputs(), 'fixture_sha256': fixture_inputs()}
    with tempfile.TemporaryDirectory(prefix='dircue format smoke ') as temporary:
        root = Path(temporary) / 'source'
        root.mkdir()
        for name, data in FIXTURES.items():
            (root / name).write_bytes(data)
        default_languages = output([candidate, '--source', 'directory', '--json', root])
        default_all = output([candidate, 'analyze', 'all', '--source', 'directory', '--json', root])
        require('formats' not in json.loads(default_all), 'default emitted opt-in formats')
        args = [candidate, 'analyze', 'formats', '--source', 'directory', '--json', root]
        standalone = output([*args, '--workers', '1'])
        require(standalone == output([*args, '--workers', '8']), 'format evidence depends on worker scheduling')
        module = json.loads(standalone)['formats']
        check_report(module)
        check_mutations(module)
        combined = output([candidate, 'analyze', 'all', '--source', 'directory', '--json', '--formats', root])
        enriched, baseline = json.loads(combined), json.loads(default_all)
        require(enriched['formats'] == module, 'standalone/combined format observations differ')
        del enriched['formats']
        enriched['schema_version'] = baseline['schema_version']
        require(enriched == baseline, 'format option changed existing report fields')
        require(default_languages == output([candidate, '--source', 'directory', '--json', root]) and
                default_all == output([candidate, 'analyze', 'all', '--source', 'directory', '--json', root]),
                'default path changed after format checks')
        saved = Path(temporary) / 'saved.json'
        saved.write_bytes(standalone)
        shutil.rmtree(root)
        comparison = output([candidate, 'compare', saved, saved, '--json'])
        compared = next(row for row in json.loads(comparison)['modules'] if row['name'] == 'formats')
        require(compared['status'] == 'unchanged' and compared['counts']['changed'] == 0,
                'saved format report did not self-compare after source removal')
        receipt['stdout_sha256'] = {name: sha(data) for name, data in
            [('default_languages', default_languages), ('default_all', default_all), ('formats', standalone), ('combined', combined), ('self_compare', comparison)]}
    check_resource_limits(candidate)
    receipt.update(passed=True, checks=sorted(CHECKS), observed_facts=copy.deepcopy(FACTS),
                   worker_required=False, external_tools_required=False, source_removed_before_compare=True)
    return validate_receipt(receipt, version, sha(candidate.read_bytes()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a fresh format smoke receipt path')
    receipt = run(args.candidate, args.version)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print('Packaged format smoke passed')


if __name__ == '__main__':
    main()
