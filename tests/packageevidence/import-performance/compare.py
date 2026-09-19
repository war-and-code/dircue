#!/usr/bin/env python3
"""Compare frozen importer helpers over valid and rejected existing JSON reports."""
import argparse,datetime,hashlib,json,os,pathlib,random,re,statistics,subprocess,time

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def percentile(values,q):
 v=sorted(values);x=(len(v)-1)*q;lo=int(x);hi=min(lo+1,len(v)-1);return v[lo]+(v[hi]-v[lo])*(x-lo)
def measure(binary,case,out,name):
 command=['/usr/bin/time','-l',str(binary),'--report',case['path'],'--limits',json.dumps(case['limits'],separators=(',',':')),*case['arguments']]
 result=subprocess.run(command,capture_output=True,timeout=180,env=dict(os.environ,GOMAXPROCS='2'))
 (out/(name+'.stdout.json')).write_bytes(result.stdout);(out/(name+'.stderr.txt')).write_bytes(result.stderr)
 if result.returncode:raise RuntimeError((name,result.returncode,result.stderr.decode()))
 row=json.loads(result.stdout);rss=re.search(rb'(\d+)\s+maximum resident set size',result.stderr);assert rss
 row['process_peak_rss_bytes']=int(rss.group(1));assert row['input_sha256']==case['input_sha256']
 return row

def equivalent(old,new,case):
 for key in ['input_sha256','output_sha256','output_bytes','report_nil','error_identity','error_text','context_checks']:
  if old[key]!=new[key]:raise AssertionError((case['name'],key,old[key],new[key]))
 expected=case.get('expected_error')
 if expected is not None and old['error_identity']!=expected:raise AssertionError((case['name'],'expected error',expected,old['error_identity']))
 if old['error_identity'] and not old['report_nil']:raise AssertionError((case['name'],'failed import returned report'))

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--before',type=pathlib.Path,required=True);p.add_argument('--after',type=pathlib.Path,required=True);p.add_argument('--cases',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True);p.add_argument('--mode',choices=['contracts','timings'],required=True);p.add_argument('--pairs',type=int,default=20);a=p.parse_args()
 if a.pairs<10:raise ValueError('timings require at least10 pairs')
 out=a.output.resolve();out.mkdir(parents=True,exist_ok=True);binaries={'before':a.before.resolve(),'after':a.after.resolve()};hashes={k:sha(v) for k,v in binaries.items()};cases=json.loads(a.cases.read_text())
 if a.mode=='timings':cases=[c for c in cases if c['name'] in ['actual-syft-fixture','synthetic-1000','synthetic-20000']]
 for case in cases:assert sha(pathlib.Path(case['path']))==case['input_sha256']
 sources={str(path):sha(path) for path in sorted(pathlib.Path('pkg/packageevidence').glob('*.go'))}
 harness={str(path):sha(path) for path in sorted(pathlib.Path(__file__).parent.iterdir()) if path.is_file()}
 started=datetime.datetime.now(datetime.timezone.utc).isoformat();randomizer=random.Random(20260919);rows=[]
 for case in cases:
  samples={'before':[],'after':[]}
  if a.mode=='timings':
   for i in range(3):
    for variant in binaries:measure(binaries[variant],case,out,case['name']+'-warmup-'+str(i)+'-'+variant)
  count=1 if a.mode=='contracts' else a.pairs
  for i in range(count):
   variants=['before','after'];randomizer.shuffle(variants);pair={}
   for variant in variants:
    sample=measure(binaries[variant],case,out,case['name']+'-'+str(i)+'-'+variant);pair[variant]=sample;samples[variant].append(sample)
   equivalent(pair['before'],pair['after'],case)
  row={'case':case,'samples':samples,'contracts_identical':True}
  if a.mode=='timings':
   row['summary']={}
   for variant,s in samples.items():
    ns=[x['duration_ns'] for x in s]
    row['summary'][variant]={'median_ms':statistics.median(ns)/1e6,'p95_ms':percentile(ns,.95)/1e6,'minimum_ms':min(ns)/1e6,'maximum_ms':max(ns)/1e6,'coefficient_of_variation':statistics.stdev(ns)/statistics.mean(ns),'median_allocated_bytes':statistics.median(x['allocated_bytes'] for x in s),'median_allocations':statistics.median(x['allocations'] for x in s),'maximum_process_peak_rss_bytes':max(x['process_peak_rss_bytes'] for x in s)}
  rows.append(row);(out/'partial.json').write_text(json.dumps(rows,indent=2)+'\n');print(case['name'],'pass',flush=True)
 assert hashes=={k:sha(v) for k,v in binaries.items()}
 assert sources=={str(path):sha(path) for path in sorted(pathlib.Path('pkg/packageevidence').glob('*.go'))}
 assert harness=={str(path):sha(path) for path in sorted(pathlib.Path(__file__).parent.iterdir()) if path.is_file()}
 receipt={'started_utc':started,'completed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'mode':a.mode,'binary_sha256':hashes,'source_sha256':sources,'harness_sha256':harness,'source_and_binaries_unchanged':True,'cases':rows,'method':'Independent child processes, GOMAXPROCS=2; import latency/allocation counters exclude reading input and output serialization. Process RSS includes both. Contracts mode has one diagnostic sample/variant; timings uses 3 warmups then deterministic shuffled alternating pairs. No Syft or source scanning.','statistical_limit':'20 paired timing samples are descriptive local measurements, not production tail guarantees. Single contract samples describe allocation behavior and are not latency comparisons.'}
 (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
if __name__=='__main__':main()
