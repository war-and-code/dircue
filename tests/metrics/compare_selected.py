#!/usr/bin/env python3
"""Measure optional metrics and standalone scc on identical selected source files."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import tempfile

from compare_language import measured, summary
from run import SOURCES, compare, counted, execute, metrics


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--scc', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--runs', type=int, default=20)
    args = parser.parse_args()
    if args.runs < 20:
        parser.error('use at least 20 samples')
    binary, scc = str(args.candidate.resolve()), str(args.scc.resolve())
    receipt = {'schema_version': '1.0.0', 'candidate_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
               'scc_sha256': hashlib.sha256(Path(scc).read_bytes()).hexdigest(),
               'scc_version': execute([scc, '--version']).strip(), 'platform': platform.platform(),
               'methodology': '20 alternating process samples after three warmups; system time peak RSS; same full-file inputs and counting settings; dircue additionally classifies and reports source coverage',
               'limitations': 'Small synthetic corpus, warm filesystem cache. Not a universal speed comparison. p99+ are observed order statistics.'}
    with tempfile.TemporaryDirectory(prefix='dircue-metrics-timing-') as folder:
        root = Path(folder)
        for name, content in SOURCES.items():
            if name.startswith('src/'):
                (root/name).parent.mkdir(parents=True, exist_ok=True)
                (root/name).write_bytes(content)
        report = metrics(binary, root)
        assert len(counted(report)) == sum(name.startswith('src/') for name in SOURCES)
        receipt['files'] = compare(report, scc, root)
        commands = {'dircue_metrics': [binary, 'analyze', 'metrics', '--json', '--source', 'directory', str(root)],
                    'scc': [scc, '--no-config', '--no-cocomo', '--no-gitignore', '--no-ignore', '--no-scc-ignore', '--format', 'json', str(root)]}
        receipt['commands'] = commands
        for _ in range(3):
            for command in commands.values():
                measured(command)
        samples = {name: [] for name in commands}
        for i in range(args.runs):
            for name in (list(commands) if i % 2 == 0 else list(reversed(commands))):
                samples[name].append(measured(commands[name]))
        receipt['samples'] = samples
        receipt['summary'] = {name: summary(rows) for name, rows in samples.items()}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2)+'\n')
    print(json.dumps(receipt['summary']))


if __name__ == '__main__':
    main()
