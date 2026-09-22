#!/usr/bin/env python3
"""Verify the classifier's 50 KiB inference window against Linguist 9.7.0."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import tempfile

import run as conformance

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
LIMIT = 50 * 1024
EXPECTED_HASHES = {
    'prefix': '0867e253703fba40dbadf677cd50926d6ac24c51a378cad099f16029a9fe8178',
    'suffix_first_50k': 'edc2807e6874422807102595da5140ef4c068cf63c6c0d65336fc4c747f0c417',
    'full': 'baf7e08c6a0af355e285d64a0126317b445842b7dab5af2ca1f6f1ad4c6517d9',
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default=conformance.IMAGE)
    parser.add_argument('--output', type=Path, default=HERE / 'results' / 'classifier-window.json')
    args = parser.parse_args()

    prefix = (b'use strict; my $value = 1; sub value { return $value; }\n' + b' ' * LIMIT)[:LIMIT]
    suffix = b'public class Example { public static void main(String[] args) {} }\n' * 1600
    fixtures = {'prefix': prefix, 'suffix_first_50k': suffix[:LIMIT], 'full': prefix + suffix}
    actual_hashes = {name: digest(data) for name, data in fixtures.items()}
    if actual_hashes != EXPECTED_HASHES:
        raise RuntimeError(f'classifier fixture hashes changed: {actual_hashes}')

    with tempfile.TemporaryDirectory(prefix='dircue-classifier-window-') as temporary:
        work = Path(temporary)
        probe = work / 'classifier-window-probe'
        build = conformance.command(
            ['go', 'build', '-trimpath', '-o', probe, './tests/conformance/classifier_window'],
            ROOT, dict(os.environ, GOWORK='off'),
        )
        if build['exit_code']:
            raise RuntimeError(build)

        candidate = {}
        for name, data in fixtures.items():
            fixture = work / (name + '.bin')
            fixture.write_bytes(data)
            result = conformance.command([probe, fixture])
            if result['exit_code']:
                raise RuntimeError(result)
            candidate[name] = json.loads(result['stdout'])['ranking']

        ruby = '''require 'json'; require 'linguist'; limit=50*1024; prefix=("use strict; my $value = 1; sub value { return $value; }\n"+(" "*limit)).b[0...limit]; suffix=("public class Example { public static void main(String[] args) {} }\n"*1600).b; candidates=[Linguist::Language['Java'],Linguist::Language['Perl']]; names=%w[Java Perl]; ranked=->(data){blob=Linguist::Blob.new('fixture.pl',data); Linguist::Classifier.call(blob,candidates).map(&:name)}; puts JSON.generate({prefix:ranked.call(prefix),suffix_first_50k:ranked.call(suffix[0...limit]),full:ranked.call(prefix+suffix),uncapped_full:Linguist::Classifier.classify(Linguist::Samples.cache,prefix+suffix,names).map(&:first)})'''
        ruby_run = conformance.command([
            'docker', 'run', '--rm', '--network=none', args.image, 'ruby', '-e', ruby,
        ])
        if ruby_run['exit_code']:
            raise RuntimeError(ruby_run)
        reference = json.loads(ruby_run['stdout'])

        controls = {
            'reference_prefix_prefers_perl': reference['prefix'] == ['Perl', 'Java'],
            'reference_suffix_prefers_java': reference['suffix_first_50k'] == ['Java', 'Perl'],
            'reference_full_uses_prefix': reference['full'] == reference['prefix'],
            'reference_uncapped_is_sensitive': reference['uncapped_full'] == reference['suffix_first_50k'],
            'candidate_prefix_matches': candidate['prefix'] == reference['prefix'],
            'candidate_suffix_matches': candidate['suffix_first_50k'] == reference['suffix_first_50k'],
            'candidate_full_uses_prefix': candidate['full'] == reference['full'],
        }
        report = {
            'scope': 'Direct centroid ranking for candidates Java and Perl on a mutation-sensitive 50 KiB fixture.',
            'provenance': {
                'reference_image': args.image,
                'reference_image_id': conformance.command(
                    ['docker', 'image', 'inspect', args.image, '--format', '{{.Id}}']
                )['stdout'].strip(),
                'git_head': conformance.command(
                    ['git', 'rev-parse', '--verify', 'HEAD'], ROOT
                )['stdout'].strip(),
                'generator_sha256': digest(Path(__file__).read_bytes()),
                'probe_source_sha256': digest((HERE / 'classifier_window' / 'main.go').read_bytes()),
                'classifier_source_sha256': digest(
                    (ROOT / 'third_party' / 'go-enry' / 'centroid.go').read_bytes()
                ),
                'probe_sha256': digest(probe.read_bytes()),
                'fixture_sha256': actual_hashes,
                'fixture_bytes': {name: len(data) for name, data in fixtures.items()},
            },
            'reference': reference,
            'candidate': candidate,
            'controls': controls,
            'passed': all(controls.values()),
        }
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + '\n')
        args.output.with_suffix('.md').write_text(
            '# Classifier 50 KiB window\n\n' +
            ('PASS' if report['passed'] else 'FAIL') +
            ': the fixture reverses its ranking after 50 KiB, and the maintained classifier matches the pinned Linguist window.\n'
        )
        print(json.dumps({'passed': report['passed'], 'controls': controls}))
        return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
