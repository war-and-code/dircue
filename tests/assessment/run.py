#!/usr/bin/env python3
"""Generated assessment acceptance checks; supplied binary, no package managers.

Relations originate in repository-assessment populations, not analyzer internals.
Each strength score is sensitivity * independence / cost; all exceed 2.
Output corruption probes measure these assertion guards, not implementation mutation
coverage. Receipts explicitly distinguish those probes from real CLI checks.
"""
from __future__ import annotations
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import random
import shutil
import subprocess
import time

MR_MATRIX = [
    ('irrelevant_file_addition', 'additive', 4, 5, 2),
    ('forest_replication', 'multiplicative', 5, 4, 3),
    ('root_rename', 'permutative', 4, 4, 2),
    ('formatting_invariance', 'equivalence', 4, 4, 2),
    ('subtree_selection', 'inclusive', 5, 5, 2),
    ('lock_removal_restore', 'invertive', 5, 4, 3),
    ('provider_independence', 'equivalence', 5, 5, 3),
]
STATES = ('covered', 'missing', 'not_applicable', 'unsupported', 'unknown')
METRICS = ('projects', 'project_roots', 'workspace_membership', 'local_dependencies')


def count(metric):
    assert isinstance(metric, dict), metric
    value = metric['count']
    assert isinstance(value, int) and not isinstance(value, bool) and value >= 0, metric
    return value


def candidate_counts(a):
    return {(r['ecosystem'], r['filename']): r['files'] for r in a['manifest_candidates']}


def locks(a):
    return {r['ecosystem']: {k: count(r[k]) for k in ('projects', 'eligible', *STATES)}
            for r in a['lockfiles']}


def population(a):
    return {'candidates': candidate_counts(a),
            **{k: count(a[k]) for k in METRICS}, 'locks': locks(a)}


def inventory(a):
    return tuple(count(a['inventory'][k]) for k in ('files', 'bytes'))


def assert_same_populations(a, b):
    assert population(a) == population(b), (population(a), population(b))


def assert_added_files(a, b, files, byte_count):
    old, new = inventory(a), inventory(b)
    assert new == (old[0] + files, old[1] + byte_count), (old, new, files, byte_count)
    assert_same_populations(a, b)


def assert_scaled(a, b, factor):
    assert inventory(b) == tuple(v * factor for v in inventory(a))
    assert candidate_counts(b) == {k: v * factor for k, v in candidate_counts(a).items()}
    for k in METRICS:
        assert count(b[k]) == count(a[k]) * factor, k
    assert locks(b) == {eco: {k: v * factor for k, v in row.items()}
                        for eco, row in locks(a).items()}


def assert_renamed(a, b, old, new):
    assert inventory(a) == inventory(b)
    assert_same_populations(a, b)
    def rename(path):
        return new + path[len(old):] if path == old or path.startswith(old + '/') else path
    left = sorted((rename(v['path']), rename(v['root']), v['kind'])
                  for v in a['candidate_evidence'])
    right = sorted((v['path'], v['root'], v['kind']) for v in b['candidate_evidence'])
    assert left == right, (left, right)


def assert_subtree(parent, child, files, byte_count, projects):
    assert inventory(child) == (files, byte_count)
    assert inventory(parent)[0] > files
    assert inventory(parent)[1] > byte_count
    assert count(child['projects']) == projects
    assert count(child['project_roots']) == projects
    assert count(child['workspace_membership']) == 0
    assert count(child['local_dependencies']) == projects - 1
    assert candidate_counts(child).get(('npm', 'package.json'), 0) == projects
    npm = locks(child)['npm']
    assert npm['projects'] == npm['covered'] == npm['eligible'] == projects, npm
    assert sum(npm[k] for k in STATES if k != 'covered') == 0, npm


def assert_lock_removed(a, b):
    x, y = locks(a)['npm'], locks(b)['npm']
    assert y['projects'] == x['projects'] and y['eligible'] == x['eligible']
    assert y['covered'] == x['covered'] - 1
    assert y['missing'] == x['missing'] + 1
    for k in ('not_applicable', 'unsupported', 'unknown'):
        assert x[k] == y[k], (k, x, y)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree_digest(root):
    value = hashlib.sha256()
    for path in sorted(root.rglob('*')):
        name = str(path.relative_to(root)).replace(os.sep, '/')
        value.update(name.encode('utf-8') + b'\0')
        if path.is_symlink():
            value.update(b'symlink:' + str(path.readlink()).encode('utf-8'))
        elif path.is_file():
            value.update(path.read_bytes())
        value.update(b'\0')
    return value.hexdigest()


