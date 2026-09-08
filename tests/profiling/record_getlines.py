#!/usr/bin/env python3
"""Record the completed getLines paired experiment. No builds or measurements."""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[1]

def imported(name,path):
    s=importlib.util.spec_from_file_location(name,path);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m

pair=imported('paired_scanner',HERE/'compare_scanner.py')
h=pair.evidence
sha,encode=h.sha,h.encode


def capture(exp,base):
    entries={}
    for path in (exp/'paired-results').rglob('*'):
        if path.is_file():entries['paired/'+path.relative_to(exp/'paired-results').as_posix()]=path.read_bytes()
    for path in (exp/'paired-inputs').iterdir():
        if path.is_file():entries['inputs/'+path.name]=path.read_bytes()
    for name in ['proof.md','getlines.patch','experiment-receipt.json','validation-receipt.json','focused-tests.log','maintained-tests.log',
                 'public-correctness.json','public-correctness-receipt.json','public-correctness-invocation.json','paired-timing.stdout','paired-timing.stderr','paired-mem.stdout','paired-mem.stderr']:
        entries['experiment/'+name]=(exp/name).read_bytes()
    for path in (exp/'public-correctness-details').glob('*.json'):entries['experiment/public-correctness-details/'+path.name]=path.read_bytes()
    for path in (exp/'build').iterdir():
        if path.is_file() and path.suffix in ('.json','.log','.py'):entries['candidate-build/'+path.name]=path.read_bytes()
    for name in ['build-receipt.json','host-receipt.json','fixture-roslyn.json','flat-summary.json','fixture-verification.json','scanner-small-baseline-rc1.json','scanner-dotnet-baseline-rc1.json']:
        entries['baseline/'+name]=(base/name).read_bytes()
    before=json.loads(entries['baseline/build-receipt.json']);after=json.loads(entries['candidate-build/build-receipt.json'])
    for name in before['source_files_sha256']:
        entries['source/base/'+name]=subprocess.run(['git','show',before['commit']+':'+name],cwd=ROOT,stdout=subprocess.PIPE,check=True).stdout
    for name in after['overlay']['files']:
        entries['source/candidate-overlay/'+name]=(exp/'application'/name).read_bytes()
    for name in ['compare_scanner.py','run.py','record_getlines.py']:entries['harness/'+name]=(HERE/name).read_bytes()
    entries['harness/record_library.py']=Path(h.__file__).read_bytes()
    entries['accepted-proof.md']=(HERE/'getlines-optimization.md').read_bytes()
    entries['pins.json']=(ROOT/'tests/performance/corpus.json').read_bytes()
    for name,folder,receipt in [('baseline',base,before),('candidate',exp/'build',after)]:
        assert sha((folder/'scanner.test').read_bytes())==receipt['binary_sha256']
    entries['binary-capture.json']=encode({n:r['binary_sha256'] for n,r in [('baseline',before),('candidate',after)]})
    return entries


