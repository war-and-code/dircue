#!/usr/bin/env python3
"""Audit published final evidence offline; capture local evidence only explicitly."""
import argparse, gzip, hashlib, io, json, re
from pathlib import Path, PurePosixPath
import subprocess, tarfile

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[1]
VALIDATION=ROOT/'.cache/v01-final-validation'
PACKAGING=ROOT/'.cache/v01-final-packaging'
FROZEN='f13f06841a49f4c862319feb88bc2a7549cee06b'
PACKAGED='e2f9ae7035e087062afe55c8335e3f43569a1d8c'
CLI='d4c1011d3d615f303cd8d60760c0eeb8d3f234fda74ac2dfd77aa75e4e8ae637'
def digest(data):return hashlib.sha256(data).hexdigest()
def encoded(value):return (json.dumps(value,indent=2,sort_keys=True)+'\n').encode()
def git(*args):return subprocess.check_output(['git',*args],cwd=ROOT)
def source(commit,name):return git('show',commit+':'+name)

def read_archive(path):
    """Read data only: reject ambiguous names and all nonregular members."""
    entries={}
    with tarfile.open(path,'r:gz') as archive:
        for member in archive:
            name=PurePosixPath(member.name)
            if (not member.isfile() or name.is_absolute() or '..' in name.parts
                    or str(name)!=member.name or member.name in entries):
                raise ValueError('invalid or duplicate evidence member: '+member.name)
            stream=archive.extractfile(member)
            if stream is None:raise ValueError('missing regular evidence content: '+member.name)
            content=stream.read()
            if len(content)!=member.size:raise ValueError('truncated evidence member: '+member.name)
            entries[member.name]=content
    return entries

def compare_cli(reference,actual,json_mode):
    if reference['exit_code']!=actual['exit_code']:return False
    if reference['exit_code']!=0:return True  # Diagnostic wording is outside contract.
    if not json_mode:return reference['stdout']==actual['stdout']
    def normalized(raw):
        value=json.loads(raw)
        if isinstance(value,dict):
            for item in value.values():
                if isinstance(item,dict) and 'files' in item:item['files'].sort()
        return json.dumps(value,sort_keys=True,separators=(',',':'))
    return normalized(reference['stdout'])==normalized(actual['stdout'])

def replay_cli(report):
    rows={row['id']:row for row in report['results']}
    assert len(rows)==len(report['results'])==420
    subtree={f'subdirectory/{mode}' for mode in ['json-breakdown','json','text','text-breakdown','short-flags']}
    large={f'single-file-limits-flat/{target}/{mode}' for target in ['over-limit-late-nul.cs','over-limit-nul-edge.cs','over-limit-generated.js'] for mode in ['file-json','file-text','file-strategies']}
    expected=subtree|large|{'symlink/file-json','attrs-quoted/flat-extension'}
    observed=set();passes=0
    for identity,row in rows.items():
        reference,actual=row['reference'],row['actual']
        json_mode=row.get('json',row.get('category')=='flat-extension')
        equal=compare_cli(reference,actual,json_mode)
        if row['status']=='PASS':
            assert equal and identity not in expected,identity
            passes+=1;continue
        assert row['status']=='XFAIL' and not equal and identity in expected,identity
        observed.add(identity)
        if identity in subtree:
            assert reference['exit_code']!=0
            oracle=rows['subdirectory-oracle/'+row['mode']]['reference']
            assert oracle['exit_code']==actual['exit_code']==0 and compare_cli(oracle,actual,json_mode)
        elif identity in large:
            assert reference['exit_code']==0
            oracle=rows['single-file-limits/'+row['target']+'/'+row['mode']]['reference']
            assert oracle['exit_code']==actual['exit_code']==0 and compare_cli(oracle,actual,json_mode)
        elif identity=='attrs-quoted/flat-extension':
            assert reference['exit_code']==0
            oracle=rows['attrs-quoted-portable-patterns/json-breakdown']['reference']
            assert oracle['exit_code']==actual['exit_code']==0 and compare_cli(oracle,actual,True)
        else:
            assert reference['exit_code']==0 and actual['exit_code']==1
            assert actual['stdout']=='' and 'not a regular Git file' in actual['stderr']
    assert passes==404 and observed==expected and len(observed)==16
    return passes,len(observed)