def put(root, path, data):
    target = root / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data if isinstance(data, bytes) else data.encode())


def jput(root, path, value, indent=None):
    put(root, path, json.dumps(value, sort_keys=True, indent=indent) + '\n')


def npm(root, path, name, version):
    manifest = dict(name=name, version='1.0.0', dependencies={'fixture-dep': version})
    jput(root, path + '/package.json', manifest)
    jput(root, path + '/package-lock.json', dict(name=name, lockfileVersion=3,
         packages={'': manifest, 'node_modules/fixture-dep': {'version': version}}))
    put(root, path + '/main.js', 'console.log("fixture");\n')


def regular_inventory(root):
    paths = [p for p in root.rglob('*') if p.is_file() and not p.is_symlink()]
    return len(paths), sum(p.stat().st_size for p in paths)


def generate(root, seed):
    rng = random.Random(seed)
    n = rng.randint(2, 5)
    for i in range(n):
        npm(root, 'standalone/s%02d' % i, 'fixture-%d-%d' % (seed, i),
            '^%d.%d.0' % (rng.randint(1, 5), rng.randint(0, 9)))
    for i in range(1, n):
        path = 'standalone/s%02d' % i
        manifest = json.loads((root / path / 'package.json').read_bytes())
        manifest['dependencies']['local-peer'] = 'file:../s%02d' % (i - 1)
        jput(root, path + '/package.json', manifest)
        lock = json.loads((root / path / 'package-lock.json').read_bytes())
        lock['packages'][''] = manifest
        jput(root, path + '/package-lock.json', lock)
    # Workspace dependency table differs between members to expose root-entry reuse.
    members = rng.randint(2, 4)
    manifest = dict(name='workspace-root-%d' % seed, private=True,
                    dependencies={'fixture-root-dep': '^1.0.0'}, workspaces=['packages/*'])
    jput(root, 'workspace/package.json', manifest)
    entries = {'': manifest}
    for i in range(members):
        member = dict(name='member-%d-%d' % (seed, i), version='1.0.0',
                      dependencies={'fixture-member-dep': '^%d.0.0' % (i + 2)})
        jput(root, 'workspace/packages/m%02d/package.json' % i, member)
        entries['packages/m%02d' % i] = member
        entries['node_modules/' + member['name']] = dict(resolved='packages/m%02d' % i, link=True)
    jput(root, 'workspace/package-lock.json', dict(lockfileVersion=3, packages=entries))
    # Unsupported native-lock association ecosystem still contributes native projects.
    put(root, 'python/pyproject.toml', '[project]\nname = "fixture-python"\nversion = "1.0.0"\n')
    return n, members


class Runner:
    def __init__(self, candidate, output):
        self.candidate, self.output = candidate, output
        self.calls, self.checks, self.probes, self.skips = [], [], [], []

    def run(self, name, root, extra=(), mode='all'):
        cmd = [str(self.candidate), 'analyze', mode,
               *(['--assessment'] if mode == 'all' else []), '--json',
               '--source', 'directory', *extra, str(root)]
        source_sha256 = tree_digest(root)
        started = time.monotonic()
        result = subprocess.run(cmd, capture_output=True, timeout=60)
        (self.output / (name + '.stdout.json')).write_bytes(result.stdout)
        (self.output / (name + '.stderr')).write_bytes(result.stderr)
        self.calls.append(dict(name=name, command=cmd, source_sha256=source_sha256, returncode=result.returncode,
                               seconds=round(time.monotonic() - started, 3)))
        assert source_sha256 == tree_digest(root), 'fixture changed during scan: ' + name
        assert result.returncode == 0, (name, result.returncode, result.stderr.decode(errors='replace'))
        report = json.loads(result.stdout)
        assert 'assessment' in report, (name, report.keys())
        a = report['assessment']
        self.check(name + ': report composition and source', lambda: self.basic(report))
        return report

    def check(self, name, operation):
        operation()
        self.checks.append(name)

    def probe(self, name, operation):
        try:
            operation()
        except (AssertionError, KeyError, TypeError):
            self.probes.append(dict(name=name, rejected=True))
            return
        raise AssertionError('Output corruption survived: ' + name)

    @staticmethod
    def basic(report):
        a = report['assessment']
        assert a['version'] == '1.0.0'
        for metric in [*a['inventory'].values(), *(a[k] for k in METRICS)]:
            assert metric['completeness'] in ('complete', 'lower_bound')
            assert metric['scope']
            if metric['completeness'] == 'lower_bound':
                assert metric['reasons']
            else:
                assert metric['reasons'] == []
        assert report['schema_version'] == '1.9.0'
        assert a['source']['mode'] == 'directory'
        assert a['source']['consistency'] == 'live_directory_metadata'
        assert 'languages' in report and 'summary' in report
        assert sum(v['bytes'] for v in report['languages']) == report['summary']['language_bytes']
        assert len({v['name'] for v in report['languages']}) == len(report['languages'])
        count(a['inventory']['files']); count(a['inventory']['bytes'])
        for row in a['lockfiles']:
            assert sum(count(row[k]) for k in STATES) == count(row['projects']), row
            assert count(row['covered']) <= count(row['eligible']) <= count(row['projects']), row
        total = a['lockfiles_overall']
        assert sum(count(total[k]) for k in STATES) == count(total['projects']), total
        for k in ('projects', 'eligible', *STATES):
            assert sum(count(row[k]) for row in a['lockfiles']) == count(total[k]), k


