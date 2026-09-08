#!/usr/bin/env python3
"""Paired scanner experiment over existing verified fixtures; never build or prepare inputs."""
import argparse
import importlib.util
import json
import math
import os
from pathlib import Path
import platform
import random
import re
import statistics

HERE=Path(__file__).resolve().parent

def imported(name,path):
    spec=importlib.util.spec_from_file_location(name,path);value=importlib.util.module_from_spec(spec);spec.loader.exec_module(value);return value

runner=imported('scanner_process',HERE/'run.py')
evidence=imported('scanner_statistics',HERE.parent/'enry-performance/record_library.py')
TOOLS=('baseline','candidate')
SELECTED=('ripgrep-directory-w1','jq-directory-w1','roslyn-git-w5')


def load(path):return json.loads(path.read_text())


def host_state():
    paths=['/proc/meminfo','/sys/fs/cgroup/cpu.max','/sys/fs/cgroup/memory.max','/sys/fs/cgroup/cpu.stat']
    return {name:Path(name).read_text() if Path(name).exists() else None for name in paths}


def built_sources(receipt):
    # Candidate receipts may inventory the entire committed snapshot, including
    # docs/evidence. Compare the exact root/fork population used by the baseline.
    return {n:h for n,h in receipt['source_files_sha256'].items()
        if n in ('go.mod','go.sum','main.go') or n.startswith(('internal/','pkg/','third_party/go-enry/'))}


def build_flags(receipt):
    argv=receipt['build_command']
    # Absolute compiler and output paths differ; all actual compile flags must match.
    assert argv[1:3]==['test','-c'],'expected direct go test -c build command'
    flags=argv[3:];output=flags.index('-o');flags=flags[:output]+flags[output+2:]
    assert flags[-1]=='./pkg/scanner' and all(f.startswith('-') for f in flags[:-1])
    assert len(flags)==len(set(flags)),'duplicate compile flags are ambiguous'
    return sorted(flags[:-1])+flags[-1:]


def benchmark(stdout):
    lines=[line for line in stdout.splitlines() if re.match(r'^BenchmarkProfileScanner-\d+\s+',line) and 'ns/op' in line]
    assert len(lines)==1,'expected one complete benchmark row'
    fields=lines[0].split();assert fields[1]=='1','one scan per process required'
    assert len(fields[2:])%2==0
    values={fields[i+1]:float(fields[i]) for i in range(2,len(fields),2)}
    assert all(math.isfinite(v) and v>=0 for v in values.values())
    assert {'ns/op','B/op','allocs/op','files/op','language-B/op','warnings/op'}<=set(values)
    return values


def summary(samples):
    fields={'scan_seconds':lambda s:s['scan_elapsed_ns']/1e9,
        'process_seconds':lambda s:s['process_wall_seconds'],
        'cpu_seconds':lambda s:s['resources']['user_seconds']+s['resources']['system_seconds'],
        'rss_kib':lambda s:s['resources']['max_rss_kib'],
        'bytes_per_scan':lambda s:s['benchmark']['B/op'],
        'allocations_per_scan':lambda s:s['benchmark']['allocs/op']}
    return {name:runner.distribution([extract(s) for s in samples]) for name,extract in fields.items()}


def comparisons(paired):
    results={}
    for metric,extract in [('scan',lambda s:s['scan_elapsed_ns']/1e9),('process',lambda s:s['process_wall_seconds']),
                           ('bytes',lambda s:s['benchmark']['B/op']),('allocations',lambda s:s['benchmark']['allocs/op'])]:
        left,right=([extract(s) for s in paired[name]] for name in TOOLS)
        if min(left+right)<=0:
            results[metric]={'ratio':None,'paired_bootstrap_95_ci':None,'reason':'zero metric: no finite ratio'};continue
        ratio=statistics.median(left)/statistics.median(right);interval=evidence.bootstrap(left,right)
        p95left,p95right=(runner.distribution(v)['p95'] for v in (left,right))
        verdict='faster with margin' if metric in ('scan','process') and ratio>=1.1 and interval[0]>1 and p95right<=p95left else 'slower with margin' if metric in ('scan','process') and ratio<=1/1.1 and interval[1]<1 else 'no speed verdict'
        results[metric]={'ratio':ratio,'paired_bootstrap_95_ci':interval,'verdict':verdict,
            'scope':'baseline/candidate ratio of medians; bootstrap resamples matched round indexes'}
    return results


