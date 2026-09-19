#!/usr/bin/env python3
"""Manual-oracle feasibility checks. Never executes the inspected repositories."""
import argparse
import hashlib
import json
import pathlib
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[2]
FIXTURES = ROOT / 'prototypes/entrypoints/fixtures'

def digest(data):
    return hashlib.sha256(data).hexdigest()

def run(binary, name, language, data):
    proc = subprocess.run([str(binary)], input=json.dumps({'path': name, 'language': language, 'source': data.decode('utf-8')}).encode(), capture_output=True, timeout=30)
    assert proc.returncode == 0, (name, proc.stderr.decode())
    assert proc.stderr == b'', (name, proc.stderr)
    result = json.loads(proc.stdout)
    assert result['parse_count'] == 1 and result['metrics_computed'] and result['same_tree_after_metrics']
    assert result['source_bytes'] == len(data) and result['path'] == name
    assert result['total_candidates'] == len(result['entries']) + result['omitted_candidates']
    assert len(result['entries']) <= 128
    for index, entry in enumerate(result['entries'], 1):
        assert entry['index'] == index
        for span in [entry['span'], *entry['evidence']]:
            start, end = span['start_byte'], span['end_byte']
            assert 0 <= start < end <= len(data)
            assert span['start_line'] == 1 + data[:start].count(b'\n')
            assert span['end_line'] == 1 + data[:end].count(b'\n')
        assert entry['status'] in ('declared', 'unresolved')
        assert 'runtime-registration-not-evaluated' in entry['reasons']
        if entry['status'] == 'declared':
            assert entry['qualification'] in ('explicit-import', 'explicit-import-alias', 'fully-qualified-symbol', 'same-file-constructor-import')
    # The harness binds this result to the bytes actually submitted to the probe.
    result['source_sha256'] = digest(data)
    result['source_hash_computed_by'] = 'experiment-harness'
    return result