def audit(entries):
    def load(name):return json.loads(entries[name])
    checks=load('validation/state.json');pack=load('packaging/state.json')
    assert checks['plan']['commit']==FROZEN and pack['plan']['commit']==PACKAGED
    assert checks['stages']['archives']['status']=='failed'
    assert 'local module replacement escapes committed source' in entries['validation/logs/archives-1.stderr'].decode()
    assert all(s['status']=='passed' for n,s in checks['stages'].items() if n!='archives')
    assert pack['complete'] and pack['runtime_sources_unchanged']
    assert set(pack['packaging_only_changes'])=={'scripts/release.py','tests/release/test_packaging.py'}
    for name,expected in checks['source_before'].items():
        if name not in pack['packaging_only_changes']:assert pack['source_before'][name]==expected
    assert pack['source_after']==pack['source_before']
    recorder=load('recorder-provenance.json')
    assert recorder['executed_recorder']['path']=='source/recorders/record_final.py'
    assert recorder['prepared_recorder']['path']=='source/prepared/record_final_validation.py'
    assert recorder['prepared_recorder']['executed'] is False
    assert recorder['executed_recorder']['executed'] is True
    for role in ['executed_recorder','prepared_recorder']:
        item=recorder[role];assert digest(entries[item['path']])==item['sha256']
    assert digest(entries['source/runners/final_validation.py'])==checks['plan']['runner_sha256']
    assert digest(entries['source/runners/final_package_continuation.py'])==pack['runner_sha256']
    assert pack['validation_helper_sha256']==digest(entries['source/runners/final_validation.py'])
    assert 'Ran 5 tests' in entries['validation/packaging-fix-tests.stderr'].decode()
    assert entries['validation/packaging-fix-tests.stderr'].decode().rstrip().endswith('OK')
    cli=load('validation/cli.json');samples=load('validation/samples.json')
    assert cli['summary']==dict(total=420,passed=404,failed=0,expected_failures=16)
    exact_passes,exceptions=replay_cli(cli)
    assert samples['summary']['samples']==samples['summary']['auragaze_matches']==samples['summary']['token_sequences_match']==3388
    assert samples['summary']['reference_errors']==0
    for sample in samples['results']:
        assert sample['auragaze']==sample['reference']['language']
        assert sample['tokenizer']['hash']==sample['reference']['token_hash'] and sample['tokenizer']['count']==sample['reference']['token_count']
    public=load('validation/public.json');stress=load('validation/stress.json')
    assert public['passed'] and stress['passed'] and len(public['projects'])==11 and len(stress['cases'])==14
    assert public['candidate_sha256']==stress['candidate_sha256']==CLI
    raw_count=0
    for project in public['projects']:
        assert project['match'];outputs=[]
        for tool,expected in project['output_sha256'].items():
            raw=entries[f"validation/public-details/{project['name']}-{tool}.json"]
            assert digest(raw)==expected;raw_count+=1
            value=json.loads(raw)
            for language in value.values():language['files'].sort()
            outputs.append(value)
        assert outputs[0]==outputs[1]
    extended_count=0
    for case in stress['cases']:
        assert case['match'];outputs=[]
        for result in case['results'].values():
            raw=entries['validation/stress-details/'+result['artifact']]
            assert digest(raw)==result['sha256'];raw_count+=1
            run=json.loads(raw);assert run['exit_code']==0
            value=json.loads(run['stdout'])
            for language in value.values():language['files'].sort()
            outputs.append(value)
        assert all(value==outputs[0] for value in outputs)
        for result in case.get('extended',{}).values():
            run=load('validation/stress-details/'+result['artifact']);assert run['exit_code']==0
            json.loads(run['stdout']);extended_count+=1
    build=load('validation/build-receipt.json')
    assert build['candidate_sha256']==CLI
    runtime={n:h for n,h in build['source_files_sha256'].items() if n in ('go.mod','go.sum','main.go') or n.startswith(('pkg/','internal/','third_party/go-enry/'))}
    for name,expected in runtime.items():assert digest(entries['source/runtime/'+name])==expected
    provenance=load('source/runtime/third_party/go-enry/PROVENANCE.json')
    assert len(provenance['files'])==45
    for name,expected in provenance['files'].items():assert digest(entries['source/runtime/third_party/go-enry/'+name])==expected
    supplement=load('packaging/archive-supplement.json')
    assert supplement['complete'] and supplement['passed'] and len(supplement['archives'])==6
    assert supplement['source_stable_after_supplement'] and all(x['exit_code']==0 for x in supplement['checks'])
    archive=load('packaging/archives/provenance.json');repeat=load('packaging/reproduction/provenance.json')
    assert archive['git_revision']==repeat['git_revision']==PACKAGED
    assert len(archive['archives'])==5 and len(repeat['archives'])==1
    rows={v['name']:v for v in archive['archives']}
    assert rows[repeat['archives'][0]['name']]==repeat['archives'][0]
    assert next(v for v in rows.values() if v['os']=='linux' and v['arch']=='arm64')['binary_sha256']==CLI
    for required in ['validation/root-conformance-readback.json','validation/root-public-readback.json','validation/root-stress-readback.json','packaging/root-archive-readback.json']:
        assert required in entries
    root_cli=load('validation/root-conformance-readback.json')
    assert root_cli['source_commit']==FROZEN
    assert root_cli['cli_sha256']==digest(entries['validation/cli.json'])
    assert root_cli['samples_sha256']==digest(entries['validation/samples.json'])
    assert root_cli['cli_rows_replayed']==420 and root_cli['exact_passes_replayed']==exact_passes and root_cli['documented_exceptions_replayed']==exceptions
    assert root_cli['unique_sample_paths']==len({r['path'] for r in samples['results']})==3388
    for name,count in [('public',11),('stress',14)]:
        review=load('validation/root-'+name+'-readback.json')
        assert review['source_commit']==FROZEN and review['report_sha256']==digest(entries['validation/'+name+'.json'])
        assert review['projects' if name=='public' else 'cases']==count
        assert review['raw_output_hashes_verified']==(22 if name=='public' else 42)
    root_archives=load('packaging/root-archive-readback.json')
    assert root_archives['source_commit']==PACKAGED and root_archives['provenance_sha256']==digest(entries['packaging/archives/provenance.json'])
    assert root_archives['archives_verified']==5
    assert root_archives['linux_arm64_archive_sha256']==repeat['archives'][0]['sha256']
    security=load('validation/security/receipt.json')
    assert security['source']['commit']==PACKAGED and security['source']['clean_before_and_after'] is True
    assert security['source']['tree']==entries['source/packaging-tree.txt'].decode().strip()
    for name in ['go.mod','go.sum']:
        assert security['source'][name.replace('.','_')+'_sha256']==digest(entries['source/runtime/'+name])
    assert security['runner_sha256']==digest(entries['validation/security/run.py'])
    for command in security['commands']:
        assert command['exit_code']==0
        for channel in ['stdout','stderr']:
            assert digest(entries['validation/security/'+command[channel]])==command[channel+'_sha256']
    commands={c['name']:c for c in security['commands']};assert len(commands)==5
    downloaded=load('validation/security/03-download-govulncheck.stdout')
    resolved=load('validation/security/02-resolve-govulncheck.stdout')
    tool=security['govulncheck']
    assert downloaded['Path']==resolved['Path']==tool['module']=='golang.org/x/vuln'
    assert downloaded['Version']==resolved['Version']==tool['resolved_version']=='v1.7.0'
    assert downloaded['Sum']==tool['module_sum']=='h1:4MQBuhmXbz2uepNJrf3v+aaZLGDqw1JluwYboegA1qg='
    assert downloaded['GoModSum']==tool['go_mod_sum']=='h1:Xw7zvU3e1bsCYYBXu+w4wcn2Kgn27f34WBCTw8LL5Us='
    version=entries['validation/security/04-govulncheck-version.stdout'].decode()
    assert f"Scanner: govulncheck@{tool['resolved_version']}" in version
    assert 'Go: go1.26.6' in version and 'DB: '+tool['database'] in version and 'DB updated: '+tool['database_updated'] in version
    assert security['toolchain']['reported']==entries['validation/security/01-go-version.stdout'].decode().strip()=='go version go1.26.6 darwin/arm64'
    scan=entries['validation/security/05-govulncheck-scan.stdout'].decode()
    assert tool['reachable_vulnerabilities']==tool['scan_exit_code']==0
    assert 'Your code is affected by 0 vulnerabilities.' in scan
    assert "This scan also found 3 vulnerabilities in packages you import and 1\nvulnerability in modules you require, but your code doesn't appear to call these\nvulnerabilities." in scan
    advisory_ids=sorted(set(re.findall(r'GO-\d{4}-\d+',scan)))
    assert advisory_ids==tool['reported_advisory_ids']==['GO-2026-5932','GO-2026-6303','GO-2026-6354','GO-2026-6355']
    assert commands['05-govulncheck-scan']['command'][-3:]==['-show','verbose','./...']
    return dict(passed=True,correctness_commit=FROZEN,packaging_commit=PACKAGED,candidate_sha256=CLI,
        cli=cli['summary'],samples=samples['summary'],public_projects=11,stress_cases=14,
        raw_language_outputs_replayed=raw_count,extended_outputs_parsed=extended_count,
        runtime_source_files_verified=len(runtime),archives=5,reproduction_matches=True,
        security_commands_verified=len(security['commands']),security_receipt_sha256=digest(entries['validation/security/receipt.json']),
        security_reachable_vulnerabilities=0,security_unreachable_advisory_ids=advisory_ids,
        initial_packaging_failure_preserved=True,packaging_only_fix_preserved=True,
        recorder_provenance=recorder,
        archived_payloads_note='Distributable archives remain separate; embedded provenance, supplement and independent archive readback identify exact payload bytes and modes.',
        limits=['No new timing samples in this record.','Linux amd64 execution is emulated; Windows and macOS amd64 are compile checked.','Coverage measures statements; no universal compatibility claim.'])