def render(report):
    lines=['# Paired scanner experiment','',
        'Baseline and candidate ran adjacently with reversed tool order each round. Every full result digest matched RC1. Scan, external process and allocation costs have different scopes.','',
        '| Scenario | Tool | Samples | Scan median / p95 (s) | Scan CV | Process median / p95 (s) | Allocated bytes / count per scan | Peak RSS (MiB) |',
        '|---|---|---:|---:|---:|---:|---:|---:|']
    for name,scenario in report['summary'].items():
        for tool,cell in scenario['tools'].items():
            s,p=cell['scan_seconds'],cell['process_seconds']
            lines.append(f"| {name} | {tool} | {s['count']} | {s['median']:.6f} / {s['p95']:.6f} | {s['coefficient_of_variation']:.3f} | {p['median']:.6f} / {p['p95']:.6f} | {cell['bytes_per_scan']['median']:,.0f} / {cell['allocations_per_scan']['median']:,.0f} | {cell['rss_kib']['max']/1024:.1f} |")
    lines+=['','| Scenario | Metric | Baseline/candidate ratio | Paired 95% interval | Verdict |','|---|---|---:|---|---|']
    for name,scenario in report['summary'].items():
        for metric,value in scenario['comparisons'].items():
            if value['ratio'] is None:
                lines.append(f"| {name} | {metric} | unavailable | unavailable | zero denominator |")
            else:
                lo,hi=value['paired_bootstrap_95_ci']
                lines.append(f"| {name} | {metric} | {value['ratio']:.3f} | [{lo:.3f}, {hi:.3f}] | {value['verdict']} |")
    lines+=['',
        'All raw samples, including excluded warmup processes, remain in result.json and their original stdout/fingerprints. P95 uses nearest rank; CV uses population standard deviation. No outlier is removed. Ratios resample matched round indexes and are meaningful only for this workload and measurement window. A speed verdict requires at least a 10% margin, a confidence interval excluding parity and no p95 regression. Raw B/op and allocs/op are Go benchmark counters, distinct from cumulative heap profiles and peak process RSS.','',
        'First-scan excludes Go package initialization; external process timing includes initialization, setup, scanning, fingerprint/output work and shutdown. OS cache, governor/turbo/SMT and host interference are uncontrolled within Docker Desktop. A single paired window does not establish three-window stability or a universal speedup. High CV and duration-floor advisories remain visible in the JSON. Separate memory profiles are never pooled with these timing samples.','']
    return '\n'.join(lines)


