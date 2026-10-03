#!/usr/bin/env python3
import hashlib, json, os, pathlib, platform, statistics, subprocess, sys, time

root = pathlib.Path.cwd()
outdir = root / '.cache/v110-validation/perf-run-final'
outdir.mkdir(parents=True, exist_ok=True)
bins = {
    'released_v101': root / '.cache/v110-validation/released-v101/dircue',
    'candidate_110': root / '.cache/v110-validation/candidate',
}
workloads = [
    ('spring_languages', ['analyze','languages','--source','directory','--max-file-bytes','0','--json',str(root/'.cache/corpus/spring-petclinic')], root/'.cache/corpus/spring-petclinic', True),
    ('dotnet_http_languages', ['analyze','languages','--source','directory','--max-file-bytes','0','--json',str(root/'.cache/corpus/aspnetcore/src/Http')], root/'.cache/corpus/aspnetcore/src/Http', True),
    ('xml128_languages', ['analyze','languages','--source','directory','--max-file-bytes','0','--json',str(root/'.cache/v110-validation/workloads/xml128')], root/'.cache/v110-validation/workloads/xml128', True),
    ('spring_map', ['map','--source','directory','--json',str(root/'.cache/corpus/spring-petclinic')], root/'.cache/corpus/spring-petclinic', False),
    ('ruff_map', ['map','--source','directory','--json',str(root/'.cache/v110-validation/golden-repos/ruff')], root/'.cache/v110-validation/golden-repos/ruff', False),
]

def sha_file(p):
    h=hashlib.sha256()
    with open(p,'rb') as f:
        for block in iter(lambda:f.read(1<<20),b''): h.update(block)
    return h.hexdigest()

def tree_hash(base):
    h=hashlib.sha256()
    files=[]
    for path in base.rglob('*'):
        if '.git' in path.parts: continue
        try:
            if path.is_file() and not path.is_symlink(): files.append(path)
        except OSError: pass
    files.sort(key=lambda p:p.relative_to(base).as_posix())
    for p in files:
        rel=p.relative_to(base).as_posix().encode()
        h.update(len(rel).to_bytes(8,'big')); h.update(rel)
        h.update(p.stat().st_size.to_bytes(8,'big'))
        h.update(bytes.fromhex(sha_file(p)))
    return {'file_count':len(files),'sha256':h.hexdigest()}

def run(label, which, args, phase, idx):
    stem=f'{label}-{phase}-{idx:02d}-{which}'
    stdout=outdir/(stem+'.stdout')
    stderr=outdir/(stem+'.stderr')
    timelog=outdir/(stem+'.time')
    cmd=['/usr/bin/time','-l','-o',str(timelog),str(bins[which]),*args]
    start=time.perf_counter()
    with stdout.open('wb') as so, stderr.open('wb') as se:
        proc=subprocess.run(cmd,stdout=so,stderr=se,cwd=root)
    elapsed=time.perf_counter()-start
    tb=timelog.read_text(errors='replace') if timelog.exists() else ''
    rss=None
    for line in tb.splitlines():
        if 'maximum resident set size' in line:
            try: rss=int(line.strip().split()[0])
            except (ValueError,IndexError): pass
    return {'workload':label,'phase':phase,'index':idx,'binary':which,'argv':[str(bins[which]),*args], 'status':proc.returncode,'wall_seconds':elapsed,'max_rss_bytes':rss,'stdout_sha256':sha_file(stdout),'stdout_bytes':stdout.stat().st_size,'stderr_sha256':sha_file(stderr),'stderr_bytes':stderr.stat().st_size,'time_log':str(timelog.relative_to(root))}

src_rev=subprocess.run(['git','rev-parse','HEAD'],cwd=root,check=True,capture_output=True,text=True).stdout.strip()
receipt=json.loads((root/'.cache/v110-validation/final-build.json').read_text())
report={'kind':'dircue-controlled-performance-comparison','method':'Warm both executables per workload; five interleaved AB/BA pairs; macOS /usr/bin/time -l per process; language stdout equivalence required before measuring; timings are local workload evidence only.','source_revision':src_rev,'candidate_binary_sha256':sha_file(bins['candidate_110']),'released_v101_binary_sha256':sha_file(bins['released_v101']),'candidate_build_receipt':receipt,'host':{'platform':platform.platform(),'machine':platform.machine(),'python':sys.version.split()[0]},'workloads':[],'runs':[]}
for name,args,base,legacy in workloads:
    gitrev=subprocess.run(['git','-C',str(base),'rev-parse','HEAD'],capture_output=True,text=True)
    row={'name':name,'argv_args':args,'path':str(base.relative_to(root)) if base.is_relative_to(root) else str(base),'tree':tree_hash(base),'git_revision':gitrev.stdout.strip() if gitrev.returncode==0 else None,'git_status':gitrev.returncode==0}
    report['workloads'].append(row)
    # Warm once in fixed A/B order, and keep outputs for exact legacy comparison.
    warm=[]
    for which in ('released_v101','candidate_110'):
        r=run(name,which,args,'warm',0); report['runs'].append(r); warm.append(r)
        if r['status']!=0:
            raise SystemExit(f'warm run failed: {r}')
    if legacy:
        a=outdir/f'{name}-warm-00-released_v101.stdout'
        b=outdir/f'{name}-warm-00-candidate_110.stdout'
        if a.read_bytes()!=b.read_bytes():
            raise SystemExit(f'exact language JSON output differs: {name}; baseline={warm[0]["stdout_sha256"]} candidate={warm[1]["stdout_sha256"]}')
        row['exact_legacy_stdout_equal']=True
        row['legacy_stdout_sha256']=warm[0]['stdout_sha256']
    else:
        row['exact_legacy_stdout_equal']=None
    for pair in range(5):
        order=('released_v101','candidate_110') if pair%2==0 else ('candidate_110','released_v101')
        for which in order:
            r=run(name,which,args,'measure',pair); report['runs'].append(r)
            if r['status']!=0:
                raise SystemExit(f'measured run failed: {r}')
    print(f'finished {name}',flush=True)

summary={}
for w in report['workloads']:
    label=w['name']; summary[label]={}
    for which in bins:
        rs=[r for r in report['runs'] if r['workload']==label and r['binary']==which and r['phase']=='measure']
        walls=[r['wall_seconds'] for r in rs]
        rss=[r['max_rss_bytes'] for r in rs if r['max_rss_bytes'] is not None]
        summary[label][which]={'n':len(rs),'median_wall_seconds':statistics.median(walls),'min_wall_seconds':min(walls),'max_wall_seconds':max(walls),'median_max_rss_bytes':statistics.median(rss) if rss else None,'min_max_rss_bytes':min(rss) if rss else None,'max_max_rss_bytes':max(rss) if rss else None,'stdout_hashes':sorted({r['stdout_sha256'] for r in rs}),'stderr_hashes':sorted({r['stderr_sha256'] for r in rs})}
    if w['name'].endswith('_languages'):
        base=summary[label]['released_v101']['median_wall_seconds']; cand=summary[label]['candidate_110']['median_wall_seconds']
        summary[label]['candidate_vs_released_median_wall_ratio']=cand/base
    else:
        base=summary[label]['released_v101']['median_wall_seconds']; cand=summary[label]['candidate_110']['median_wall_seconds']
        summary[label]['candidate_vs_released_median_wall_ratio']=cand/base
report['summary']=summary
path=outdir/'report.json'
path.write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
print(json.dumps({'report':str(path.relative_to(root)),'summary':summary},indent=2,sort_keys=True))
