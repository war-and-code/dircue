#!/usr/bin/env python3
"""Verify exact golden outputs, or run interleaved paired cache-only A/B samples."""
import argparse,gzip,hashlib,json,os,statistics,subprocess,random
from pathlib import Path
from benchmark import ROOT,OUT,CACHE,command,measure,summary,save,digest,utc,inventory

def main():
 p=argparse.ArgumentParser();p.add_argument('action',choices=['verify','measure']);a=p.parse_args()
 receipt=json.loads((OUT/'candidate-build.json').read_text());controls={k:os.getenv(k) for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG','GOFLAGS','CGO_ENABLED']};assert controls==receipt['runtime_controls'];candidate=Path(receipt['binary_path']);baseline=CACHE/'baseline/dircue'
 assert digest(candidate)==receipt['candidate_sha256'] and digest(baseline)==receipt['baseline_rebuild_sha256']
 cases=json.loads((OUT/'scenarios.json').read_text());rows=[]
 if a.action=='verify':
  dest=OUT/'candidate-equivalence.json'
  if dest.exists():raise RuntimeError('Refusing to overwrite equivalence receipt')
  commands=[]
  for c in cases:
   for lane in c['lanes']:commands.append((c['name']+'-'+lane,command(c,lane),CACHE/'perf'/f"golden-{c['name']}-{lane}.json.gz"))
  supplement=json.loads(gzip.decompress((OUT/'supplement.json.gz').read_bytes()))
  for lane,cmd in supplement['commands'].items():commands.append((lane,cmd,CACHE/'perf'/('golden-'+lane+'.json.gz')))
  for name,cmd,golden in commands:
   cmd=[str(candidate),*cmd[1:]];result=subprocess.run(cmd,stdout=subprocess.PIPE,stderr=subprocess.PIPE,cwd=ROOT,timeout=300)
   expected=gzip.decompress(golden.read_bytes());assert result.returncode==0 and not result.stderr and result.stdout==expected,(name,result.returncode,result.stderr[:200])
   rows.append({'name':name,'exact_match':True,'stdout_sha256':hashlib.sha256(result.stdout).hexdigest(),'command':cmd});print(name,'exact',flush=True)
  save(dest,{'at':utc(),'candidate_sha256':digest(candidate),'baseline_sha256':digest(baseline),'scope':'All 18 core and 4 supplemental warm CLI goldens, untimed functional window. Raw baseline JSON retained under .cache/v100/perf.','cases':rows})
  return
 dest=OUT/'cache-ab.json.gz'
 if dest.exists():raise RuntimeError('Refusing to overwrite A/B receipt')
 selected=[('spring-framework','languages'),('spring-framework','discovery'),('roslyn','languages'),('roslyn','all'),('aspnetcore','optional'),('xml-2gib','optional')]
 result={'started_at':utc(),'runtime_controls':controls,'harness_sha256':digest(Path(__file__)),'baseline_sha256':digest(baseline),'candidate_sha256':digest(candidate),'method':'3 warmup pairs plus20 measured adjacent pairs, AB/BA alternated each round. All raw samples retained; exactstdout/emptystderr/exit0 per invocation. Sharedhost quietwindow; no concurrent builds/tests/installers. Medians and paired bootstrap are diagnostic evidence, not portable SLA.','scenarios':[]}
 for name,lane in selected:
  case=next(c for c in cases if c['name']==name);assert inventory(Path(case['path']),case['source'])['manifest_sha256']==case['input']['manifest_sha256']
  old=command(case,lane);commands={'baseline':old,'candidate':[str(candidate),*old[1:]]};golden=gzip.decompress((CACHE/'perf'/f'golden-{name}-{lane}.json.gz').read_bytes());samples={key:[] for key in commands};warmups={key:[] for key in commands}
  for i in range(23):
   for key in ['baseline','candidate'][::1 if i%2==0 else -1]:
    payload,s=measure(commands[key],CACHE/'perf/cache-ab-time.txt');assert payload==golden,(name,lane,key,'output mismatch');s['round']=i-2;(warmups if i<3 else samples)[key].append(s)
   if i>=3 and (i-2)%5==0:print(name,lane,'pair',i-2,flush=True)
  ratios=[b['seconds']/a['seconds'] for a,b in zip(samples['baseline'],samples['candidate'])];rng=random.Random(100);boot=sorted(statistics.median(rng.choices(ratios,k=len(ratios))) for _ in range(5000))
  entry={'corpus':name,'lane':lane,'commands':commands,'warmups':warmups,'samples':samples,'summary':{key:summary(values,case['input']['bytes']) for key,values in samples.items()},'paired_candidate_over_baseline':ratios,'paired_median_ratio':statistics.median(ratios),'paired_bootstrap_median_ratio_95pct':[boot[125],boot[4874]],'bootstrap_note':'Fixed-seed percentile bootstrap of20 paired ratios; supports only this same-host workload/window, not production tail precision.'}
  result['scenarios'].append(entry);save(CACHE/'perf'/('ab-'+name+'-'+lane+'.json'),entry);print(name,lane,'ratio',entry['paired_median_ratio'],flush=True)
 for c in cases:assert inventory(Path(c['path']),c['source'])['manifest_sha256']==c['input']['manifest_sha256']
 assert digest(candidate)==receipt['candidate_sha256'] and digest(baseline)==receipt['baseline_rebuild_sha256'];result['finished_at']=utc();save(dest,result)
if __name__=='__main__':main()