def main():
    p=argparse.ArgumentParser(description=__doc__)
    for n in ['baseline-binary','candidate-binary','baseline-build','candidate-build','overlay-receipt',
              'git-root','flat-root','git-receipt','flat-receipt','host-receipt','output']:
        p.add_argument('--'+n,type=Path,required=True)
    p.add_argument('--rc1-small',type=Path,required=True);p.add_argument('--rc1-dotnet',type=Path,required=True)
    p.add_argument('--pins',type=Path,default=HERE.parent/'performance/corpus.json')
    p.add_argument('--stage',choices=['timing','mem','pilot'],default='timing')
    p.add_argument('--timing',type=Path,help='completed paired result.json required for the separate memory stage')
    p.add_argument('--pilot-rounds',type=int,default=5,help='1-10 diagnostic pairs, excluded from accepted timing samples')
    p.add_argument('--scenario',action='append',choices=SELECTED)
    p.add_argument('--small-runs',type=int,default=100);p.add_argument('--roslyn-runs',type=int,default=20)
    p.add_argument('--warmups',type=int,default=3);p.add_argument('--seed',type=int,default=20260907)
    p.add_argument('--timeout',type=float,default=600)
    a=p.parse_args()
    assert platform.system()=='Linux','run in prepared Linux container'
    assert a.small_runs>=100 and a.roslyn_runs>=20 and a.warmups>=3
    assert 1<=a.pilot_rounds<=10
    assert math.isfinite(a.timeout) and a.timeout>0
    for n,v in vars(a).items():
        if isinstance(v,Path):setattr(a,n,v.resolve())
    selected=a.scenario or list(SELECTED);assert len(selected)==len(set(selected))
    assert not a.output.exists(),'fresh output required'
    for root in [a.git_root,a.flat_root]:assert root.is_dir() and not a.output.is_relative_to(root)
    binaries={n:getattr(a,n+'_binary') for n in TOOLS}
    build_paths={n:getattr(a,n+'_build') for n in TOOLS};builds={n:load(v) for n,v in build_paths.items()}
    for n in TOOLS:
        assert runner.sha(binaries[n])==builds[n]['binary_sha256'],'binary/build mismatch: '+n
        assert builds[n].get('source_unchanged_after_build',builds[n].get('source_stable_during_build',False)),'missing source stability evidence'
        assert 'go1.26.6' in builds[n]['binary_modules']
    assert build_flags(builds['baseline'])==build_flags(builds['candidate']),'compile flags differ'
    assert not any(flag.startswith(('-gcflags','-race','-ldflags')) for flag in build_flags(builds['baseline']))
    overlay=load(a.overlay_receipt)
    assert overlay['base_commit']==builds['baseline']['commit']
    assert builds['candidate'].get('base_commit',builds['candidate'].get('commit'))==overlay['base_commit']
    for n in TOOLS:assert overlay[n+'_binary_sha256']==builds[n]['binary_sha256']
    left,right=(built_sources(builds[n]) for n in TOOLS)
    delta={n:{'before':left.get(n),'after':right.get(n)} for n in sorted(set(left)|set(right)) if left.get(n)!=right.get(n)}
    assert delta and delta==overlay['changes'],'source delta differs from explicit overlay receipt'
    correctness=overlay['correctness'];assert correctness['passed']
    proof=(a.overlay_receipt.parent/correctness['receipt']).resolve();assert runner.sha(proof)==correctness['sha256']
    pins={p['name']:p['commit'] for p in load(a.pins)['projects']}
    expected={}
    for path in [a.rc1_small,a.rc1_dotnet]:
        historical=load(path);assert historical['complete']
        for key in ['pins_sha256']:assert historical['identity'][key]==runner.sha(a.pins)
        assert historical['identity']['receipts']['git']['sha256']==runner.sha(a.git_receipt)
        assert historical['identity']['receipts']['flat']['sha256']==runner.sha(a.flat_receipt)
        expected.update(historical['result_digests'])
    receipts={'git':a.git_receipt,'flat':a.flat_receipt,'host':a.host_receipt}
    identity={'binaries':{n:runner.sha(v) for n,v in binaries.items()},'builds':{n:runner.sha(v) for n,v in build_paths.items()},
        'overlay':runner.sha(a.overlay_receipt),'correctness':runner.sha(proof),
        'receipts':{n:runner.sha(v) for n,v in receipts.items()},'pins':runner.sha(a.pins),
        'harness':runner.sha(Path(__file__)),'process_helper':runner.sha(HERE/'run.py'),
        'statistics_helper':runner.sha(HERE.parent/'enry-performance/record_library.py'),
        'git_root':str(a.git_root),'flat_root':str(a.flat_root),'gomaxprocs':5,'include_files':False,
        'runtime':{'GOGC':'100','GOMEMLIMIT':'off','GODEBUG':''},'time_binary_sha256':runner.sha(Path('/usr/bin/time')),
        'platform':{'kernel':platform.release(),'machine':platform.machine(),'cpu_count':os.cpu_count(),
            'cpu_affinity':sorted(os.sched_getaffinity(0)),'python':platform.python_version()}}
    if a.stage=='mem':
        assert a.timing is not None
        timing=load(a.timing);assert timing['complete'] and timing['stage']=='timing' and timing['identity']==identity
        assert set(selected)<=set(timing['scenarios'])
    else:assert a.timing is None
    a.output.mkdir(parents=True)
    report={'complete':False,'stage':a.stage,'identity':identity,'scenarios':selected,'samples':[],
        'build_receipts':builds,'overlay':overlay,'expected_digests':{n:expected[n] for n in selected},
        'methodology':{'warmups':a.warmups if a.stage=='timing' else 0,'small_runs':a.small_runs,'roslyn_runs':a.roslyn_runs,'seed':a.seed,
            'pilot_rounds':a.pilot_rounds if a.stage=='pilot' else 0,
            'pilot_policy':'Diagnostic fresh-process pairs only; choose fixed timing count before timing begins; never pool pilot samples.',
            'paired_bootstrap_seed':73191,'paired_bootstrap_resamples':2000,
            'order':'seeded initial tool order per scenario, reversed each round; scenario order shuffled per round',
            'outliers':'none removed','cache':'uncontrolled; fixture verification and preceding scans can warm cache',
            'profiles':'separate invocation, never pooled with timing','ratio':'baseline/candidate median, paired round resampling',
            'scope':'fresh first Scan after package initialization; external process and allocation metrics retained separately'},
        'timing_sha256':runner.sha(a.timing) if a.timing else None,'started_at_utc':runner.utc_now(),
        'load_average_before':os.getloadavg(),'host_state_before':host_state()}
    runner.write(a.output/'plan.json',report)
    rng=random.Random(a.seed);initial={s:([*TOOLS] if rng.getrandbits(1) else list(reversed(TOOLS))) for s in selected}
    counts={s:(a.roslyn_runs if s=='roslyn-git-w5' else a.small_runs) for s in selected}
    rounds=[('warmup',i) for i in range(a.warmups)]+[('measured',i) for i in range(max(counts.values()))] if a.stage=='timing' else [('mem',0)]
    if a.stage=='pilot':
        counts={s:a.pilot_rounds for s in selected}
        rounds=[('pilot',i) for i in range(a.pilot_rounds)]
    try:
        for phase,index in rounds:
            active=[s for s in selected if phase!='measured' or index<counts[s]];rng.shuffle(active)
            for scenario in active:
                project,source,workers=runner.SCENARIOS[scenario]
                order=initial[scenario] if index%2==0 else list(reversed(initial[scenario]))
                for name in order:
                    output=a.output/f'{phase}-{index:03d}-{scenario}-{name}';output.mkdir()
                    env={k:v for k,v in os.environ.items() if not k.startswith(('DIRCUE_PROFILE_','GO','CGO'))}
                    env.update(GOMAXPROCS='5',GOGC='100',GOMEMLIMIT='off',GODEBUG='')
                    controls={'ROOT':str((a.git_root if source=='git' else a.flat_root)/project),'SOURCE':source,
                        'REVISION':pins[project] if source=='git' else '','INIT':'first-scan','MODE':'languages','WORKERS':str(workers),
                        'MAX_TREE_SIZE':'100000','INCLUDE_FILES':'0','FIXTURE_ID':scenario+'-'+pins[project],
                        'RUN_ID':'scan','OUTPUT_DIR':str(output),'FIXTURE_RECEIPT':str(receipts['git' if source=='git' else 'flat']),
                        'BUILD_RECEIPT':str(build_paths[name]),'HOST_RECEIPT':str(a.host_receipt),
                        'EXPECTED_SHA256':expected[scenario],'OS_CACHE':'uncontrolled; input preparation and preceding invocations can warm filesystem pages',
                        'MUTEX_FRACTION':'0','BLOCK_RATE_NS':'0'}
                    env.update({'DIRCUE_PROFILE_'+k:v for k,v in controls.items()})
                    cmd=[str(binaries[name]),'-test.run=^$','-test.bench=^BenchmarkProfileScanner$','-test.benchtime=1x','-test.count=1','-test.benchmem']
                    if a.stage=='mem':cmd.append('-test.memprofile='+str(output/'mem.pprof'))
                    cmd=['/usr/bin/time','-f','{"wall_seconds":%e,"user_seconds":%U,"system_seconds":%S,"max_rss_kib":%M}','-o',str(output/'time.json'),'--']+cmd
                    start=runner.utc_now();elapsed=runner.execute(cmd,env,output,a.timeout)
                    paths=list(output.glob('scan-*.json'));assert len(paths)==1
                    fingerprint=load(paths[0]);assert fingerprint['result_sha256']==expected[scenario]
                    settings={v['Key']:v['Value'] for v in fingerprint['build_info']['Settings']}
                    assert settings=={'CGO_ENABLED':'0','GOARCH':'arm64','GOOS':'linux','GOARM64':'v8.0','-trimpath':'true','-compiler':'gc','-buildmode':'exe'},'actual binary settings differ from optimized baseline'
                    assert fingerprint['go_version']=='go1.26.6' and fingerprint['gomaxprocs']==5 and fingerprint['iterations']==1
                    values=benchmark((output/'stdout.txt').read_text());assert values['ns/op']==fingerprint['measured_elapsed_ns']
                    resources=load(output/'time.json');assert all(math.isfinite(v) and v>=0 for v in resources.values())
                    sample={'scenario':scenario,'tool':name,'phase':phase,'round':index,'order':order,'command':cmd,
                        'started_at_utc':start,'finished_at_utc':runner.utc_now(),'process_wall_seconds':elapsed,
                        'scan_elapsed_ns':fingerprint['measured_elapsed_ns'],'resources':resources,'benchmark':values,
                        'result_sha256':fingerprint['result_sha256'],'fingerprint':str(paths[0]),'fingerprint_sha256':runner.sha(paths[0]),
                        'stdout_sha256':runner.sha(output/'stdout.txt'),'stderr_sha256':runner.sha(output/'stderr.txt')}
                    if a.stage=='mem':sample['profile_sha256']=runner.sha(output/'mem.pprof')
                    report['samples'].append(sample)
                print(f'{phase} {index+1}: {scenario} pair verified',flush=True)
        if a.stage in ('timing','pilot'):
            report['summary']={}
            for scenario in selected:
                sample_phase='measured' if a.stage=='timing' else 'pilot'
                paired={n:sorted([s for s in report['samples'] if s['scenario']==scenario and s['tool']==n and s['phase']==sample_phase],key=lambda s:s['round']) for n in TOOLS}
                assert all(len(v)==counts[scenario] for v in paired.values())
                report['summary'][scenario]={'tools':{n:summary(v) for n,v in paired.items()},'comparisons':comparisons(paired),
                    'below_10s_scan_advisory':{n:sum(s['scan_elapsed_ns'] for s in v)<10e9 for n,v in paired.items()},
                    'below_10s_process_advisory':{n:sum(s['process_wall_seconds'] for s in v)<10 for n,v in paired.items()}}
                if any(report['summary'][scenario]['below_10s_scan_advisory'].values()):
                    report['summary'][scenario]['comparisons']['scan']['verdict']='insufficient measured duration'
                if any(report['summary'][scenario]['below_10s_process_advisory'].values()):
                    report['summary'][scenario]['comparisons']['process']['verdict']='insufficient measured duration'
                if a.stage=='pilot':
                    for metric in ('scan','process'):report['summary'][scenario]['comparisons'][metric]['verdict']='diagnostic pilot only'
        for n,path in binaries.items():assert runner.sha(path)==identity['binaries'][n]
        for n,path in build_paths.items():assert runner.sha(path)==identity['builds'][n]
        for n,path in receipts.items():assert runner.sha(path)==identity['receipts'][n]
        assert runner.sha(a.overlay_receipt)==identity['overlay'] and runner.sha(proof)==identity['correctness']
        assert runner.sha(Path(__file__))==identity['harness'] and runner.sha(HERE/'run.py')==identity['process_helper']
        report.update(complete=True,finished_at_utc=runner.utc_now(),load_average_after=os.getloadavg(),host_state_after=host_state())
        runner.write(a.output/'result.json',report)
        if a.stage=='timing':
            with (a.output/'README.md').open('x') as stream:stream.write(render(report))
    except BaseException as error:
        report.update(error=str(error),finished_at_utc=runner.utc_now());runner.write(a.output/'partial.json',report);raise

if __name__=='__main__':main()