def run_seed(r, root, seed, syft):
    fixture = root / ('seed-%d' % seed)
    fixture.mkdir()
    n, members = generate(fixture, seed)
    prefix = 's%d-' % seed
    before = r.run(prefix + 'baseline', fixture)
    a = before['assessment']
    r.check(prefix + 'standalone and combined command equivalence', lambda: expect(
        before, r.run(prefix + 'standalone-command', fixture, mode='assessment')))
    expected = regular_inventory(fixture)
    r.check(prefix + 'generated JavaScript file and byte facts', lambda: expect(
        [(v['file_count'], v['bytes']) for v in before['languages'] if v['name'] == 'JavaScript'],
        [(n, sum(p.stat().st_size for p in fixture.rglob('main.js')))]))
    r.check(prefix + 'generated exact file and byte facts', lambda: expect(inventory(a), expected))
    r.check(prefix + 'generated manifest counts and bytes', lambda: assert_exact_manifests(a, fixture))
    corrupt = copy.deepcopy(a); corrupt['manifest_candidates'][0]['files'] -= 1
    r.probe(prefix + 'manifest filename undercount', lambda: assert_exact_manifests(corrupt, fixture))
    corrupt = copy.deepcopy(before); corrupt['assessment']['lockfiles_overall']['covered']['count'] += 1
    r.probe(prefix + 'overall partition double count', lambda: r.basic(corrupt))
    r.check(prefix + 'generated projects and workspace membership', lambda: expect(
        (count(a['projects']), count(a['project_roots']), count(a['workspace_membership']), count(a['local_dependencies'])),
        (n + members + 2, n + members + 2, members, n - 1)))
    r.check(prefix + 'standalone and shared workspace lock coverage', lambda: expect(
        locks(a)['npm']['covered'], n + members + 1))
    r.check(prefix + 'unsupported ecosystem population', lambda: expect(
        locks(a)['python']['unsupported'], 1))
    r.check(prefix + 'one/eight worker equivalence', lambda: expect(
        before, r.run(prefix + 'workers-eight', fixture, ('--workers', '8'))))

    payload = ('# inert-%d\n' % seed).encode() * (seed % 5 + 1)
    put(fixture, 'notes/inert.txt', payload)
    added = r.run(prefix + 'added-inert', fixture)['assessment']
    r.check(prefix + 'MR additive irrelevant file', lambda: assert_added_files(a, added, 1, len(payload)))
    corrupt = copy.deepcopy(added); corrupt['inventory']['files']['count'] += 1
    r.probe(prefix + 'inventory off by one', lambda: assert_added_files(a, corrupt, 1, len(payload)))

    # Compose rename after addition; length preserves metadata byte counts.
    (fixture / 'standalone').rename(fixture / 'renamedone')
    renamed = r.run(prefix + 'renamed', fixture)['assessment']
    r.check(prefix + 'MR path permutation + addition chain', lambda: assert_renamed(
        added, renamed, 'standalone', 'renamedone'))
    corrupt = copy.deepcopy(renamed); corrupt['candidate_evidence'][0]['path'] += '.wrong'
    r.probe(prefix + 'candidate path corruption', lambda: assert_renamed(
        added, corrupt, 'standalone', 'renamedone'))

    selected = fixture / 'renamedone'
    selected_facts = regular_inventory(selected)
    subtree = r.run(prefix + 'subtree', selected)['assessment']
    r.check(prefix + 'MR inclusive subtree confinement', lambda: assert_subtree(
        renamed, subtree, *selected_facts, n))
    corrupt = copy.deepcopy(subtree); corrupt['workspace_membership']['count'] += 1
    r.probe(prefix + 'workspace leaked across selection', lambda: assert_subtree(
        renamed, corrupt, *selected_facts, n))

    clone_root = root / ('clones-%d' % seed)
    factor = 2 + seed % 2
    for i in range(factor):
        shutil.copytree(selected, clone_root / ('copy%d' % i))
    replicated = r.run(prefix + 'replicated', clone_root)['assessment']
    r.check(prefix + 'MR multiplication independent forest', lambda: assert_scaled(subtree, replicated, factor))
    corrupt = copy.deepcopy(replicated); corrupt['projects']['count'] -= 1
    r.probe(prefix + 'project dropped from replicated population', lambda: assert_scaled(subtree, corrupt, factor))

    chosen = selected / 's00/package.json'
    value = json.loads(chosen.read_bytes()); value['description'] = 'unrelated metadata ' + str(seed)
    chosen.write_text(json.dumps(value, indent=4) + '\n')
    formatted = r.run(prefix + 'formatted', fixture)['assessment']
    r.check(prefix + 'MR formatting and unrelated metadata equivalence', lambda: assert_same_populations(renamed, formatted))
    corrupt = copy.deepcopy(formatted); corrupt['local_dependencies']['count'] += 1
    r.probe(prefix + 'metadata became dependency', lambda: assert_same_populations(renamed, corrupt))

    lock_path = selected / 's00/package-lock.json'
    saved = lock_path.read_bytes(); lock_path.unlink()
    removed = r.run(prefix + 'removed-lock', fixture)['assessment']
    r.check(prefix + 'MR lock availability removal', lambda: assert_lock_removed(formatted, removed))
    corrupt = copy.deepcopy(removed)
    next(v for v in corrupt['lockfiles'] if v['ecosystem'] == 'npm')['covered']['count'] += 1
    r.probe(prefix + 'stale covered lock count', lambda: assert_lock_removed(formatted, corrupt))
    lock_path.write_bytes(saved)
    restored = r.run(prefix + 'restored-lock', fixture)
    r.check(prefix + 'MR removal restoration roundtrip', lambda: expect(formatted, restored['assessment']))
    # A mismatching declaration still has an observed supported lock association.
    mismatch = json.loads(chosen.read_bytes()); mismatch['dependencies']['fixture-dep'] = '^99.0.0'
    chosen.write_text(json.dumps(mismatch) + '\n')
    differing = r.run(prefix + 'different-direct-table', fixture)
    r.check(prefix + 'coverage statistics independent of policy result', lambda: assert_same_populations(
        restored['assessment'], differing['assessment']))
    r.check(prefix + 'direct mismatch actually exercised', lambda: require(any(
        c['status'] == 'different' for ctx in differing['lockfiles']['contexts']
        for c in ctx['checks']), 'expected a direct-table mismatch'))

    enriched = r.run(prefix + 'syft-attached', fixture,
                     ('--syft-report', str(syft), '--syft-root', '/'))
    r.check(prefix + 'MR provider report cannot change native facts', lambda: expect(
        differing['assessment'], enriched['assessment']))
    corrupt = copy.deepcopy(enriched['assessment']); corrupt['project_roots']['count'] += 1
    r.probe(prefix + 'provider invented project root', lambda: expect(differing['assessment'], corrupt))


