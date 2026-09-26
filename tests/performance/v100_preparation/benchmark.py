#!/usr/bin/env python3
"""Measurement-only v0.8.0 baseline. Never builds or executes corpus code."""
import signal
import argparse, gzip, hashlib, importlib.util, json, math, os, platform, re, statistics, subprocess, time
from pathlib import Path
from datetime import datetime, timezone
if not __debug__: raise RuntimeError('Do not run measurement gates with Python -O')
ROOT=Path(__file__).resolve().parents[3]
OUT=Path(os.environ.get('V100_OUTPUT_DIR', ROOT/'tests/performance/v100_preparation')).resolve()
CACHE=ROOT/'.cache/v100'
EXPECTED='29b57bd3c126e0f545f361d580b3c397625ce02a'
spec=importlib.util.spec_from_file_location('prior', ROOT/'tests/performance/v060_candidate/benchmark.py')
prior=importlib.util.module_from_spec(spec); spec.loader.exec_module(prior)
digest, inventory=prior.digest,prior.inventory

def run(args): return subprocess.check_output(args,cwd=ROOT,text=True).strip()
def save(path,v):
 path.parent.mkdir(parents=True,exist_ok=True)
 raw=(json.dumps(v,indent=2)+'\n').encode()
 path.write_bytes(gzip.compress(raw,mtime=0) if path.suffix=='.gz' else raw)
def utc(): return datetime.now(timezone.utc).isoformat()
def command(case,lane):
 modes={'languages':[], 'discovery':['analyze','discovery'], 'all':['analyze','all'], 'projects':['analyze','projects'], 'optional':['analyze','all','--projects','--graph','--formats','--declarations','--environments','--availability','--registries']}
 return [str(CACHE/'baseline/dircue'),*modes[lane],'--json','--source',case['source'],'--workers','8','--tree-size','1000000',*(['--rev',case['input']['commit']] if case['source']=='git' else []),case['path']]
