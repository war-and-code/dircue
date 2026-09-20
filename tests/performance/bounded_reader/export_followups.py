#!/usr/bin/env python3
"""Export allocation attribution and focused reader comparisons without local paths."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import shutil
import statistics


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--allocation-export', type=Path, required=True)
    parser.add_argument('--idle-gc-export', type=Path, required=True)
    parser.add_argument('--variant-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    root = Path(__file__).resolve().parents[3]
    replacements = {str(args.variant_dir.resolve()): '<variant-build>',
                    str(root): '<project>', str(Path.home()): '<home>'}

    def clean(value):
        if isinstance(value, dict):
            return {key: clean(item) for key, item in value.items()}
        if isinstance(value, list):
            return [clean(item) for item in value]
        if isinstance(value, str):
            for before, after in sorted(replacements.items(), key=lambda item: -len(item[0])):
                value = value.replace(before, after)
        return value

    def save(path, document):
        text = json.dumps(clean(document), indent=2, sort_keys=True) + '\n'
        if '/Users/' in text or '/private/var/' in text or str(Path.home()) in text:
            raise ValueError('local path remains in ' + str(path))
        if path.suffix == '.gz':
            path.write_bytes(gzip.compress(text.encode(), mtime=0))
        else:
            path.write_text(text)

    source_receipts = {}
    for label, source in [('original-allocation', args.allocation_export),
                          ('original-idle-gc', args.idle_gc_export)]:
        destination = args.output / label
        destination.mkdir()
        for path in sorted(source.iterdir()):
            if path.name not in ('allocation-results.json', 'build-inputs.json') and not path.name.endswith('-top.txt'):
                raise ValueError('unexpected exported artifact: ' + path.name)
            source_receipts[label + '/' + path.name] = digest(path)
            if path.suffix == '.json':
                document = json.loads(path.read_text())
                if path.name == 'allocation-results.json':
                    if not document['exact_stdout_stderr_exit_match']:
                        raise ValueError('allocation outputs do not match')
                    document['evidence_phase'] = 'Original reader, before the growth-cap correction; not the final runtime.'
                    document['build_inputs_file'] = 'build-inputs.json.gz'
                save(destination / (path.name + ('.gz' if path.name == 'build-inputs.json' else '')), document)
            else:
                text = path.read_text()
                if '/Users/' in text or '/private/var/' in text:
                    raise ValueError('local path in stack table')
                shutil.copyfile(path, destination / path.name)

    builds = json.loads((args.variant_dir / 'builds.json').read_text())
    source_receipts['focused-builds.json'] = digest(args.variant_dir / 'builds.json')
    save(args.output / 'focused-builds.json.gz', builds)
    for name in ['xml', 'roslyn-directory', 'memory-target']:
        path = args.variant_dir / (name + '-measurements') / 'results.json'
        report = json.loads(path.read_text())
        if not report['exact_output_equal']:
            raise ValueError('focused outputs do not match')
        if name == 'memory-target':
            expected = {'growth': report['binary_sha256']}
            groups = report['samples'].values()
        else:
            expected = report['binary_sha256']
            groups = [group for modes in report['samples'].values() for group in modes.values()]
        for lane, binary_hash in expected.items():
            if builds['builds'][lane]['sha256'] != binary_hash:
                raise ValueError('measured executable differs from build receipt')
        for group in groups:
            for samples in group.values():
                if len(samples) != 20 or any(sample['exit_code'] != 0 for sample in samples):
                    raise ValueError('expected 20 successful samples per lane')
        comparisons = []
        if name == 'memory-target':
            comparisons = [(target, samples, report['summary'][target], 'unset', 'target')
                           for target, samples in report['samples'].items()]
        else:
            comparisons = [(variant + '/' + mode, samples, report['summary'][variant][mode], 'baseline', 'candidate')
                           for variant, modes in report['samples'].items()
                           for mode, samples in modes.items()]
        for comparison, samples, summary, base_lane, candidate_lane in comparisons:
            hashes = {sample['stdout_sha256'] for lane in samples.values() for sample in lane}
            if len(hashes) != 1:
                raise ValueError('output hashes differ: ' + comparison)
            for lane in (base_lane, candidate_lane):
                for metric in ('seconds', 'peak_rss_bytes'):
                    if statistics.median(sample[metric] for sample in samples[lane]) != summary[lane][metric]:
                        raise ValueError('median does not reproduce: ' + comparison)
            for metric in ('seconds', 'peak_rss_bytes'):
                change = (summary[candidate_lane][metric] / summary[base_lane][metric] - 1) * 100
                if change != summary['change_percent'][metric]:
                    raise ValueError('percentage does not reproduce: ' + comparison)
        report['evidence_phase'] = 'Final growth-capped reader; reservation-cap or memory-target variations identified by lane.'
        report['export_source_sha256'] = digest(path)
        source_receipts[name + '-results.json'] = digest(path)
        save(args.output / (name + '-results.json'), report)
    save(args.output / 'export-receipt.json', {
        'source_receipt_sha256': source_receipts,
        'exporter_sha256': digest(Path(__file__)),
        'transformations': ['Local paths replaced with placeholders.',
                           'Build-input manifests gzip-compressed without losing fields.',
                           'Original-reader attribution distinguished from final-reader timing.',
                           'All measured samples, source/tool/executable hashes and output hashes preserved.'],
        'reproduction_helpers': {
            'build_variants.py': 'Portable adaptation of the recorded build.py; hashes differ from the collection script.',
            'measure_variants.py': 'Portable adaptation of measure.py with explicit paths and additional environment recording.',
            'measure_memory_targets.py': 'Portable adaptation of measure_memory.py; same measured settings and pair ordering.'},
    })


if __name__ == '__main__':
    main()
