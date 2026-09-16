#!/usr/bin/env python3
"""Check metrics against pinned scc and exercise directory and Git source selection."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

COUNTERS = {'bytes': 'Bytes', 'lines': 'Lines', 'code': 'Code',
            'comment': 'Comment', 'blank': 'Blank', 'complexity': 'Complexity'}
SOURCES = {
    'src/RawStringLimitation.cs': b'var text = """\n"\n// commentlike\n""";\n',
    'src/RawStringLimitation.java': b'var text = """\n"\n// commentlike\n""";\n',
    'src/Example.java': b'''package sample;
// One line comment.
public record Example(String name) {
    public int count(int n) {
        /* A block comment
           across lines. */
        if (n > 2 && n < 10) return n;
        return 0;
    }
}
''',
    'src/TextBlock.java': b'''class TextBlock {
    String text = """
        // This is a Java text block.
        /* It contains comment markers. */
        """;
    boolean check(int n) { return n > 0 ? true : false; }
}
''',
    'src/Example.cs': b'''namespace Example;
// A comment.
public record Person(string Name) {
    public int Count(int n) {
        if (n > 2 && n < 10) return n;
        return 0;
    }
}
''',
    'src/Strings.cs': b'''namespace Example;
public class Strings {
    string escaped = "// not a comment \\" quoted";
    string verbatim = @"/* not a comment */ "" quoted";
    string raw = """
        // raw string text
        """;
    string interpolated = $"answer: {1 + 2}";
}
''',
    'src/CRLF.cs': b'// CRLF comment\r\n\r\nclass CRLF { }\r\n',
    'src/NoNewline.java': b'class NoNewline { int n = 1; }',
    'src/Unicode.java': 'class Unicode { String name = "caf\u00e9"; }\n'.encode(),
    'src/view.tsx': b'// JSX comment\nexport const View = () => <div>Hello</div>;\n',
    'src/long.cs': b'class Long {\n/*\n' + b'comment crosses read prefix\n' * 9000 + b'*/\nint n = 1;\n}\n',
    'generated/Generated.cs': b'// generated file\nclass Generated {}\n',
    'vendor/Vendor.java': b'class Vendor {}\n',
    'docs/Example.cs': b'class Documentation {}\n',
    'logs/activity.xml': b'<?xml version="1.0"?>\n<logs><entry>example</entry></logs>\n',
    'README.md': b'# Metrics fixture\n\nExample text.\n',
    '.gitattributes': b'generated/** linguist-generated\nvendor/** linguist-vendored\ndocs/** linguist-documentation\n',
}
EXPECTED_SOURCE = {name for name in SOURCES if name.startswith('src/')}


def execute(command, cwd=None, env=None):
    result = subprocess.run(command, capture_output=True, text=True, cwd=cwd, env=env, timeout=600)
    if result.returncode:
        raise RuntimeError(f'{command}: exit {result.returncode}: {result.stderr[:3000]}')
    return result.stdout


def metrics(binary, root, *options):
    report = json.loads(execute([binary, 'analyze', 'metrics', '--json', '--files', *options, str(root)]))
    assert report['schema_version'] == '1.1.0', report
    return report['metrics']


def counted(report):
    return {row['path']: row for row in report['files'] if row['status'] == 'counted'}


def reference(binary, path):
    result = json.loads(execute([binary, '--no-config', '--no-cocomo', '--no-gitignore',
                                 '--no-ignore', '--no-scc-ignore', '--by-file', '--format', 'json', str(path)]))
    files = [file for group in result for file in group['Files']]
    assert len(files) == 1, result
    return files[0]