def prepare(corpus,xml):
 if any((OUT/n).exists() for n in ['build-receipt.json','host-receipt.json','scenarios.json']): raise RuntimeError('Refusing to replace prepared receipts; set V100_OUTPUT_DIR to a fresh directory')
 assert run(['git','rev-parse','HEAD'])==EXPECTED
 files=prior.build_inputs(dict(os.environ,CGO_ENABLED='0',GOWORK='off',GOFLAGS=''))
 receipt={'commit':EXPECTED,'created_at':utc(),'production_inputs':files,'go_version':run(['go','version']),'flags':'-mod=readonly -buildvcs=false -trimpath; CLI -ldflags -X dircue/internal/cli.Version=0.8.0; default optimizations, no stripping, no -N/-l, no race','binaries':{n:{'sha256':digest(CACHE/'baseline'/n),'build_info':run(['go','version','-m',str(CACHE/'baseline'/n)])} for n in ['dircue','scanner.test']}}
 save(OUT/'build-receipt.json',receipt)
 def probe(args):
  p=subprocess.run(args,text=True,capture_output=True); return p.stdout.strip() if p.returncode==0 else {'unavailable':p.stderr.strip()}
 host={'at':utc(),'platform':platform.platform(),'kernel':probe(['uname','-r']),'cpu':probe(['sysctl','-n','machdep.cpu.brand_string']),'logical_cpus':os.cpu_count(),'physical_cpus':probe(['sysctl','-n','hw.physicalcpu']),'memory_bytes':probe(['sysctl','-n','hw.memsize']),'swap':probe(['sysctl','vm.swapusage']),'filesystem':probe(['df','-T',str(ROOT)]),'power':probe(['pmset','-g','custom']),'thermal':probe(['pmset','-g','therm']),'environment':{k:os.getenv(k) for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG','CGO_ENABLED','GOFLAGS']},'isolation':'Shared macOS desktop; coordinated agent quiet window; no CPU pinning, kernel tuning, cache eviction, or process isolation. OS background activity remains uncontrolled.','smt_governor_turbo':'not applicable or not exposed by this macOS Apple Silicon host; unchanged','tools':{x:probe(['which',x]) for x in ['go','python3','hyperfine','sample','xctrace','perf','samply']}}
 save(OUT/'host-receipt.json',host)
 cases=[]
 for name in ['spring-framework','roslyn','aspnetcore']:
  path=corpus/name
  assert not run(['git','-C',str(path),'status','--porcelain'])
  inv=inventory(path,'git')
  raw=subprocess.check_output(['git','-C',str(path),'ls-tree','-rlz','HEAD'])
  inv['bytes']=sum(int(e.split(b'\t',1)[0].split()[-1]) for e in raw.split(b'\0') if e and e.split(b'\t',1)[0].split()[-1]!=b'-')
  inv['git_storage']=run(['git','-C',str(path),'count-objects','-v'])
  cases.append({'name':name,'source':'git','path':str(path),'input':inv,'lanes':['languages','discovery','all','projects']+(['optional'] if name=='aspnetcore' else [])})
 cases.append({'name':'xml-2gib','source':'directory','path':str(xml),'input':inventory(xml,'directory'),'lanes':['languages','discovery','all','projects','optional']})
 save(OUT/'scenarios.json',cases)
 for c in cases: save(CACHE/'perf'/('fixture-'+c['name']+'.json'),c)
 print('Prepared',len(cases),'corpora',flush=True)
def measure(cmd,artifact):
 if platform.system()!='Darwin': raise RuntimeError('This measurement parser requires macOS time -l')
 start=time.perf_counter(); p=subprocess.Popen(['/usr/bin/time','-l','-o',str(artifact),*cmd],stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
 try: stdout,stderr=p.communicate(timeout=300)
 except subprocess.TimeoutExpired:
  os.killpg(p.pid,signal.SIGKILL); p.communicate(); raise
 elapsed=time.perf_counter()-start
 assert p.returncode==0 and not stderr,(cmd,p.returncode,stderr[:200])
 raw=artifact.read_text(); first=re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys',raw); assert first
 fields={}
 for line in raw.splitlines()[1:]:
  m=re.match(r'\s*(\d+)\s+(.+)',line)
  if m: fields[m[2]]=int(m[1])
 user,sys=float(first[2]),float(first[3])
 return stdout,{'seconds':elapsed,'time_real_seconds':float(first[1]),'user_seconds':user,'system_seconds':sys,'cpu_percent':100*(user+sys)/elapsed,'peak_rss_bytes':fields['maximum resident set size'],'time_fields':fields,'time_raw':raw,'exit_code':p.returncode,'stderr_sha256':hashlib.sha256(stderr).hexdigest(),'stdout_sha256':hashlib.sha256(stdout).hexdigest(),'at':utc()}
def summary(samples,size):
 x=sorted(s['seconds'] for s in samples); q=lambda p:x[max(0,math.ceil(len(x)*p)-1)]
 return {'n':len(x),'p50':statistics.median(x),'p95':q(.95),'p99':q(.99),'p99_9':q(.999),'p99_99':q(.9999),'max':max(x),'cv':statistics.stdev(x)/statistics.mean(x),'mad':statistics.median(abs(t-statistics.median(x)) for t in x),'median_peak_rss_bytes':statistics.median(s['peak_rss_bytes'] for s in samples),'max_peak_rss_bytes':max(s['peak_rss_bytes'] for s in samples),'median_cpu_percent':statistics.median(s['cpu_percent'] for s in samples),'operations_per_second':1/statistics.median(x),'logical_input_bytes_per_second':size/statistics.median(x)}
def collect():
 if (OUT/'baseline.json').exists() or any(OUT.glob('*-samples.json.gz')): raise RuntimeError('Refusing to replace samples; prepare a fresh V100_OUTPUT_DIR')
 cases=json.loads((OUT/'scenarios.json').read_text()); before=digest(CACHE/'baseline/dircue')
 assert before==json.loads((OUT/'build-receipt.json').read_text())['binaries']['dircue']['sha256']
 execution_env={k:os.getenv(k) for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG','CGO_ENABLED','GOFLAGS']}
 assert execution_env==json.loads((OUT/'host-receipt.json').read_text())['environment']
 report={'started_at':utc(),'harness_sha256':digest(Path(__file__)),'runtime_environment':execution_env,'method':{'runs':20,'warmups':3,'cache':'warm filesystem, fresh process each invocation; no cache eviction','wall':'perf_counter including time wrapper and output pipe; validation/hashing excluded','order':'case by case, lane order alternated each round','percentiles':'nearest rank; p50 median. p95 is worst-few-of-20; p99/p99.9/p99.99 equal worst observed, not reliable population-tail estimates','throughput':'logical full corpus bytes divided by median wall, NOT physical I/O bandwidth; bounded reads may read far less','outliers':'all retained; no outlier removal','memory':'standalone process peak RSS; no PSS on macOS. time footprint also retained; no per-core process counters','budget':'measurement baseline only; no invented absolute SLA; investigate >10% same-host drift before speedup claims'},'cases':[]}
 for case in cases:
  expected={}; samples={lane:[] for lane in case['lanes']}; warm={lane:[] for lane in case['lanes']}
  for i in range(3):
   for lane in case['lanes']:
    payload,s=measure(command(case,lane),CACHE/'perf/time.txt'); assert lane not in expected or expected[lane]==payload
    expected[lane]=payload; warm[lane].append(s)
  for i in range(20):
   for lane in case['lanes'][::1 if i%2==0 else -1]:
    payload,s=measure(command(case,lane),CACHE/'perf/time.txt'); assert payload==expected[lane],(case['name'],lane,'output changed')
    s['round']=i+1; samples[lane].append(s)
   if i%5==4: print(case['name'],'round',i+1,flush=True)
  for lane,payload in expected.items(): (CACHE/'perf'/f"golden-{case['name']}-{lane}.json.gz").write_bytes(gzip.compress(payload,mtime=0))
  record={**case,'commands':{lane:command(case,lane) for lane in case['lanes']},'warmups':warm,'samples':samples,'summary':{lane:summary(v,case['input']['bytes']) for lane,v in samples.items()}}
  save(OUT/(case['name']+'-samples.json.gz'),record); report['cases'].append({k:v for k,v in record.items() if k not in ['samples','warmups']})
  print(case['name'],json.dumps(record['summary']),flush=True)
 assert before==digest(CACHE/'baseline/dircue')
 for case in cases:
  after=inventory(Path(case['path']),case['source']); assert after['manifest_sha256']==case['input']['manifest_sha256']
 report['finished_at']=utc(); save(OUT/'baseline.json',report)
if __name__=='__main__':
 p=argparse.ArgumentParser(); p.add_argument('action',choices=['prepare','measure']); p.add_argument('--corpus',type=Path); p.add_argument('--xml',type=Path); a=p.parse_args()
 prepare(a.corpus,a.xml) if a.action=='prepare' else collect()