def assert_exact_manifests(a, root):
    expected = {}
    for p in root.rglob('*'):
        if p.is_file() and not p.is_symlink() and p.name in ('package.json', 'pyproject.toml'):
            eco = 'npm' if p.name == 'package.json' else 'python'
            row = expected.setdefault((eco, p.name), [0, 0])
            row[0] += 1; row[1] += p.stat().st_size
    actual = {}
    for row in a['manifest_candidates']:
        key = row['ecosystem'], row['filename']
        assert key not in actual, key
        actual[key] = [row['files'], row['bytes']]
    assert actual == expected, (actual, expected)
    assert count(a['manifest_candidate_population']) == sum(v[0] for v in expected.values())


def expect(actual, expected):
    assert actual == expected, (actual, expected)


def require(condition, message):
    assert condition, message


def directed(r, root, syft):
    boundary = root / 'boundary'
    npm(boundary, 'ancestor', 'ancestor', '^1.0.0')
    jput(boundary, 'ancestor/unrelated/package.json', dict(name='unrelated', dependencies={'x': '^1.0.0'}))
    report = r.run('ancestor-selected-child', boundary / 'ancestor/unrelated')
    r.check('outside ancestor lock cannot cover selected child', lambda: expect(
        (locks(report['assessment'])['npm']['covered'], locks(report['assessment'])['npm']['missing']), (0, 1)))
    whole_ancestor = r.run('ancestor-selected-whole', boundary / 'ancestor')['assessment']
    r.check('unrelated descendant cannot inherit selected ancestor lock', lambda: expect(
        (locks(whole_ancestor)['npm']['projects'], locks(whole_ancestor)['npm']['covered'],
         locks(whole_ancestor)['npm']['missing'] + locks(whole_ancestor)['npm']['unknown']), (2, 1, 1)))
    jput(boundary, 'ancestor/unrelated/package-lock.json', dict(lockfileVersion=999, packages={}))
    unsupported = r.run('unsupported-lock-version', boundary / 'ancestor/unrelated')
    r.check('unsupported lock version is explicit', lambda: expect(locks(unsupported['assessment'])['npm']['unsupported'], 1))
    attributed = root / 'attribute-scope'
    npm(attributed, 'app', 'attribute-fixture', '^1.0.0')
    attr_before = r.run('attribute-baseline', attributed)
    attr_bytes = '*.js linguist-generated=true\n'
    put(attributed, '.gitattributes', attr_bytes)
    attr_after = r.run('attribute-generated-exclusion', attributed)
    r.check('language exclusion preserves regular filename population', lambda: assert_added_files(
        attr_before['assessment'], attr_after['assessment'], 1, len(attr_bytes.encode())))
    r.check('language statistics respect generated-file attributes', lambda: expect(
        [v for v in attr_after['languages'] if v['name'] == 'JavaScript'], []))
    dotnet = root / 'dotnet'
    csproj = '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup><ItemGroup><PackageReference Include="Fixture.Core" Version="1.0.0" /></ItemGroup></Project>\n'
    put(dotnet, 'locked/App.csproj', csproj)
    put(dotnet, 'missing/App.csproj', csproj)
    put(dotnet, 'empty/App.csproj', '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n')
    jput(dotnet, 'locked/packages.lock.json', dict(version=1, dependencies={'net8.0': {
        'Fixture.Core': dict(type='Direct', requested='[1.0.0, )', resolved='1.0.0', contentHash='fixture')}}))
    nuget = r.run('nuget-association-partition', dotnet)
    row = locks(nuget['assessment'])['nuget']
    r.check('NuGet covered missing and not-applicable partition', lambda: expect(
        (row['projects'], row['covered'], row['missing'], row['not_applicable'], row['unknown']),
        (3, 1, 1, 1, 0)))
    malformed = root / 'malformed'
    put(malformed, 'package.json', '{broken json')
    report = r.run('malformed-manifest', malformed)
    r.check('malformed manifest remains filename candidate', lambda: expect(
        candidate_counts(report['assessment']).get(('npm', 'package.json')), 1))
    r.check('malformed manifest parsing uncertainty disclosed', lambda: require(
        report['declarations']['status'] == 'partial', report['declarations']))
    r.check('unparsed manifest population remains explicit', lambda: expect(
        count(report['assessment']['unparsed_manifest_candidates']), 1))
    r.check('malformed manifest cannot invent a parsed project', lambda: expect(
        count(report['assessment']['projects']), 0))
    r.check('malformed project population uncertainty carries reasons', lambda: require(
        report['assessment']['projects']['completeness'] == 'lower_bound' and
        'manifest_candidates_unparsed' in report['assessment']['projects']['reasons'],
        report['assessment']['projects']))
    # A valid project plus malformed .NET input must count only the valid project.
    malformed_dotnet = root / 'malformed-dotnet'
    put(malformed_dotnet, 'valid/App.csproj', csproj)
    put(malformed_dotnet, 'bad/Broken.csproj', '<Project><broken')
    malformed_native = r.run('malformed-dotnet-population', malformed_dotnet)['assessment']
    r.check('malformed .NET declaration cannot overcount parsed projects', lambda: expect(
        (count(malformed_native['projects']), count(malformed_native['project_roots']),
         count(malformed_native['unparsed_manifest_candidates'])), (1, 1, 1)))
    r.check('malformed ecosystem population carries lower-bound disclosure', lambda: require(
        malformed_native['lockfiles_overall']['projects']['completeness'] == 'lower_bound' and
        bool(malformed_native['lockfiles_overall']['projects']['reasons']), malformed_native))
    # Symlink points outside input and must not become a filename candidate.
    links = root / 'links'; links.mkdir()
    try:
        (links / 'package.json').symlink_to(malformed / 'package.json')
    except (OSError, NotImplementedError) as error:
        r.skips.append('symlink control unavailable: ' + str(error))
    else:
        put(links, 'readme.txt', 'safe data\n')
        linked = r.run('untrusted-manifest-symlink', links)
        r.check('untrusted symlink excluded from regular filename population', lambda: expect(
            (inventory(linked['assessment'])[0], sum(candidate_counts(linked['assessment']).values())), (1, 0)))
    # Provider has a wrong tree/root and cannot fix an absent native lock.
    wrong = r.run('wrong-provider-root', boundary / 'ancestor/unrelated',
                  ('--syft-report', str(syft), '--syft-root', '/not-this-tree'))
    r.check('wrong provider root preserves native assessment', lambda: expect(
        wrong['assessment'], unsupported['assessment']))
    odd = root / 'odd-paths'
    npm(odd, 'unicodé/服务 #space', 'unicode-fixture', '^1.0.0')
    if os.name != 'nt':
        npm(odd, 'literal\\backslash/line\nend', 'posix-fixture', '^1.0.0')
    else:
        r.skips.append('POSIX literal backslash and control-name case skipped on Windows')
    odd_report = r.run('selected-legal-filenames', odd)['assessment']
    r.check('selected legal filenames preserve exact regular inventory', lambda: expect(
        inventory(odd_report), regular_inventory(odd)))
    r.check('selected legal filenames preserve manifest facts', lambda: assert_exact_manifests(odd_report, odd))
    r.check('selected legal filename coverage remains complete', lambda: require(
        odd_report['inventory']['files']['completeness'] == 'complete' and
        odd_report['projects']['completeness'] == 'complete', odd_report))
    r.check('selected legal filename locks preserve project identity', lambda: expect(
        locks(odd_report)['npm'], dict(projects=1 if os.name == 'nt' else 2,
            eligible=1 if os.name == 'nt' else 2, covered=1 if os.name == 'nt' else 2,
            missing=0, not_applicable=0, unsupported=0, unknown=0)))
    variants = root / 'manifest-group-variants'
    for i in range(140):
        put(variants, 'p%04d/custom%04d.PrOj' % (i, i), '<Project />\n')
        conventional = 'directory.build.props'
        letters = iter(range(32))
        spelling = ''.join(c.upper() if c.isalpha() and i & (1 << next(letters)) else c
                           for c in conventional)
        put(variants, 'c%04d/%s' % (i, spelling), '<Project />\n')
        put(variants, 'requirements/group%04d.txt' % i, 'fixture-dep==1.0.0\n')
        jput(variants, 'd%04d\\global.json' % i, dict(sdk=dict(version='8.0.100')))
    variant_report = r.run('manifest-group-variants', variants)['assessment']
    r.check('manifest name variants retain exact selected metadata', lambda: expect(
        inventory(variant_report), regular_inventory(variants)))
    r.check('recognized unbounded filenames normalize to bounded groups', lambda: expect(
        {item['filename']: item['files'] for item in variant_report['manifest_candidates']},
        {'*.proj': 140, 'directory.build.props': 140, 'requirements/*.txt': 140,
         'global.json': 140}))
    r.check('normalized manifest groups preserve complete candidate population', lambda: require(
        count(variant_report['manifest_candidate_population']) == 560 and
        variant_report['manifest_candidate_population']['completeness'] == 'complete', variant_report))
    gowork = root / 'go-workspace-population'
    put(gowork, 'go.work', 'go 1.23.0\nuse (\n ./a\n ./b\n)\n')
    put(gowork, 'a/go.mod', 'module example.invalid/a\ngo 1.23.0\nrequire example.invalid/b v0.0.0\nreplace example.invalid/b => ../b\n')
    put(gowork, 'b/go.mod', 'module example.invalid/b\ngo 1.23.0\n')
    go_report = r.run('go-workspace-population', gowork)['assessment']
    r.check('Go workspace configuration does not inflate parsed module roots', lambda: expect(
        (count(go_report['projects']), count(go_report['project_roots'])), (2, 2)))
    r.check('Go workspace membership and local replacement are distinct facts', lambda: expect(
        (count(go_report['workspace_membership']), count(go_report['local_dependencies'])), (2, 1)))
    r.check('Go modules remain outside native lock eligibility', lambda: expect(
        locks(go_report)['go'], dict(projects=2, eligible=0, covered=0, missing=0,
            not_applicable=0, unsupported=2, unknown=0)))
    if os.name != 'nt':
        aliased = root / 'dotnet-posix-source-alias'
        put(aliased, 'dir\\App.csproj', csproj)
        put(aliased, 'dir/App.csproj', csproj)
        alias_report = r.run('dotnet-posix-source-alias', aliased)['assessment']
        r.check('POSIX .NET source aliases retain exact selected metadata', lambda: expect(
            inventory(alias_report), regular_inventory(aliased)))
        r.check('POSIX .NET source aliases retain both physical manifest candidates', lambda: expect(
            candidate_counts(alias_report).get(('nuget', '*.csproj')), 2))
        r.check('POSIX .NET normalized identity ambiguity qualifies parsed population', lambda: require(
            count(alias_report['projects']) <= 2 and
            alias_report['projects']['completeness'] == 'lower_bound' and
            bool(alias_report['projects']['reasons']), alias_report))
    else:
        r.skips.append('POSIX .NET literal backslash/source-alias control skipped on Windows')
    many = root / 'sample-cap'
    for i in range(300):
        item = dict(name='sample-%d' % i)
        if i > 0:
            item['dependencies'] = {'local-peer': 'file:../p%04d' % (i - 1)}
        jput(many, 'p%04d/package.json' % i, item)
    capped = r.run('candidate-sample-cap', many)['assessment']
    r.check('filename aggregate remains exact above evidence cap', lambda: expect(
        candidate_counts(capped).get(('npm', 'package.json')), 300))
    r.check('sample omission does not truncate project aggregate', lambda: expect(count(capped['projects']), 300))
    r.check('candidate evidence omission is explicit', lambda: require(
        sum(capped['omitted_candidate_evidence'].values()) > 0, capped))
    r.check('local relationship sample cap preserves exact aggregate', lambda: expect(
        count(capped['local_dependencies']), 299))
    r.check('local relationship sample omission is explicit', lambda: require(
        capped['omitted_local_dependency_evidence'] > 0 and
        capped['local_dependencies']['completeness'] == 'complete' and
        capped['local_dependencies']['reasons'] == [], capped['local_dependencies']))
    large_workspace = root / 'workspace-evidence-cap'
    jput(large_workspace, 'package.json', dict(name='large-workspace', private=True, workspaces=['packages/*']))
    for i in range(300):
        jput(large_workspace, 'packages/m%04d/package.json' % i, dict(name='member-%04d' % i))
    sampled_workspace = r.run('workspace-evidence-cap', large_workspace)['assessment']
    r.check('workspace sample cap preserves exact aggregate', lambda: expect(
        count(sampled_workspace['workspace_membership']), 300))
    r.check('workspace sample omission is explicit', lambda: require(
        sampled_workspace['omitted_workspace_evidence'] > 0 and
        sampled_workspace['workspace_membership']['completeness'] == 'complete' and
        sampled_workspace['workspace_membership']['reasons'] == [], sampled_workspace['workspace_membership']))
    r.check('sample cap preserves aggregate completeness', lambda: require(
        capped['manifest_candidate_population']['completeness'] == 'complete' and
        capped['projects']['completeness'] == 'complete' and
        capped['project_roots']['completeness'] == 'complete', capped))
    # Default parser-document limit (4096) must not hide complete filename totals.
    for i in range(300, 4100):
        jput(many, 'p%04d/package.json' % i, dict(name='sample-%d' % i))
    documents = r.run('parser-document-cap', many)['assessment']
    r.check('document cap preserves exact filename total', lambda: expect(
        candidate_counts(documents).get(('npm', 'package.json')), 4100))
    r.check('document cap discloses observed project lower bound', lambda: require(
        0 < count(documents['projects']) < 4100 and
        documents['projects']['completeness'] == 'lower_bound' and
        bool(documents['projects']['reasons']), documents['projects']))
    limited = r.run('content-limit', many, ('--max-file-bytes', '1'))['assessment']
    r.check('content cap preserves filename aggregate', lambda: expect(candidate_counts(limited), candidate_counts(documents)))
    r.check('content cap discloses parser population lower bound', lambda: require(
        limited['projects']['completeness'] == 'lower_bound' and bool(limited['projects']['reasons']), limited['projects']))