def compare(report, reference_binary, root):
    details = []
    for name, row in counted(report).items():
        expected = reference(reference_binary, root / name)
        assert row['grammar'] == expected['Language'], (name, row['grammar'], expected['Language'])
        actual = row['counts']
        for key, field in COUNTERS.items():
            assert actual[key] == expected[field], (name, key, actual[key], expected[field])
        assert actual['files'] == 1
        details.append({'path': name, 'sha256': hashlib.sha256((root/name).read_bytes()).hexdigest(),
                        'language': row['language'], 'grammar': row['grammar'], 'counts': actual})
    for key in ('files', *COUNTERS):
        assert report['totals'][key] == sum(row['counts'][key] for row in counted(report).values())
        assert report['totals'][key] == sum(row[key] for row in report['languages'])
        assert report['totals'][key] == sum(row[key] for row in report['directories'])
    return details


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--scc', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--large-xml', action='store_true', help='write and scan a 1,100 MiB XML file')
    args = parser.parse_args()
    binary, scc = str(args.candidate.resolve()), str(args.scc.resolve())
    version = execute([scc, '--version']).strip()
    assert version.endswith('4.1.0'), version
    receipt = {'schema_version': '1.0.0', 'started_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
               'candidate_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
               'scc_sha256': hashlib.sha256(Path(scc).read_bytes()).hexdigest(), 'scc_version': version,
               'harness_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'checks': []}
    with tempfile.TemporaryDirectory(prefix='dircue-metrics-') as folder:
        root = Path(folder)
        for name, content in SOURCES.items():
            path = root/name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        source = metrics(binary, root)
        assert source['source'] == 'directory'
        assert source['status'] == 'complete', source
        assert set(counted(source)) == EXPECTED_SOURCE, set(counted(source))
        receipt['source_files'] = compare(source, scc, root)
        receipt['known_upstream_limitations'] = ['scc 4.1.0 counts the // line in RawStringLimitation.java and .cs as a comment although it is raw-string content; parity is not semantic parsing accuracy.']
        for name in ('src/RawStringLimitation.java', 'src/RawStringLimitation.cs'):
            assert counted(source)[name]['counts']['comment'] == 1
        receipt['checks'].append('source scope: exact full-file counters, Java/C# variants, TSX, long block comment')
        text = metrics(binary, root, '--metrics-scope', 'text')
        assert {'generated/Generated.cs', 'vendor/Vendor.java', 'docs/Example.cs', 'logs/activity.xml', 'README.md'} <= set(counted(text)), text
        receipt['text_files'] = compare(text, scc, root)
        receipt['checks'].append('text scope includes detected generated/vendor/docs/XML; totals agree at every aggregation level')
        all_report = json.loads(execute([binary, 'analyze', 'all', '--json', '--metrics', '--files', str(root)]))
        assert all_report['metrics'] == source
        plain = json.loads(execute([binary, 'analyze', 'all', '--json', str(root)]))
        assert 'metrics' not in plain and plain['schema_version'] == '1.0.0'
        receipt['checks'].append('metrics opt-in: all without metrics retains schema 1.0.0 and omits counting')
        limited = metrics(binary, root, '--metrics-max-file-bytes', '1024')
        assert limited['status'] == 'partial'
        assert 'src/long.cs' not in counted(limited)
        assert limited['totals']['files'] == source['totals']['files']-1
        receipt['checks'].append('oversized eligible file yields partial coverage and contributes no prefix counts')
        env = os.environ | {'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': os.devnull,
                            'GIT_AUTHOR_NAME': 'fixture', 'GIT_AUTHOR_EMAIL': 'fixture@example.invalid',
                            'GIT_COMMITTER_NAME': 'fixture', 'GIT_COMMITTER_EMAIL': 'fixture@example.invalid'}
        def git(*options):
            return execute(['git', '-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgsign=false', *options], cwd=root, env=env).strip()
        git('init', '-q', '--initial-branch=main')
        git('add', '.')
        git('commit', '-qm', 'fixture')
        committed = metrics(binary, root)
        assert committed['source'] == 'git' and committed['tree'] == git('rev-parse', 'HEAD^{tree}')
        assert committed['totals'] == source['totals']
        original_tree = committed['tree']
        (root/'src/Example.cs').write_bytes(b'class Replaced {}\n')
        (root/'src/New.java').write_bytes(b'class New {}\n')
        git('add', 'src/Example.cs')
        dirty = metrics(binary, root)
        assert dirty == committed, (dirty, committed)
        current = metrics(binary, root, '--source', 'directory')
        assert current['totals'] != committed['totals'] and 'src/New.java' in counted(current)
        compare(current, scc, root)
        git('add', '.')
        git('commit', '-qm', 'next fixture')
        previous = metrics(binary, root, '--rev', 'HEAD~1')
        assert previous['tree'] == original_tree and previous['totals'] == committed['totals']
        receipt['checks'].append('Git HEAD and --rev metrics use committed blobs despite staged, dirty, and untracked files')
        size = 1100*1024*1024 if args.large_xml else 2*1024*1024
        with (root/'logs/large.xml').open('wb') as stream:
            header, footer = b'<?xml version="1.0"?>\n<logs>\n', b'</logs>\n'
            stream.write(header)
            remaining = size-len(header)-len(footer)
            block = b'<entry>log</entry>\n'*4096
            while remaining:
                if remaining < len(block):
                    record = b'<entry>log</entry>\n'
                    part = record * (remaining//len(record))
                    part += b' ' * (remaining-len(part))
                else:
                    part = block
                stream.write(part)
                remaining -= len(part)
            stream.write(footer)
        skipped_xml = metrics(binary, root, '--source', 'directory')
        assert 'logs/large.xml' not in counted(skipped_xml)
        assert skipped_xml['totals'] == current['totals']
        receipt['xml_bytes'] = size
        receipt['checks'].append('large XML excluded by default without changing code totals')
        if args.large_xml:
            requested_xml = metrics(binary, root, '--source', 'directory', '--metrics-scope', 'text')
            assert requested_xml['status'] == 'partial' and 'logs/large.xml' not in counted(requested_xml)
            receipt['checks'].append('explicit text scope reports over-limit XML as partial without prefix counts')
    receipt['result'] = 'pass'
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2)+'\n')
    print(json.dumps({'result': 'pass', 'checks': receipt['checks'], 'receipt': str(args.output)}))


if __name__ == '__main__':
    main()