# Manual source expectations; Python qualification was narrowed after the retained adversarial audit.
EXPECTED = {
    'SpringExample.java': [('/api', None, 'declared'), ('/items', 'GET', 'declared'), (None, 'GET', 'unresolved'), ('${configured}/items', 'GET', 'unresolved')],
    'AliasController.cs': [('api/[controller]', None, 'unresolved'), ('items', 'GET', 'declared'), (None, None, 'unresolved'), ('/candidate', None, 'unresolved')],
    'fastapi_alias.py': [('/items', None, 'unresolved'), (None, None, 'unresolved'), ('/detail', None, 'unresolved')],
    'Foreign.java': [('/foreign', None, 'unresolved')],
    'Shadow.java': [('/shadow', None, 'unresolved')],
    'Foreign.cs': [('/foreign', None, 'unresolved')],
    'rebound.py': [('/rebound', None, 'unresolved')],
    'nested.py': [('/nested', None, 'unresolved')],
    'module_alias.py': [('/module-alias', None, 'unresolved')],
    'function_rebound.py': [('/function-rebound', None, 'unresolved')],
    'deleted.py': [('/deleted', None, 'unresolved')],
    'loop_rebound.py': [('/loop', None, 'unresolved')],
    'method_replaced.py': [('/method', None, 'unresolved')],
    'Qualified.java': [('/qualified', 'GET', 'declared'), (None, 'GET', 'unresolved')],
    'Qualified.cs': [('/qualified', 'GET', 'declared'), (None, 'GET', 'unresolved')],
}
SPANS = {
    'SpringExample.java': ['@RequestMapping("/api")', '@GetMapping("/items")', '@GetMapping(PREFIX + "/dynamic")', '@GetMapping("${configured}/items")'],
    'AliasController.cs': ['Route("api/[controller]")', 'Read("items")', 'HttpPost(Prefix + "/dynamic")', 'arbitrary.MapGet("/candidate", () => "ok")'],
    'fastapi_alias.py': ['@app.get("/items")', '@router.post(PREFIX + "/dynamic")', '@router.get("/detail")'],
}
REAL = [
    ('spring-framework', '9e8cea3ef8ae02efb7956b071cd7bbef7c22cb82', 'spring-webmvc/src/test/java/org/springframework/web/servlet/mvc/annotation/CglibProxyControllerTests.java', 'Java', ['/test', None, '/test', '/hotels', '/bookings']),
    ('aspnetcore', '7387de91234d3ef751fa50b3d1bfede4130213ff', 'src/Mvc/test/WebSites/BasicWebSite/Controllers/NonNullableApiController.cs', 'C#', ['api/NonNullable', None]),
    ('aspnetcore', '7387de91234d3ef751fa50b3d1bfede4130213ff', 'src/Http/samples/MinimalSample/Program.cs', 'C#', ['/plaintext', '/', '/outerget', '/innerget', '/', '/json', '/hello/{name}', '/null-result', '/todo/{id}', '/problem/{problemType}', '/todos', '/todos']),
]

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--binary', type=pathlib.Path, required=True)
    p.add_argument('--corpus', type=pathlib.Path, required=True)
    p.add_argument('--output', type=pathlib.Path, required=True)
    args = p.parse_args()
    results, checks = [], []
    for name, expected in EXPECTED.items():
        data = (FIXTURES / name).read_bytes()
        language = {'.java':'Java', '.cs':'C#', '.py':'Python'}[pathlib.Path(name).suffix]
        result = run(args.binary, name, language, data)
        actual = [(e['route_literal'], e['method'], e['status']) for e in result['entries']]
        assert actual == expected, (name, expected, actual)
        assert result['syntax_errors'] is False
        if name in SPANS:
            assert len(SPANS[name]) == len(result['entries'])
            for token, entry in zip(SPANS[name], result['entries']):
                expected_start = data.index(token.encode())
                assert entry['span']['start_byte'] == expected_start
                assert entry['span']['end_byte'] == expected_start + len(token.encode())
            result['independently_expected_spans'] = len(SPANS[name])
        # No matches inside the fixtures' deliberately misleading comments/string examples.
        assert all(e['route_literal'] not in ('/comment', '/string', 'comment', 'string') for e in result['entries'])
        replay = run(args.binary, name, language, data)
        replay.pop('timings_ns'); stable = dict(result); stable.pop('timings_ns'); stable.pop('independently_expected_spans', None)
        assert stable == replay
        results.append(result); checks.append({'case':name, 'expected':expected, 'actual':actual, 'passed':True})
    malformed = b'from fastapi import FastAPI\napp = FastAPI()\n@app.get("/broken")\ndef broken(:\n pass\n'
    recovery = run(args.binary, 'recovery.py', 'Python', malformed)
    assert recovery['syntax_errors'] and recovery['status'] == 'partial'
    assert len(recovery['entries']) == 1 and recovery['entries'][0]['status'] == 'unresolved'
    assert 'syntax-recovery' in recovery['entries'][0]['reasons']
    results.append(recovery)
    many = ('from fastapi import FastAPI\napp = FastAPI()\n' + ''.join(f'@app.get("/route{i}")\ndef f{i}(): pass\n' for i in range(140))).encode()
    capped = run(args.binary, 'cap.py', 'Python', many)
    assert capped['total_candidates'] == 140 and capped['omitted_candidates'] == 12 and capped['status'] == 'partial'
    assert [e['route_literal'] for e in capped['entries']] == [f'/route{i}' for i in range(128)]
    results.append(capped)
    for repo, commit, name, language, expected in REAL:
        data = subprocess.run(['git', '-C', str(args.corpus/repo), 'show', f'{commit}:{name}'], check=True, capture_output=True).stdout
        result = run(args.binary, name, language, data)
        actual = [e['route_literal'] for e in result['entries']]
        assert actual == expected, (repo, name, expected, actual)
        result['source_origin'] = {'repository': f'https://github.com/{"spring-projects/spring-framework" if repo == "spring-framework" else "dotnet/aspnetcore"}', 'commit':commit, 'path':name, 'license':'Apache-2.0' if repo == 'spring-framework' else 'MIT'}
        if repo == 'aspnetcore':
            assert all(e['status'] == 'unresolved' for e in result['entries'])
        results.append(result); checks.append({'case':f'{repo}/{name}', 'expected_routes':expected, 'actual_routes':actual, 'passed':True})
    source = ROOT / 'prototypes/structural/worker/examples/http_declarations.rs'
    assert source.read_text().count('Ast::parse(') == 1
    invalid = [({'path':'../outside', 'language':'Java', 'source':''}, 'path'), ({'path':'x.java','language':'Java','source':'x'*(256*1024+1)}, 'source-size'), ({'path':'x.py','language':'Python','source':'('*300+'x'+')'*300},'depth')]
    for request, name in invalid:
        proc = subprocess.run([str(args.binary)], input=json.dumps(request).encode(),capture_output=True,timeout=30)
        assert proc.returncode != 0 and not proc.stdout, (name, proc.stdout)
        expected_error = b'syntax tree node/depth limit' if name == 'depth' else b'invalid bounded source/path'
        assert expected_error in proc.stderr, (name, proc.stderr)
        checks.append({'case':f'refusal-{name}', 'passed':True})
    receipt = {'experiment':'http-declaration-feasibility', 'binary_sha256':digest(args.binary.read_bytes()), 'probe_source_sha256':digest(source.read_bytes()), 'harness_sha256':digest(pathlib.Path(__file__).read_bytes()), 'lockfile_sha256':digest((ROOT/'prototypes/structural/worker/Cargo.lock').read_bytes()), 'fixture_sha256':{name:digest((FIXTURES/name).read_bytes()) for name in EXPECTED}, 'results':results, 'checks':checks, 'all_passed':True, 'limitations':['No endpoint resolution or runtime execution', 'Timings are incidental diagnostic samples, not quiet-window benchmarks', 'Declared means syntactic evidence, not actual symbol binding or a runtime route', 'All Python receiver-dependent candidates remain unresolved', 'Single-file lexical import/assignment evidence does not establish dependency identity', 'No dependency manifest is required or consulted', 'Unsupported spellings and APIs are outside the experiment coverage']}
    args.output.parent.mkdir(parents=True,exist_ok=True)
    args.output.write_text(json.dumps(receipt,indent=2)+'\n')
    print(f'Passed {len(results)} source cases and {len(invalid)} explicit bounds refusals')

if __name__ == '__main__':
    main()