def audit(entries):
    load=lambda n:json.loads(entries[n])
    before,after=load('baseline/build-receipt.json'),load('candidate-build/build-receipt.json')
    assert before['commit']==after['base_commit']=='390ce5fbcb605eec72af524368d03bdf53715b24'
    assert before['source_unchanged_after_build'] and after['source_stable_during_build']
    assert pair.build_flags(before)==pair.build_flags(after)
    for name,digest in before['source_files_sha256'].items():assert sha(entries['source/base/'+name])==digest
    for name,value in after['overlay']['files'].items():
        assert sha(entries['source/candidate-overlay/'+name])==value['after_sha256']
        assert after['source_files_sha256'][name]==value['after_sha256']
    left,right=pair.built_sources(before),pair.built_sources(after)
    delta={n:{'before':left.get(n),'after':right.get(n)} for n in sorted(set(left)|set(right)) if left.get(n)!=right.get(n)}
    overlay=load('inputs/overlay-receipt.json');assert delta==overlay['changes']
    proof=load('experiment/public-correctness-receipt.json')
    assert proof['complete'] and len(proof['projects'])==11 and all(p['match'] for p in proof['projects'])
    assert proof['build_receipt_sha256']==sha(entries['candidate-build/build-receipt.json'])
    assert proof['report_sha256']==sha(entries['experiment/public-correctness.json'])
    assert overlay['correctness']['sha256']==sha(entries['experiment/public-correctness-receipt.json'])
    for name,digest in proof['output_files'].items():assert sha(entries['experiment/'+name])==digest
    def artifact(path):
        if path.startswith('/results/'):return 'paired/'+path.removeprefix('/results/')
        if path.startswith('/baseline/'):return 'baseline/'+path.removeprefix('/baseline/')
        if path.startswith('/experiment/build/'):return 'candidate-build/'+path.removeprefix('/experiment/build/')
        raise AssertionError(path)
    report=load('paired/timing/result.json');profiles=load('paired/mem/result.json')
    assert report['complete'] and profiles['complete'] and report['identity']==profiles['identity']
    assert profiles['timing_sha256']==sha(entries['paired/timing/result.json'])
    identity=report['identity']
    for key,file in [('harness','compare_scanner.py'),('process_helper','run.py'),('statistics_helper','record_library.py')]:assert identity[key]==sha(entries['harness/'+file])
    assert identity['pins']==sha(entries['pins.json'])
    assert identity['overlay']==sha(entries['inputs/overlay-receipt.json'])
    for name,receipt in [('baseline',before),('candidate',after)]:
        assert identity['binaries'][name]==receipt['binary_sha256']==load('binary-capture.json')[name]
        assert identity['builds'][name]==sha(entries['baseline/build-receipt.json' if name=='baseline' else 'candidate-build/build-receipt.json'])
    historical={}
    for group in ['small','dotnet']:historical.update(load(f'baseline/scanner-{group}-baseline-rc1.json')['result_digests'])
    counts={};samples=report['samples']+profiles['samples']
    for sample in samples:
        key=sample['phase'];counts[key]=counts.get(key,0)+1
        assert sample['result_sha256']==historical[sample['scenario']]
        fp=artifact(sample['fingerprint']);assert sha(entries[fp])==sample['fingerprint_sha256']
        fingerprint=load(fp);assert fingerprint['result_sha256']==sample['result_sha256'] and fingerprint['measured_elapsed_ns']==sample['scan_elapsed_ns']
        parent=fp.rsplit('/',1)[0]
        for name in ['stdout','stderr']:assert sha(entries[parent+'/'+name+'.txt'])==sample[name+'_sha256']
        assert pair.benchmark(entries[parent+'/stdout.txt'].decode())==sample['benchmark']
        assert load(parent+'/time.json')==sample['resources']
        for name,value in fingerprint['receipts'].items():assert sha(entries[artifact(value['path'])])==value['sha256']
        if sample['phase']=='mem':assert sha(entries[parent+'/mem.pprof'])==sample['profile_sha256']
    assert counts=={'warmup':18,'measured':440,'mem':2}
    for scenario,value in report['summary'].items():
        paired={n:sorted([s for s in report['samples'] if s['scenario']==scenario and s['tool']==n and s['phase']=='measured'],key=lambda s:s['round']) for n in pair.TOOLS}
        for name,values in paired.items():
            computed=pair.summary(values)
            for metric,dist in computed.items():
                for k,v in dist.items():h.equal(v,value['tools'][name][metric][k],metric+' '+k)
        computed=pair.comparisons(paired)
        for metric,comparison in computed.items():
            for k,v in comparison.items():
                actual=value['comparisons'][metric][k]
                if isinstance(v,float):h.equal(v,actual,'ratio')
                else:assert actual==v
        assert not any(value['below_10s_scan_advisory'].values()) and not any(value['below_10s_process_advisory'].values())
    assert pair.render(report).encode()==entries['paired/timing/README.md']
    top_receipt=load('paired/tops/receipt.json')
    for row in top_receipt['outputs']:
        for field,digest in [('profile','profile_sha256'),('stdout','stdout_sha256')]:
            relative=Path(row[field]).relative_to('.cache/getlines-opt3/paired-results').as_posix();assert sha(entries['paired/'+relative])==row[digest]
        assert row['binary_sha256'] in identity['binaries'].values()
    return {'complete':True,'source_base_commit':before['commit'],'candidate_is_clean_commit':False,'counts':counts,
        'baseline_binary_sha256':before['binary_sha256'],'candidate_binary_sha256':after['binary_sha256'],
        'source_files_verified':len(before['source_files_sha256']),'overlay_paths':sorted(after['overlay']['files']),
        'actual_ruby_repositories':11,'all_rc1_full_digests_match':True,'all_distributions_ratios_intervals_recomputed':True,
        'all_benchmark_allocation_rows_reparsed':True,'all_profile_hashes_verified':True,'summary':report['summary']}


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--audit-only',action='store_true');p.add_argument('--output',type=Path,default=HERE/'results/getlines-opt3');a=p.parse_args()
    if a.audit_only:
        hashes=load_hashes=json.loads((a.output/'SHA256SUMS.json').read_text())
        for n,d in hashes.items():assert sha((a.output/n).read_bytes())==d
        entries=h.unarchive((a.output/'evidence.tar.gz').read_bytes())
        for n,d in json.loads(entries['manifest.json']).items():assert sha(entries[n])==d
    else:
        assert not a.output.exists();entries=capture(ROOT/'.cache/getlines-opt3',ROOT/'.cache/scanner-profiles-opt2')
    result=audit(entries)
    if a.audit_only:
        assert encode(result)==(a.output/'audit.json').read_bytes();print('PASS: opt3 archive and paired metrics replayed.');return
    readme=entries['paired/timing/README.md']+b'\nThis experiment is accepted for reduced allocation volume, with no headline speed claim. Separate Roslyn allocation profiles attribute 297.19 MB to getLines in the baseline and 2.50 MB in the candidate (pprof display units). These sampled cumulative values are separate from benchmark B/op and peak RSS. The original isolated proof is retained as historical evidence; the current accepted proof is [getlines-optimization.md](../../getlines-optimization.md).\n'
    gate={'scope':'getLines allocation experiment; no headline speed claim','questions':{
        '1_workload':'pass: pinned Roslyn Git and jq/ripgrep raw-blob directories',
        '2_baseline':'pass: immutable opt2 scanner paired with isolated candidate',
        '3_correctness':'pass: frozen-helper tests, 11 actual Ruby repositories and every full RC1 digest',
        '4_source':'pass: committed base plus four hash-verified overlay files',
        '5_build':'pass: same exact compiler, flags, CGO and actual binary settings',
        '6_environment':'waive: Docker Desktop, uncontrolled governor/turbo/SMT and host interference',
        '7_samples':'pass: 100/100/20 pairs, 3 warmups each, both duration floors met',
        '8_distribution':'pass: all samples retained; median,p95,CV and intervals independently recomputed',
        '9_order':'pass: seeded initial order, alternated every round, adjacent tool pairs',
        '10_profiler':'pass: separate matching-symbol memory profiles, never pooled with timings',
        '11_three_tiers':'qualified: allocation-volume win; timing below10percentmargin; peakRSS unchanged',
        '12_cost_scope':'pass: B/op, allocation count, sampled cumulative allocation and peakRSS distinct',
        '13_repetition':'waive: one paired window, no three independent windows',
        '14_reproduction':'qualified: retained source/receipts/outputs; same fixtures and Docker environment required'}}
    entries['honest-gate.json']=encode(gate)
    entries['manifest.json']=encode({n:sha(b) for n,b in sorted(entries.items())})
    archive=h.archive(entries);assert h.unarchive(archive)==entries
    a.output.mkdir(parents=True)
    for name,data in {'README.md':readme,'audit.json':encode(result),'honest-gate.json':encode(gate),'evidence.tar.gz':archive}.items():(a.output/name).write_bytes(data)
    (a.output/'SHA256SUMS.json').write_bytes(encode({p.name:sha(p.read_bytes()) for p in a.output.iterdir()}))
    print('PASS: opt3 archive captured; SHA256 '+sha(archive))

if __name__=='__main__':main()