def validate_candidate_schema(r, repo):
    result = subprocess.run([str(r.candidate), 'capabilities', '--schema', 'profile', '--json'],
                            capture_output=True, timeout=15)
    (r.output / 'candidate-profile.schema.json').write_bytes(result.stdout)
    (r.output / 'schema-export.stderr').write_bytes(result.stderr)
    assert result.returncode == 0, result.stderr
    exported = json.loads(result.stdout)
    assert exported['$schema'] == 'https://json-schema.org/draft/2020-12/schema'
    overlay = r.output / 'schema-overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(repo / 'schema/assessment_generated_audit_test.go'):
        str(Path(__file__).with_name('schema_test.go.txt').resolve())}}))
    # Resolve the already available toolchain before disabling checksum lookups.
    # The Go launcher cannot verify even a cached auto-toolchain with GOSUMDB=off.
    toolchain = subprocess.run(['go', 'env', 'GOROOT'], cwd=repo,
        env=dict(os.environ, GOPROXY='off'), capture_output=True, timeout=15)
    assert toolchain.returncode == 0, toolchain.stderr.decode(errors='replace')
    go_executable = Path(toolchain.stdout.decode().strip()) / 'bin' / ('go.exe' if os.name == 'nt' else 'go')
    checked = subprocess.run([str(go_executable), 'test', '-overlay', str(overlay), './schema',
                              '-run', '^TestAssessmentGeneratedReports$', '-count=1', '-v'],
                             cwd=repo, env=dict(os.environ, GOMAXPROCS='2', GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local',
                             DIRCUE_ASSESSMENT_REPORTS=str(r.output),
                             DIRCUE_ASSESSMENT_REPORT_COUNT=str(len(r.calls))),
                             capture_output=True, timeout=60)
    (r.output / 'schema-validation.log').write_bytes(checked.stdout + checked.stderr)
    assert checked.returncode == 0, (checked.stdout + checked.stderr).decode(errors='replace')
    return dict(validated_reports=len(r.calls), native_saved_reports_validated=len(r.calls),
                native_aggregate_corruption_guards=sum(1 + sum(bool(a[k]) for k in
                    ('workspace_evidence', 'local_dependency_evidence'))
                    for a in (json.loads(p.read_bytes())['assessment'] for p in
                              r.output.glob('*.stdout.json'))),
                schema_corruption_guards=3*len(r.calls),
                validator_go=str(go_executable),
                candidate_export_sha256=digest(r.output / 'candidate-profile.schema.json'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--seeds', type=int, default=3)
    args = parser.parse_args()
    assert 1 <= args.seeds <= 50
    output = args.output.resolve(); output.mkdir(parents=True, exist_ok=False)
    root = output / 'fixtures'; root.mkdir()
    repo = Path(__file__).resolve().parents[2]
    syft = repo / 'tests/packageevidence/fixtures/syft-1.52.0.json'
    candidate = args.candidate.resolve()
    r = Runner(candidate, output)
    receipt = dict(passed=False, candidate=str(candidate), candidate_sha256=digest(candidate),
                   harness_sha256=digest(Path(__file__)),
                   schema_helper_sha256=digest(Path(__file__).with_name('schema_test.go.txt')),
                   syft_sha256=digest(syft),
                   seeds=list(range(args.seeds)), mr_strength_matrix=[dict(
                       name=n, category=c, sensitivity=f, independence=i, cost=k,
                       score=f*i/k) for n,c,f,i,k in MR_MATRIX],
                   mutation_scope='Output corruption guard probes only; no implementation mutation claim.')
    try:
        for seed in range(args.seeds):
            run_seed(r, root, seed, syft)
        directed(r, root, syft)
        receipt['native_schema_validation'] = validate_candidate_schema(r, repo)
        assert receipt['candidate_sha256'] == digest(candidate), 'candidate changed during run'
        receipt['passed'] = True
    finally:
        receipt.update(calls=r.calls, checks=r.checks, output_corruption_probes=r.probes,
                       os_skips=r.skips,
                       acceptance_checks=len(r.checks), rejected_corruptions=len(r.probes))
        (output / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print('%d acceptance checks; %d/%d output-corruption guards rejected' % (
          len(r.checks), len(r.probes), len(r.probes)))


if __name__ == '__main__':
    main()
