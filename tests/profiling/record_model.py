#!/usr/bin/env python3
"""Record the completed binary centroid model paired experiment. No builds or measurements."""
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
    def tree(folder,prefix):
        for path in folder.rglob('*'):
            if path.is_file():entries[prefix+'/'+path.relative_to(folder).as_posix()]=path.read_bytes()
    tree(exp/'paired-results','paired')
    tree(exp/'paired-inputs','inputs')
    tree(exp/'paired-baseline-0577','normalized')
    for name in ['proof.md','design.md','runtime.patch','updater.patch','encode_centroid_model.py',
                 'classifier-math-identity.json','regeneration-comparison.json','correctness-receipt.json',
                 'live-delta-manifest.json','full-tests.log','updater-tests.log',
                 'paired-pilot.stdout','paired-pilot.stderr','paired-timing.stdout','paired-timing.stderr','paired-mem.stdout','paired-mem.stderr']:
        entries['experiment/'+name]=(exp/name).read_bytes()
    delta=json.loads(entries['experiment/live-delta-manifest.json'])
    for name,row in delta['planned_files'].items():
        if row['operation']!='delete':entries['integration/'+name]=(ROOT/row['source']).read_bytes()
    entries['candidate-build/build-receipt.json']=(exp/'candidate/final-build/build-receipt.json').read_bytes()
    for name in ['host-receipt.json','fixture-roslyn.json','flat-summary.json','fixture-verification.json','scanner-small-baseline-rc1.json','scanner-dotnet-baseline-rc1.json']:
        entries['baseline/'+name]=(base/name).read_bytes()
    before=json.loads(entries['normalized/baseline-build-receipt.json']);after=json.loads(entries['candidate-build/build-receipt.json'])
    for name in before['source_files_sha256']:
        entries['source/base/'+name]=subprocess.run(['git','show',before['commit']+':'+name],cwd=ROOT,stdout=subprocess.PIPE,check=True).stdout
    for name in after['source_files_sha256']:entries['source/candidate/'+name]=(exp/'candidate/build-root'/name).read_bytes()
    for name in ['compare_scanner.py','run.py','record_model.py']:entries['harness/'+name]=(HERE/name).read_bytes()
    entries['harness/record_library.py']=Path(h.__file__).read_bytes()
    entries['accepted-proof.md']=(HERE/'model-format-optimization.md').read_bytes()
    entries['pins.json']=(ROOT/'tests/performance/corpus.json').read_bytes()
    for name in ['receipt.json','samples.json','samples.md','stdout.log','stderr.log','plan.json']:
        entries['samples/'+name]=(exp/'sample-validation'/name).read_bytes()
    for name in json.loads(entries['samples/receipt.json'])['harness_files']:
        entries['samples/harness/'+name]=(exp/'sample-validation/source'/name).read_bytes()
    for path in (exp/'public-correctness').iterdir():
        if path.is_file() and path.name not in ['auragaze','evidence.tar.gz']:
            entries['public/'+path.name]=path.read_bytes()
    tree(exp/'public-correctness/comparison-details','public/comparison-details')
    for folder,receipt in [(ROOT/'.cache/getlines-opt3/build',before),(exp/'candidate/final-build',after)]:
        assert sha((folder/'scanner.test').read_bytes())==receipt['binary_sha256']
    assert sha((exp/'public-correctness/auragaze').read_bytes())==json.loads(entries['public/correctness-receipt.json'])['candidate_sha256']
    entries['binary-capture.json']=encode({n:r['binary_sha256'] for n,r in [('baseline',before),('candidate',after)]})
    return entries


