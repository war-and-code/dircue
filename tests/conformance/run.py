#!/usr/bin/env python3
"""Differential black-box checks against the pinned Ruby Linguist CLI.
# Result keys retain the original evidence format across the project rename.

Generates isolated fixtures; never executes content from a scanned repository.
Exit 1 on any unexpected mismatch. Reports retain actual reference output.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
IMAGE = "dircue-linguist:9.7.0"


def command(argv, cwd=None, env=None):
    p = subprocess.run([str(x) for x in argv], cwd=cwd, env=env, capture_output=True, text=True)
    return {"exit_code": p.returncode, "stdout": p.stdout, "stderr": p.stderr}


def terminal_path_oracle(reference, case, mode):
    """Escape only the independently enumerated hostile fixture names."""
    paths = {
        'attrs-quoted': [('tab\tname.py', r'tab\tname.py')],
        'unusual-paths': [('control-\x1b[31m.py', r'control-\x1b[31m.py'),
                          ('line\nbreak.js', r'line\nbreak.js')],
    }
    if mode != 'text-breakdown' or case not in paths or reference['exit_code'] != 0:
        return None
    expected = dict(reference)
    for raw, escaped in paths[case]:
        marker = '  ' + raw + '\n'
        if expected['stdout'].count(marker) != 1:
            return None
        expected['stdout'] = expected['stdout'].replace(marker, '  ' + escaped + '\n')
    if case == 'attrs-quoted':
        if reference['stderr']:
            return None
        # This fixture already exercises dircue's documented quoted-pattern
        # notices. Permit these exact diagnostics, not arbitrary stderr.
        expected['stderr'] = ''.join(
            f'warning: .gitattributes: line {line}: quoted pattern ignored to match Linguist 9.7.0 (unsupported_gitattributes)\n'
            for line in (1, 2))
    return expected


def terminal_oracle_matches(expected, actual):
    """Require the path substitution to be the only output difference."""
    matched, detail = compare(expected, actual, False)
    return matched and expected['stderr'] == actual['stderr'], detail


def git(directory, *args):
    env = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
               GIT_AUTHOR_DATE="2026-01-01T00:00:00Z", GIT_COMMITTER_DATE="2026-01-01T00:00:00Z")
    result = command(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", *args], directory, env)
    if result["exit_code"]:
        raise RuntimeError(result)
    return result["stdout"].strip()


def generate(base):
    cases = []
    def add(name, files, category="classification", mutate=None, target="."):
        directory = base / name
        directory.mkdir(parents=True)
        for filename, content in files.items():
            path = directory / filename
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content if isinstance(content, bytes) else content.encode())
        git(directory, "init", "-q", "--initial-branch=main")
        git(directory, "add", "--all")
        git(directory, "commit", "-q", "--allow-empty", "-m", "Deterministic conformance fixture")
        if mutate:
            mutate(directory)
        cases.append({"id": name, "category": category, "requirement": "MUST", "target": target,
                      "head": git(directory, "rev-parse", "HEAD"), "fixture_sha256": fixture_digest(directory)})
        return directory

    go = 'package main\n\nfunc main() {}\n'
    py = 'def main():\n    print("hello")\n\nmain()\n'
    js = 'function main() { console.log("hello"); }\nmain();\n'

    java_record = '''package com.acme.domain;

public record Customer(String id, String displayName) {
    public Customer {
        if (id.isBlank()) throw new IllegalArgumentException("id");
    }
}
'''
    java_service = '''package com.acme.orders;

import com.acme.domain.Customer;

public final class OrderService {
    public String greeting(Customer customer) {
        return "Hello, " + customer.displayName();
    }
}
'''
    add("java-enterprise", {
        ".gitattributes": "generated/** linguist-generated=true\nvendor/** linguist-vendored=false\n",
        "pom.xml": """<project xmlns=\"http://maven.apache.org/POM/4.0.0\"><modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>enterprise-parent</artifactId><version>1.0.0</version><packaging>pom</packaging><modules><module>services/orders</module></modules></project>\n""",
        "settings.gradle.kts": 'rootProject.name = "enterprise"\ninclude(":services:orders")\n',
        "build.gradle.kts": 'plugins { java }\njava { toolchain { languageVersion.set(JavaLanguageVersion.of(21)) } }\n',
        "src/main/java/module-info.java": "module com.acme.enterprise { exports com.acme.domain; }\n",
        "src/main/java/com/acme/domain/Customer.java": java_record,
        "services/orders/pom.xml": """<project xmlns=\"http://maven.apache.org/POM/4.0.0\"><modelVersion>4.0.0</modelVersion><parent><groupId>com.acme</groupId><artifactId>enterprise-parent</artifactId><version>1.0.0</version></parent><artifactId>orders</artifactId></project>\n""",
        "services/orders/build.gradle": "plugins { id 'java-library' }\ndependencies { implementation project(':domain') }\n",
        "services/orders/src/main/java/com/acme/orders/OrderService.java": java_service,
        "generated/com/acme/GeneratedModel.java": "package com.acme; public final class GeneratedModel {}\n",
        "vendor/com/acme/SupportedVendorApi.java": "package com.acme; public interface SupportedVendorApi {}\n",
    }, "java")

    csharp_modern = '''namespace Acme.App;

public sealed record Customer(string Id, string DisplayName);

public static class Greeting
{
    public static string Render(Customer customer) => $"""
        Hello, {customer.DisplayName}
        Customer id: {customer.Id}
        """;
}
'''
    add("dotnet-enterprise", {
        ".gitattributes": "src/App/Generated/** linguist-generated=true\nvendor/** linguist-vendored=false\n",
        "Enterprise.sln": "Microsoft Visual Studio Solution File, Format Version 12.00\n# Visual Studio Version 17\n",
        "global.json": '{"sdk":{"version":"8.0.100","rollForward":"latestFeature"}}\n',
        "Directory.Build.props": "<Project><PropertyGroup><Nullable>enable</Nullable><LangVersion>latest</LangVersion></PropertyGroup></Project>\n",
        "Directory.Packages.props": "<Project><ItemGroup><PackageVersion Include=\"Microsoft.Extensions.Logging\" Version=\"8.0.0\" /></ItemGroup></Project>\n",
        "src/Shared/Shared.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
        "src/App/App.csproj": '<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup><ItemGroup><ProjectReference Include="../Shared/Shared.csproj" /><PackageReference Include="Microsoft.Extensions.Logging" /></ItemGroup></Project>\n',
        "src/Legacy/Legacy.csproj": '<Project ToolsVersion="15.0" xmlns="http://schemas.microsoft.com/developer/msbuild/2003"><ItemGroup><Compile Include="LegacyService.cs" /><ProjectReference Include="../Shared/Shared.csproj" /></ItemGroup></Project>\n',
        "src/App/Customer.cs": csharp_modern,
        "src/App/Views/Home/Index.cshtml": "@model Acme.App.Customer\n<h1>Hello, @Model.DisplayName</h1>\n",
        "src/App/Views/Shell.xaml": '<Window xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"><Grid><TextBlock Text="Hello" /></Grid></Window>\n',
        "src/Legacy/LegacyService.cs": "namespace Acme.Legacy { public sealed class LegacyService { } }\n",
        "src/App/Generated/Api.g.cs": "namespace Acme.App.Generated; public sealed class Api {}\n",
        "vendor/Interop/SupportedInterop.cs": "namespace Acme.Interop; public static class SupportedInterop {}\n",
    }, "dotnet")

    add("basic", {"main.go": go, "main.py": py, "main.js": js, "index.html": '<!doctype html><html><body>Hello</body></html>\n', "style.css": 'body { color: red; }\n'})
    add("empty", {}, "selection")
    add("empty-source", {"main.go": "", "main.py": ""}, "selection")
    add("hidden", {".hidden.py": py, ".config/main.go": go, "normal.go": go}, "selection")
    add("data-prose", {"main.go": go, "README.md": '# A project\n', "data.json": '{"a": 1}\n', "config.yaml": 'hello: world\n', "notes.txt": 'hello world\n'}, "selection")
    add("binary", {"main.go": go, "binary.py": b'\x00\x01\x02garbage\x00', "plain.rtf": b'{\\rtf1 text}' * 700, "late-nul.rtf": b'{\\rtf1 text}' * 700 + b'\x00', "icon.png": b'\x89PNG\r\n\x1a\n\x00\x00'}, "selection")
    add("encoding", {"latin1.py": '# café\nprint("hello")\n'.encode('latin-1'), "utf16.py": py.encode('utf-16'), "utf32.py": py.encode('utf-32'), "bom.py": b'\xef\xbb\xbf' + py.encode()}, "encoding")
    add("large-source", {"main.py": '# a useful comment\n' * 60000 + py}, "selection")
    add("lfs", {".gitattributes": "*.py filter=lfs\n", "main.go": go, "large.py": "version https://git-lfs.github.com/spec/v1\noid sha256:" + "a" * 64 + "\nsize 123456789\n"}, "selection")
    add("vendor", {"main.go": go, "vendor/example/a.go": go, "node_modules/a/index.js": js, "third_party/a/main.py": py}, "selection")
    add("generated", {"main.go": go, "generated.go": '// Code generated by example. DO NOT EDIT.\n' + go, "bundle.min.js": 'var a=1;' * 100}, "selection")
    add("grouped", {"x.tsx": 'export default function X() { return <div>Hello</div>; }\n', "x.jsx": 'export default function X() { return <div>Hello</div>; }\n', "x.h": '#ifndef X_H\n#define X_H\nint f(void);\n#endif\n'}, "classification")
    add("shebang", {"run": '#!/usr/bin/env python3\n' + py, "script": '#!/bin/sh\necho hello\n', "modeline": '# -*- mode: ruby -*-\nputs "hello"\n'})
    add("attrs-language", {".gitattributes": '*.odd linguist-language=python\n*.strange linguist-language=c++\n', "x.odd": py, "x.strange": 'int main() { return 0; }\n'}, "attributes")
    add("attrs-selection", {".gitattributes": 'generated.go linguist-generated\ncustom/** linguist-vendored\nvendor/** linguist-vendored=false\n*.md linguist-detectable\nmain.py linguist-detectable=false\n', "generated.go": go, "custom/a.py": py, "vendor/a.py": py, "main.py": py, "README.md": '# Detectable\n', "main.go": go}, "attributes")
    add("attrs-documentation", {".gitattributes": 'docs/** linguist-documentation=false\ncustom/** linguist-documentation\n', "docs/main.py": py, "custom/main.py": py, "main.go": go}, "attributes")
    add("attrs-language-reset", {".gitattributes": '*.py linguist-language=Ruby\nmain.py -linguist-language\n', "main.py": py, "other.py": py}, "attributes")
    add("attrs-unknown-language", {".gitattributes": '*.py linguist-language=NotARealLanguage\n', "main.py": py}, "attributes")
    add("attrs-nested", {".gitattributes": '*.py linguist-generated\n', "main.go": go, "sub/.gitattributes": '*.py -linguist-generated\n', "sub/main.py": py, "excluded.py": py, "other/main.py": py}, "attributes")
    add("attrs-macro", {".gitattributes": '[attr]generated linguist-generated\n*.py generated\n', "main.go": go, "main.py": py}, "attributes")
    add("attrs-empty-values", {".gitattributes": "a.json linguist-detectable=\nb.go linguist-language=\nc.go -diff\n", "a.json": "{}\n", "b.go": go, "c.go": go}, "attributes")
    add("attrs-only-empty", {".gitattributes": "empty.odd linguist-language=Python\n", "empty.odd": ""}, "attributes")
    add("attrs-forced-empty", {".gitattributes": "empty.odd linguist-language=Python\n", "empty.odd": "", "main.go": go}, "attributes")
    add("attrs-macro-order", {".gitattributes": "[attr]generated linguist-generated\na.py generated -linguist-generated\nb.py -linguist-generated generated\n", "main.go": go, "a.py": py, "b.py": py}, "attributes")
    add("attrs-quoted", {".gitattributes": '"with space.py" linguist-generated\n"tab\\tname.py" linguist-generated\n', "main.go": go, "with space.py": py, "tab\tname.py": py}, "attributes")
    add("attrs-quoted-portable-patterns", {".gitattributes": "with?space.py linguist-generated\ntab?name.py linguist-generated\n", "main.go": go, "with space.py": py, "tab\tname.py": py}, "attributes")
    add("attrs-unset", {".gitattributes": '*.py linguist-generated\nmain.py !linguist-generated\n', "main.go": go, "main.py": py, "ignored.py": py}, "attributes")
    add("attrs-paths", {".gitattributes": '/root.py linguist-generated\nsub/**/x.py linguist-generated\n[ab].py linguist-generated\n', "root.py": py, "sub/root.py": py, "sub/x.py": py, "sub/deep/x.py": py, "a.py": py, "c.py": py}, "attributes")
    def dirty(p):
        (p / 'main.go').unlink()
        (p / 'main.py').write_text(py * 5)
        (p / 'untracked.js').write_text(js)
        (p / 'ignored.js').write_text(js)
        (p / '.gitattributes').write_text('*.py linguist-generated\n')
    add("git-dirty-head", {"main.go": go, "main.py": py, ".gitignore": 'ignored.js\n'}, "git", dirty)
    def info_attrs(p):
        (p / ".git/info/attributes").write_text("*.py linguist-generated\n")
    add("git-info-attributes", {"main.go": go, "main.py": py}, "git", info_attrs)
    def symlink(p):
        (p / 'link.py').symlink_to('main.py')
        git(p, 'add', 'link.py')
        git(p, 'commit', '-q', '-m', 'Add symlink')
    add("symlink", {"main.py": py}, "git", symlink)
    def gitlink(p):
        git(p, 'update-index', '--add', '--cacheinfo', '160000,' + git(p, 'rev-parse', 'HEAD') + ',submodule')
        git(p, 'commit', '-q', '-m', 'Add gitlink')
        (p / 'submodule').mkdir()
        (p / 'submodule/main.py').write_text(py)
    add("submodule", {"main.go": go}, "git", gitlink)
    add("subdirectory-oracle", {"main.py": py}, "selection")
    add("subdirectory", {"main.go": go, "sub/main.py": py}, "git", target="sub")
    add("single-files", {"main.go": go, "main.py": py, "main.js": js, "README.md": '# A project\n', "empty.py": '', "crlf.py": py.replace('\n', '\r\n'), "utf16.py": "a\n\n".encode("utf-16"), "utf32.py": "a\n\n".encode("utf-32"), "binary.png": b'\x89PNG\r\n\x1a\n\x00\x00', "late-nul.rtf": b'{\\rtf1 text}' * 700 + b'\x00'}, "files")
    def sized_csharp(size, nul_at=None, utf16=False):
        source = ('namespace Synthetic; public class Program {}\n' + '// filler\n' * (size // 8 + 1))
        data = bytearray((b'\xff\xfe' + source.encode('utf-16-le'))[:size] if utf16 else source.encode()[:size])
        if nul_at is not None:
            data[nul_at:nul_at + (2 if utf16 else 1)] = b'\0' * (2 if utf16 else 1)
        return bytes(data)
    file_limits = add('single-file-limits', {
        'small-text.cs': sized_csharp(300*1024),
        'small-late-nul.cs': sized_csharp(300*1024, 200*1024),
        'small-utf16-late-nul.cs': sized_csharp(300*1024, 200*1024, utf16=True),
        'limit-late-nul.cs': sized_csharp(1<<20, 200*1024),
        'over-limit-late-nul.cs': sized_csharp((1<<20)+1, 200*1024),
        'over-limit-nul-edge.cs': sized_csharp((1<<20)+1, (1<<20)-1),
        'over-limit-nul-outside.cs': sized_csharp((1<<20)+1, 1<<20),
        'large-late-nul.cs': sized_csharp(2<<20, 1100*1024),
        'over-limit-text.cs': sized_csharp((1<<20)+1),
        'over-limit-generated.js': ((b'function main() { return 42; }\n' + b'// filler\n' * 150000)[:(1<<20)+1-len(b'\n//# sourceMappingURL=generated.js.map\n')] + b'\n//# sourceMappingURL=generated.js.map\n'),
    }, 'single-file-read-scope')
    targets = sorted(path.name for path in file_limits.iterdir() if path.is_file())
    cases[-1]['single_file_targets'] = targets
    flat_limits = base / 'single-file-limits-flat'
    shutil.copytree(file_limits, flat_limits, ignore=shutil.ignore_patterns('.git'))
    cases.append({'id': 'single-file-limits-flat', 'category': 'flat-single-files', 'requirement': 'MUST',
                  'target': '.', 'head': None, 'fixture_sha256': fixture_digest(flat_limits), 'single_file_targets': targets})
    magic = add('single-file-magic', {
        'upper.PNG': b'\x89PNG\r\n\x1a\nhello',
        'lower.png': b'\x89PNG\r\n\x1a\nhello',
        'alternate.jpe': b'\xff\xd8\xff\x00',
        'image.webp': b'RIFF\x00\x00\x00\x00WEBP',
        'text.png': b'#!/usr/bin/python\nprint(1)\n',
        'nul.ps': b'%!PS-Adobe-3.0\n%%Title: example\n\x00showpage\n',
        'magic.gif': b'GIF89aordinarytext',
        'magic.pdf': b'%PDF-1.7\nordinarytext\n',
        'utf16le.cs': b'\xff\xfe' + 'namespace Synthetic; public class Program {}\n'.encode('utf-16-le'),
        'utf16be.cs': b'\xfe\xff' + 'namespace Synthetic; public class Program {}\n'.encode('utf-16-be'),
        'utf32le.cs': b'\xff\xfe\x00\x00' + 'namespace Synthetic; public class Program {}\n'.encode('utf-32-le'),
        'utf32be.cs': b'\x00\x00\xfe\xff' + 'namespace Synthetic; public class Program {}\n'.encode('utf-32-be'),
    }, 'single-file-magic')
    targets = sorted(path.name for path in magic.iterdir() if path.is_file())
    cases[-1]['single_file_targets'] = targets
    flat_magic = base / 'single-file-magic-flat'
    shutil.copytree(magic, flat_magic, ignore=shutil.ignore_patterns('.git'))
    cases.append({'id': 'single-file-magic-flat', 'category': 'flat-single-files', 'requirement': 'MUST',
                  'target': '.', 'head': None, 'fixture_sha256': fixture_digest(flat_magic), 'single_file_targets': targets})

    def second_revision(p):
        (p / 'main.py').write_text(py)
        git(p, 'add', '--all')
        git(p, 'commit', '-q', '-m', 'Second revision')
    add("nested-tree-size", {"a.go": go, "sub/b.py": py}, "git")
    add("revisions", {"main.go": go}, "git", second_revision)
    add("unusual-paths", {"unicode-π.py": py, "with space.go": go, "line\nbreak.js": js, "control-\x1b[31m.py": py}, "paths")
    return cases


def fixture_digest(directory):
    h = hashlib.sha256()
    for path in sorted(directory.rglob('*')):
        if '.git' in path.relative_to(directory).parts:
            continue
        h.update(str(path.relative_to(directory)).encode() + b'\0')
        if path.is_symlink():
            h.update(os.readlink(path).encode())
        elif path.is_file():
            h.update(path.read_bytes())
    return h.hexdigest()


def normalize_json(output):
    data = json.loads(output)
    if isinstance(data, dict):
        for value in data.values():
            if isinstance(value, dict) and 'files' in value:
                value['files'] = sorted(value['files'])
    return data


def compare(reference, actual, json_mode):
    if reference['exit_code'] != actual['exit_code']:
        return False, 'exit code differs'
    if reference['exit_code']:
        return True, 'both reject the invocation (error wording is not compared)'
    if json_mode:
        try:
            return normalize_json(reference['stdout']) == normalize_json(actual['stdout']), 'JSON keys, sizes, percentage strings, and file sets'
        except (ValueError, TypeError) as e:
            return False, 'invalid JSON: ' + str(e)
    return reference['stdout'] == actual['stdout'], 'exact stdout bytes'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default=IMAGE)
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--output', type=Path, default=HERE / 'results' / 'latest.json')
    parser.add_argument('--mode', action='append', help='Only execute named mode (repeatable); use json-breakdown for fast diagnosis')
    parser.add_argument('--case', action='append', help='Only execute named fixture (repeatable)')
    parser.add_argument('--keep-fixtures', type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='dircue-conformance-') as temporary:
        work = Path(temporary)
        fixtures = args.keep_fixtures.resolve() if args.keep_fixtures else work / 'fixtures'
        fixtures.mkdir(parents=True, exist_ok=True)
        cases = generate(fixtures)
        if args.case:
            selected = set(args.case)
            for companion in ('single-file-limits', 'single-file-magic'):
                if companion + '-flat' in selected:
                    selected.add(companion)
            if 'subdirectory' in selected:
                selected.add('subdirectory-oracle')
            if 'attrs-quoted' in selected:
                selected.add('attrs-quoted-portable-patterns')
            cases = [c for c in cases if c['id'] in selected]
            if not cases:
                parser.error('no recognized --case selected')
        binary = args.binary.resolve() if args.binary else work / 'dircue'
        if not args.binary:
            result = command(['go', 'build', '-trimpath', '-o', binary, '.'], ROOT)
            if result['exit_code']:
                raise RuntimeError(result)
        invocations = []
        modes = [('json-breakdown', ['--breakdown', '--json'], True), ('json', ['--json'], True),
                 ('text', [], False), ('text-breakdown', ['--breakdown'], False), ('short-flags', ['-bj'], True)]
        if args.mode:
            modes = [m for m in modes if m[0] in args.mode]
        for case in cases:
            if case["category"] == "flat-single-files":
                continue
            for mode, flags, is_json in modes:
                invocations.append({'id': case['id'] + '/' + mode, 'category': case['category'], 'requirement': case['requirement'],
                                    'case': case['id'], 'target': case['target'], 'mode': mode, 'flags': flags, 'json': is_json})
        extras = []
        for case in cases:
            if 'single_file_targets' in case:
                for filename in case['single_file_targets']:
                    for mode, flags, is_json in [('file-json', ['--json'], True), ('file-text', [], False), ('file-strategies', ['--strategies'], False)]:
                        extras.append(dict(case=case['id'], id=case['id']+'/'+filename+'/'+mode, category=case['category'], requirement='MUST', target=filename, mode=mode, flags=flags, json=is_json, relative=True))
            if case['id'] == 'single-files':
                extras.append(dict(case=case['id'],id=case['id']+'/tree-size-zero-file',category='single-files',requirement='MUST',target='main.py',mode='file-json',flags=['--tree-size=0','--json'],json=True,relative=True))
                for filename in ['main.go', 'main.py', 'main.js', 'README.md', 'empty.py', 'crlf.py', 'utf16.py', 'utf32.py', 'binary.png', 'late-nul.rtf']:
                    for mode, flags, is_json in [('file-json', ['--json'], True), ('file-text', [], False), ('file-strategies', ['--strategies'], False)]:
                        extras.append(dict(case=case['id'], id=case['id']+'/'+filename+'/'+mode, category='single-files', requirement='MUST', target=filename, mode=mode, flags=flags, json=is_json, relative=True))
            if case['id'] == 'git-dirty-head':
                extras.append(dict(case=case['id'], id=case['id']+'/file-json', category='single-files', requirement='MUST', target='main.py', mode='file-json', flags=['--json'], json=True, relative=True))
            if case['id'] == 'git-dirty-head':
                for filename in ['untracked.js', 'main.go']:
                    extras.append(dict(case=case['id'], id=case['id']+'/'+filename+'/file-json', category='single-files', requirement='MUST', target=filename, mode='file-json', flags=['--json'], json=True, relative=True))
            if case['id'] == 'symlink':
                extras.append(dict(case=case['id'], id=case['id']+'/file-json', category='single-files', requirement='MUST', target='link.py', mode='file-json', flags=['--json'], json=True, relative=True))
            if case['id'] == 'nested-tree-size':
                for limit in [-1,0,1,2,3]:
                    extras.append(dict(case=case['id'], id=case['id']+'/tree-size-'+str(limit), category='cli-options', requirement='MUST', target='.', mode='tree-size', flags=['--tree-size='+str(limit), '-bj'], json=True))
            if case['id'] == 'revisions':
                extras.append(dict(case=case['id'], id=case['id']+'/historical-file-json', category='single-files', requirement='MUST', target='main.py', mode='file-json', flags=['--rev','HEAD~1','--json'], json=True, relative=True))
                for mode, flags in [('revision', ['--rev', 'HEAD~1', '--breakdown', '--json']), ('revision-short', ['-r', 'HEAD~1', '-bj']), ('invalid-revision', ['--rev', 'does-not-exist', '--json'])]:
                    extras.append(dict(case=case['id'], id=case['id']+'/'+mode, category='revision', requirement='MUST', target='.', mode=mode, flags=flags, json=True))
            if case['id'] == 'basic':
                for mode, flags, is_json in [('tree-size', ['--tree-size=1', '-bj'], True), ('tree-size-strategies', ['--tree-size=1','--strategies'], False), ('strategies', ['--strategies'], False), ('strategies-json', ['--strategies', '-bj'], True), ('trailing-flags', ['-bj'], True)]:
                    extras.append(dict(case=case['id'], id=case['id']+'/'+mode, category='cli-options', requirement='MUST', target='.', mode=mode, flags=flags, json=is_json, trailing=mode=='trailing-flags'))
        invocations.extend(x for x in extras if not args.mode or x['mode'] in args.mode)
        if not invocations:
            parser.error('no matching invocations selected')
        manifest = work / 'manifest.json'
        manifest.write_text(json.dumps(invocations))
        ruby = '''require 'json'; require 'open3'; tasks=JSON.parse(File.read('/manifest.json')); results={}; tasks.each do |t|; cwd=File.join('/fixtures',t['case']); target=t['relative'] ? t['target'] : File.join(cwd,t['target']); argv=t['trailing'] ? [target,*t['flags']] : [*t['flags'],target]; out,err,status=Open3.capture3('github-linguist', *argv, chdir:cwd); results[t['id']]={exit_code:status.exitstatus,stdout:out,stderr:err}; end; puts JSON.generate(results)'''
        reference = command(['docker', 'run', '--rm', '--network=none', '-v', str(fixtures)+':/fixtures:ro', '-v', str(manifest)+':/manifest.json:ro', args.image, 'ruby', '-e', ruby])
        if reference['exit_code']:
            raise RuntimeError(reference)
        refs = json.loads(reference['stdout'])
        results = []
        for invocation in invocations:
            cwd = fixtures / invocation['case']
            target = invocation['target'] if invocation.get('relative') else cwd / invocation['target']
            argv = [target, *invocation['flags']] if invocation.get('trailing') else [*invocation['flags'], target]
            actual = command([binary, *argv], cwd=cwd)
            ref = refs[invocation['id']]
            passed, comparison = compare(ref, actual, invocation['json'])
            status = 'PASS' if passed else 'FAIL'
            if invocation['case'] == 'subdirectory' and ref['exit_code'] != 0:
                extension_oracle = refs['subdirectory-oracle/' + invocation['mode']]
                extension_pass, _ = compare(extension_oracle, actual, invocation['json'])
                if extension_pass:
                    status = 'XFAIL'
                    comparison = 'DISC-003: reference rejects subdirectories; candidate matches the committed subtree oracle'
            if invocation['case'] == 'single-file-limits-flat' and invocation['target'] in ('over-limit-late-nul.cs', 'over-limit-nul-edge.cs', 'over-limit-generated.js') and not passed and ref['exit_code'] == 0:
                prefix_oracle = refs['single-file-limits/' + invocation['target'] + '/' + invocation['mode']]
                prefix_pass, _ = compare(prefix_oracle, actual, invocation['json'])
                if prefix_pass:
                    status = 'XFAIL'
                    comparison = 'DISC-007: Git-free single files >1 MiB use bounded 128 KiB content; candidate must match actual Git LazyBlob oracle for identical file bytes'
            if invocation['case'] == 'symlink' and invocation['mode'] == 'file-json' and ref['exit_code'] == 0:
                if actual['exit_code'] == 1 and not actual['stdout'] and 'not a regular Git file' in actual['stderr']:
                    status = 'XFAIL'
                    comparison = 'DISC-006: explicit single-file symlink is refused with no output; normal file counterpart is covered'
            escaped_oracle = terminal_path_oracle(ref, invocation['case'], invocation['mode'])
            if escaped_oracle is not None and not passed:
                escaped_pass, _ = terminal_oracle_matches(escaped_oracle, actual)
                if escaped_pass:
                    status = 'XFAIL'
                    comparison = 'DISC-008: terminal control characters in fixture paths are escaped; all other reference bytes and exit status must match'
            result = dict(invocation, status=status, comparison=comparison, reference=ref, actual=actual)
            results.append(result)
            print(result['status'], result['id'], flush=True)
        # Flat directory operation is an explicit extension: compare the same committed
        # fixture after removing Git metadata to the reference's committed-tree output.
        for case in cases:
            if case['category'] == 'git' or case['id'] + '/json-breakdown' not in refs:
                continue
            flat = work / 'flat' / case['id']
            shutil.copytree(fixtures / case['id'], flat, ignore=shutil.ignore_patterns('.git'), symlinks=True)
            actual = command([binary, '--breakdown', '--json', flat])
            ref = refs[case['id'] + '/json-breakdown']
            passed, comparison = compare(ref, actual, True)
            status = 'PASS' if passed else 'FAIL'
            if case['id'] == 'attrs-quoted' and not passed:
                extension_oracle = refs['attrs-quoted-portable-patterns/json-breakdown']
                extension_pass, _ = compare(extension_oracle, actual, True)
                if extension_pass:
                    status = 'XFAIL'
                    comparison = 'DISC-004: flat mode applies Git-standard quoted patterns; equivalent portable-pattern reference verified'
            results.append({'id': case['id'] + '/flat-extension', 'category': 'flat-extension', 'requirement': 'MUST',
                            'status': status, 'comparison': comparison, 'reference': ref, 'actual': actual})
        git_head = command(['git', 'rev-parse', '--verify', 'HEAD'], ROOT)
        git_state = command(['git', 'status', '--porcelain=v1'], ROOT)
        provenance = {'reference_image': args.image, 'reference_version': '9.7.0',
                      'image_inspect': command(['docker', 'image', 'inspect', args.image, '--format', '{{json .}}'])['stdout'],
                      'reference_gems': command(['docker', 'run', '--rm', '--network=none', args.image, 'cat', '/reference-gems.txt'])['stdout'],
                      'go_version': command(['go', 'version'])['stdout'].strip(),
                      'auragaze_git_head': git_head['stdout'].strip() if git_head['exit_code'] == 0 else None,
                      'auragaze_worktree_state': {'exit_code': git_state['exit_code'], 'dirty': bool(git_state['stdout']) if git_state['exit_code'] == 0 else None, 'porcelain': git_state['stdout']},
                      'auragaze_binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                      'generator_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                      'fixture_cases': cases, 'unix_time': time.time()}
        report = {'provenance': provenance, 'summary': {'total': len(results), 'passed': sum(r['status']=='PASS' for r in results),
                                                      'failed': sum(r['status']=='FAIL' for r in results), 'expected_failures': sum(r['status']=='XFAIL' for r in results)}, 'results': results}
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + '\n')
        markdown = ['# Generated conformance matrix', '', 'Reference: github-linguist 9.7.0. Synthetic requirements are scoped below; this is not a claim of universal language compatibility.', '',
                    '| Category | MUST tested | Passing | Divergent (verified extension) | Failing | Exact-match score |', '|---|---:|---:|---:|---:|---:|']
        for category in sorted({r['category'] for r in results}):
            group = [r for r in results if r['category']==category]
            passed = sum(r['status']=='PASS' for r in group)
            divergent = sum(r['status']=='XFAIL' for r in group)
            markdown.append(f'| {category} | {len(group)} | {passed} | {divergent} | {len(group)-passed-divergent} | {passed/len(group):.1%} |')
        markdown += ['', '## Failures', ''] + ['- ' + r['id'] + ': ' + r['comparison'] for r in results if r['status']=='FAIL']
        markdown += ['', '## Verified intentional extensions', ''] + ['- ' + r['id'] + ': ' + r['comparison'] for r in results if r['status']=='XFAIL']
        args.output.with_suffix('.md').write_text('\n'.join(markdown) + '\n')
        print(json.dumps(report['summary']))
        return bool(report['summary']['failed'])


if __name__ == '__main__':
    raise SystemExit(main())