def main():
    if not __debug__:raise SystemExit('Run this evidence auditor without Python -O; assertions must remain enabled.')
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',type=Path,required=True)
    p.add_argument('--execute',action='store_true');p.add_argument('--audit-only',action='store_true');a=p.parse_args()
    out=a.output.resolve()
    if a.audit_only:
        # This branch uses only the three published evidence files. Do not add
        # Git, source-tree, cache, subprocess or network dependencies here.
        entries=read_archive(out/'evidence.tar.gz')
        manifest=json.loads((out/'SHA256SUMS.json').read_text())
        assert manifest['files']=={n:digest(b) for n,b in entries.items()}
        assert manifest['archive_sha256']==digest((out/'evidence.tar.gz').read_bytes())
        result=audit(entries);assert result==json.loads((out/'audit.json').read_text());print(json.dumps(result,indent=2));return
    if not a.execute:raise SystemExit('Capture requires --execute after the evidence gate opens; audit-only needs no execution flag.')
    assert not out.exists(),'output must be fresh'
    entries={}
    def add(name,path):
        assert not path.is_symlink() and path.is_file(),path
        entries[name]=path.read_bytes()
    for prefix,parent in [('validation',VALIDATION),('packaging',PACKAGING)]:
        for path in parent.iterdir():
            if path.is_file() and path.suffix in ('.json','.md','.out','.stdout','.stderr'):
                add(prefix+'/'+path.name,path)
        for folder in ['logs','public-details','stress-details','security']:
            if (parent/folder).exists():
                for path in sorted((parent/folder).iterdir()):
                    if path.is_file():add(prefix+'/'+folder+'/'+path.name,path)
    for folder in ['archives','reproduction']:
        for name in ['provenance.json','SHA256SUMS']:add('packaging/'+folder+'/'+name,PACKAGING/folder/name)
        for row in json.loads((PACKAGING/folder/'provenance.json').read_text())['archives']:
            assert digest((PACKAGING/folder/row['name']).read_bytes())==row['sha256']
    build=json.loads(entries['validation/build-receipt.json'])
    for name in build['source_files_sha256']:
        if name in ('go.mod','go.sum','main.go') or name.startswith(('pkg/','internal/','third_party/go-enry/')):
            entries['source/runtime/'+name]=source(FROZEN,name)
    provenance=source(FROZEN,'third_party/go-enry/PROVENANCE.json')
    entries['source/runtime/third_party/go-enry/PROVENANCE.json']=provenance
    for name in json.loads(provenance)['files']:
        full='third_party/go-enry/'+name
        if 'source/runtime/'+full not in entries:entries['source/runtime/'+full]=source(FROZEN,full)
    for name in ['LICENSE','THIRD_PARTY_NOTICES.md']:
        entries['source/'+name]=source(FROZEN,name)
    for name in ['tests/conformance/run.py','tests/conformance/samples.py','tests/conformance/tokenizer_probe.go.txt','tests/performance/compare.py','tests/performance/corpus.json','tests/stress/compare.py','tests/release/smoke.py']:
        entries['source/harness/'+name]=source(FROZEN,name)
    for name in ['tests/stress/generate.py','tests/stress/pack_views.py']:
        entries['source/harness/'+name]=source(FROZEN,name)
    for name in ['scripts/release.py','tests/release/test_packaging.py']:
        entries['source/packaging/'+name]=source(PACKAGED,name)
    entries['source/packaging-only.patch']=git('diff',FROZEN,PACKAGED,'--','scripts/release.py','tests/release/test_packaging.py')
    entries['source/packaging-tree.txt']=git('rev-parse',PACKAGED+'^{tree}')
    for name in ['final_validation.py','final_package_continuation.py','final_archive_supplement.py']:
        add('source/runners/'+name,ROOT/'.cache'/name)
    for name in ['record_final_validation.py','final-validation-runbook.md']:
        add('source/prepared/'+name,ROOT/'.cache'/name)
    add('source/recorders/record_final.py',Path(__file__).resolve())
    entries['recorder-provenance.json']=encoded({
        'executed_recorder':{'path':'source/recorders/record_final.py','executed':True,
            'sha256':digest(entries['source/recorders/record_final.py']),'role':'This entrypoint captures this bundle.'},
        'prepared_recorder':{'path':'source/prepared/record_final_validation.py','executed':False,
            'sha256':digest(entries['source/prepared/record_final_validation.py']),'role':'Prepared predecessor; did not create an earlier bundle.'},
        'executed_validation_runners':['source/runners/final_validation.py','source/runners/final_package_continuation.py','source/runners/final_archive_supplement.py','validation/security/run.py'],
        'prepared_runbook':'source/prepared/final-validation-runbook.md'})
    result=audit(entries);out.mkdir(parents=True)
    with (out/'evidence.tar.gz').open('wb') as raw:
        with gzip.GzipFile(filename='',mode='wb',fileobj=raw,mtime=0,compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed,mode='w',format=tarfile.PAX_FORMAT) as archive:
                for name,content in sorted(entries.items()):
                    m=tarfile.TarInfo(name);m.size=len(content);m.mode=0o644;m.uid=m.gid=m.mtime=0;m.uname=m.gname=''
                    archive.addfile(m,io.BytesIO(content))
    reread=read_archive(out/'evidence.tar.gz')
    assert reread==entries and audit(reread)==result
    (out/'audit.json').write_bytes(encoded(result))
    (out/'SHA256SUMS.json').write_bytes(encoded({'archive_sha256':digest((out/'evidence.tar.gz').read_bytes()),'files':{n:digest(b) for n,b in entries.items()}}))
    print(json.dumps(result,indent=2))
if __name__=='__main__':main()