def audit(entries):
    load=lambda n:json.loads(entries[n])
    before,after=load('normalized/baseline-build-receipt.json'),load('candidate-build/build-receipt.json')
    assert before['commit']==after['base_commit']=='0577e7e190dfe65551c9eda0be8fb8922c9de5b1'
    assert before['source_stable_during_build'] and after['source_stable_during_build']
    assert pair.build_flags(before)==pair.build_flags(after)
    normalization=before['normalization']
    assert normalization['reused_binary'] and not normalization['new_build_performed']
    assert normalization['original_receipt_sha256']==sha(entries['normalized/original-baseline-build-receipt.json'])
    assert normalization['normalizer_sha256']==sha(entries['normalized/normalizer.py'])
    original=load('normalized/original-baseline-build-receipt.json')
    assert pair.built_sources(original)==pair.built_sources(before)
    for side,receipt in [('base',before),('candidate',after)]:
        for name,digest in receipt['source_files_sha256'].items():assert sha(entries['source/'+side+'/'+name])==digest
    left,right=pair.built_sources(before),pair.built_sources(after)
    delta={n:{'before':left.get(n),'after':right.get(n)} for n in sorted(set(left)|set(right)) if left.get(n)!=right.get(n)}
    overlay=load('inputs/overlay-receipt.json');assert delta==overlay['changes']
    combined=load('inputs/correctness-receipt.json');assert combined['complete'] and combined['passed']
    assert overlay['correctness']['sha256']==sha(entries['inputs/correctness-receipt.json'])
    assert combined['candidate_build_receipt_sha256']==sha(entries['candidate-build/build-receipt.json'])
    assert combined['candidate_source_files']==after['source_files_sha256']
    for kind,key in [('sample_gate','samples/receipt.json'),('public_gate','public/correctness-receipt.json')]:
        assert combined[kind]['sha256']==sha(entries[key])
    sample=load('samples/receipt.json');samples=load('samples/samples.json')
    assert sample['complete'] and sample['exit_code']==0 and sample['source_unchanged']
    assert sample['candidate_source_files']==after['source_files_sha256']
    assert sample['samples_sha256']==sha(entries['samples/samples.json'])
    for name,digest in sample['harness_files'].items():assert sha(entries['samples/harness/'+name])==digest
    for name in ['stdout','stderr']:assert sample[name+'_sha256']==sha(entries['samples/'+name+'.log'])
    rows=samples['results'];assert len(rows)==3388 and len({r['path'] for r in rows})==3388
    assert all(r['auragaze']==r['reference']['language'] and r['tokenizer']['hash']==r['reference']['token_hash'] and r['tokenizer']['count']==r['reference']['token_count'] for r in rows)
    assert sum(r['enry']==r['reference']['language'] for r in rows)==3241
    proof=load('public/correctness-receipt.json');public=load('public/comparison.json')
    assert proof['complete'] and proof['passed'] and proof['projects']==11 and proof['raw_outputs_verified']==22
    assert proof['build_receipt_sha256']==sha(entries['public/build-receipt.json'])
    assert load('public/build-receipt.json')['source_files_sha256']==after['source_files_sha256']
    assert proof['report_sha256']==sha(entries['public/comparison.json']) and public['passed']
    assert len(public['projects'])==11 and all(r['match'] for r in public['projects'])
    for name,digest in load('public/SHA256SUMS.json').items():
        if name not in ('evidence.tar.gz','auragaze'):assert sha(entries['public/'+name])==digest
        if name=='auragaze':assert digest==proof['candidate_sha256']
    integration=load('experiment/live-delta-manifest.json')
    for name,row in integration['planned_files'].items():
        if row['operation']!='delete':assert sha(entries['integration/'+name])==row['sha256']
    provenance=load('source/candidate/third_party/go-enry/PROVENANCE.json')
    for name,digest in provenance['files'].items():assert sha(entries['source/candidate/third_party/go-enry/'+name])==digest
    assert provenance['patch_sha256']==sha(entries['integration/third_party/patches/enry-linguist-9.7.patch'])
    assert provenance['generator_script_sha256']==sha(entries['integration/third_party/update_enry.py'])
    assert provenance['linguist_centroid_binary_sha256']==sha(entries['source/candidate/third_party/go-enry/data/centroid_model.bin'])
    for row in public['projects']:
        outputs={}
        for tool in ['auragaze','linguist']:
            name='public/comparison-details/'+row['name']+'-'+tool+'.json'
            assert sha(entries[name])==row['output_sha256'][tool]
            outputs[tool]=load(name)
            for lang in outputs[tool].values():lang['files']=sorted(lang['files'])
        assert outputs['auragaze']==outputs['linguist']
    math=load('experiment/classifier-math-identity.json')
    marker=b'type centroidClassifier struct{}'
    assert entries['source/base/third_party/go-enry/centroid.go'].split(marker,1)[1]==entries['source/candidate/third_party/go-enry/centroid.go'].split(marker,1)[1]
    def artifact(path):
        if path.startswith('/results/'):return 'paired/'+path.removeprefix('/results/')
        if path.startswith('/fixture-meta/'):return 'baseline/'+path.removeprefix('/fixture-meta/')
        if path.startswith('/baseline-normalized/'):return 'normalized/'+path.removeprefix('/baseline-normalized/')
        if path.startswith('/experiment/candidate/final-build/'):return 'candidate-build/'+path.removeprefix('/experiment/candidate/final-build/')
        raise AssertionError(path)
    report=load('paired/timing/result.json');profiles=load('paired/mem/result.json');pilot=load('paired/pilot/result.json')
    assert pilot['complete'] and pilot['identity']==report['identity']
    decision=load('inputs/sampling-decision.json')
    assert decision['decision_before_accepted_timing'] and decision['pilot_sha256']==sha(entries['paired/pilot/result.json'])
    assert decision['small_pairs']==500 and decision['roslyn_pairs']==20
    assert pilot['finished_at_utc'] < report['started_at_utc'] < profiles['started_at_utc']
    assert report['complete'] and profiles['complete'] and report['identity']==profiles['identity']
    assert profiles['timing_sha256']==sha(entries['paired/timing/result.json'])
    identity=report['identity']
    for key,file in [('harness','compare_scanner.py'),('process_helper','run.py'),('statistics_helper','record_library.py')]:assert identity[key]==sha(entries['harness/'+file])
    assert identity['pins']==sha(entries['pins.json'])
    assert identity['overlay']==sha(entries['inputs/overlay-receipt.json'])
    assert identity['correctness']==sha(entries['inputs/correctness-receipt.json'])
    for key,name in [('git','fixture-roslyn.json'),('flat','flat-summary.json'),('host','host-receipt.json')]:assert identity['receipts'][key]==sha(entries['baseline/'+name])
    for name,receipt in [('baseline',before),('candidate',after)]:
        assert identity['binaries'][name]==receipt['binary_sha256']==load('binary-capture.json')[name]
        assert identity['builds'][name]==sha(entries['normalized/baseline-build-receipt.json' if name=='baseline' else 'candidate-build/build-receipt.json'])
    historical={}
    for group in ['small','dotnet']:historical.update(load(f'baseline/scanner-{group}-baseline-rc1.json')['result_digests'])
    counts={};samples=report['samples']+profiles['samples']+pilot['samples']
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
    assert counts=={'warmup':18,'measured':2040,'mem':2,'pilot':20}
    for scenario,value in report['summary'].items():
        paired={n:sorted([s for s in report['samples'] if s['scenario']==scenario and s['tool']==n and s['phase']=='measured'],key=lambda s:s['round']) for n in pair.TOOLS}
        for name,values in paired.items():
            assert len(values)==(20 if scenario=='roslyn-git-w5' else 500)
            assert [v['round'] for v in values]==list(range(len(values)))
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
            relative=Path(row[field]).relative_to('.cache/centroid-wire-opt4/paired-results').as_posix();assert sha(entries['paired/'+relative])==row[digest]
        assert row['binary_sha256'] in identity['binaries'].values()
        stdout='paired/'+Path(row['stdout']).relative_to('.cache/centroid-wire-opt4/paired-results').as_posix()
        assert sha(entries[stdout.removesuffix('.txt')+'.stderr'])==row['stderr_sha256']
    return {'complete':True,'source_base_commit':before['commit'],'candidate_is_clean_commit':False,'counts':counts,
        'baseline_binary_sha256':before['binary_sha256'],'candidate_binary_sha256':after['binary_sha256'],
        'source_files_verified':len(before['source_files_sha256']),'overlay_paths':sorted(delta),
        'actual_ruby_repositories':11,'actual_ruby_samples_and_tokens':3388,'official_enry_matches':3241,'pilot_never_pooled':True,'normalized_baseline_reused_without_rebuild':True,'all_rc1_full_digests_match':True,'all_distributions_ratios_intervals_recomputed':True,
        'all_benchmark_allocation_rows_reparsed':True,'all_profile_hashes_verified':True,'summary':report['summary']}


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--audit-only',action='store_true');p.add_argument('--output',type=Path,default=HERE/'results/centroid-wire-opt4');a=p.parse_args()
    if a.audit_only:
        hashes=load_hashes=json.loads((a.output/'SHA256SUMS.json').read_text())
        for n,d in hashes.items():assert sha((a.output/n).read_bytes())==d
        entries=h.unarchive((a.output/'evidence.tar.gz').read_bytes())
        for n,d in json.loads(entries['manifest.json']).items():assert sha(entries[n])==d
    else:
        assert not a.output.exists();entries=capture(ROOT/'.cache/centroid-wire-opt4',ROOT/'.cache/scanner-profiles-opt2')
    result=audit(entries)
    if a.audit_only:
        assert encode(result)==(a.output/'audit.json').read_bytes();print('PASS: opt4 archive and paired metrics replayed.');return
    readme=entries['paired/timing/README.md']+b"\nAccepted for the scoped small-repository startup and allocation improvements. Roslyn timing remains inconclusive and peak RSS is effectively unchanged. Separate sampled loadCentroids allocation decreases from 25.15 MB to 3.02 MB; the total sampled profile is nearly unchanged. The stripped CLI grows by 524,288 bytes (512 KiB). These results are a paired model-format comparison, not an upstream Enry CLI or final release acceptance claim. See [model-format-optimization.md](../../model-format-optimization.md).\n\nReplay without workloads: `python3 tests/profiling/record_model.py --audit-only`. The deterministic archive retains source, model, updater, original/normalized build receipts, all raw samples, profiles and correctness outputs. No binaries or corpus checkouts are embedded.\n"
    gate={'scope':'binary centroid model; small-repository startup improvement, Roslyn timing inconclusive','questions':{
        '1_workload':'pass: pinned Roslyn Git and jq/ripgrep raw-blob directories',
        '2_baseline':'pass: accepted opt3 byte-identical reused binary, original and normalization receipts preserved',
        '3_correctness':'pass: 3388 actual Ruby labels/tokens, 11 actual Ruby repositories and every full RC1 digest',
        '4_source':'pass: committed base plus complete isolated candidate inventory and explicit six-path source delta',
        '5_build':'pass: same exact compiler, flags, CGO and actual binary settings',
        '6_environment':'waive: Docker Desktop, uncontrolled governor/turbo/SMT and host interference',
        '7_samples':'pass: fixed500/500/20 pairs, 3 warmups each, both duration floors met; pilot excluded',
        '8_distribution':'pass: no samples removed; median,p95,CV, CPU,RSS,allocations and intervals recomputed',
        '9_order':'pass: seeded initial order, alternated every round, adjacent tool pairs',
        '10_profiler':'pass: separate matching-symbol memory profiles, never pooled with timings',
        '11_three_tiers':'qualified: small scan/process and RSS improve; Roslyn timing inconclusive and RSS unchanged',
        '12_cost_scope':'pass: 512KiB stripped CLI growth; B/op, sampled cumulative allocation and peakRSS distinct',
        '13_repetition':'waive: one paired window, no three independent windows',
        '14_reproduction':'qualified: source/receipts/raw outputs retained; final integrated library/CLI/release gates pending'}}
    entries['honest-gate.json']=encode(gate)
    entries['manifest.json']=encode({n:sha(b) for n,b in sorted(entries.items())})
    archive=h.archive(entries);assert h.unarchive(archive)==entries
    a.output.mkdir(parents=True)
    for name,data in {'README.md':readme,'audit.json':encode(result),'honest-gate.json':encode(gate),'evidence.tar.gz':archive}.items():(a.output/name).write_bytes(data)
    (a.output/'SHA256SUMS.json').write_bytes(encode({p.name:sha(p.read_bytes()) for p in a.output.iterdir()}))
    print('PASS: opt4 archive captured; SHA256 '+sha(archive))

if __name__=='__main__':main()
