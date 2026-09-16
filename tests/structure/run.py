#!/usr/bin/env python3
"""Compare production structural reports with direct pinned-worker observations."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def execute(command, **kwargs):
    result = subprocess.run(command, capture_output=True, timeout=300, **kwargs)
    if result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {result.stderr[-2000:]!r}")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--corpus-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--files', type=int, default=50)
    args = parser.parse_args()
    if args.files < 4:
        parser.error('--files must be at least 4')
    candidate, worker = args.candidate.resolve(), args.worker.resolve()
    sampling = ROOT / 'prototypes/structural/tests/benchmark.py'
    spec = importlib.util.spec_from_file_location('structural_sample', sampling)
    sampler = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(sampler)
    receipt = {'candidate_sha256': sha(candidate), 'worker_sha256': sha(worker),
               'harness_sha256': sha(Path(__file__)), 'sampling_sha256': sha(sampling),
               'platform': platform.platform(), 'corpora': [],
               'method': 'Sampled tracked working files; explicit attributes include generated/vendor samples. '
                         'Compare each production result with a separate direct worker invocation. '
                         'Repeat production output with 1 and 8 workers. No build execution or network. '
                         'This validates integration, not independent correctness of BCA metrics.'}
    for name, suffix, language in [('spring-framework', '.java', 'Java'), ('roslyn', '.cs', 'C#'), ('aspnetcore', '.cs', 'C#')]:
        root = args.corpus_root.resolve() / name
        files = sampler.sample(root, suffix, args.files)
        corpus = {'name': name, 'commit': sampler.git(root, 'rev-parse', 'HEAD'), 'files': files}
        started = time.monotonic()
        with tempfile.TemporaryDirectory(prefix='dircue-production-structure-') as temporary:
            stage = Path(temporary)
            for entry in files:
                target = stage / entry['path']
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(root / entry['path'], target)
            (stage / '.gitattributes').write_text(f'*{suffix} linguist-vendored=false linguist-generated=false linguist-documentation=false linguist-detectable=true\n')
            command = [str(candidate), 'analyze', 'structure', '--source', 'directory', '--json', '--files', '--structural-worker', str(worker), str(stage)]
            first = execute([*command, '--workers', '1']).stdout
            second = execute([*command, '--workers', '8']).stdout
            if first != second:
                raise AssertionError(f'{name}: nondeterministic report')
            report = json.loads(first)
            structural = report['structure']
            actual = {entry['path']: entry for entry in structural['files']}
            if structural['parse_count'] != len(files) or structural['analyzed_files'] != len(files):
                raise AssertionError(f'{name}: unexpected coverage {structural}')
            partial = []
            for entry in files:
                path = entry['path']
                request = {'path': path, 'language': language, 'source': (stage / path).read_bytes().decode('utf-8'), 'mode': 'combined'}
                direct = json.loads(execute([str(worker)], input=json.dumps(request).encode()).stdout)
                observed = actual[path]
                for key in ['status', 'language', 'source_bytes', 'parse_count', 'syntax_errors', 'observations', 'metrics', 'provenance']:
                    if observed[key] != direct[key]:
                        raise AssertionError(f'{name}:{path}:{key}: differing worker result')
                if observed['status'] == 'partial':
                    partial.append(path)
            expected = 'partial' if partial else 'complete'
            if structural['status'] != expected:
                raise AssertionError(f'{name}: coverage status')
            corpus.update(analyzed_files=len(files), partial_files=partial,
                          parse_count=structural['parse_count'], status=structural['status'],
                          observations=structural['observations'],
                          structural_report_sha256=hashlib.sha256(json.dumps(structural, sort_keys=True).encode()).hexdigest())
        corpus['validation_wall_seconds'] = time.monotonic() - started
        receipt['corpora'].append(corpus)
        print(f"{name}: {len(files)} exact comparisons; {len(partial)} qualified partial results", flush=True)
    receipt['passed'] = True
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')


if __name__ == '__main__':
    main()
