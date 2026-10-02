#!/usr/bin/env python3
import hashlib,json,pathlib,platform,statistics,subprocess,time
root=pathlib.Path.cwd(); out=root/'.cache/v110-validation/perf-run-final'; out.mkdir(parents=True,exist_ok=True)
bins={'released_v101':root/'.cache/v110-validation/released-v101/dircue','candidate_110':root/'.cache/v110-validation/candidate'}
workloads=[('spring_languages_extended',30,['analyze','languages','--source','directory','--max-file-bytes','0','--json',str(root/'.cache/corpus/spring-petclinic')]),('dotnet_http_languages_extended',15,['analyze','languages','--source','directory','--max-file-bytes','0','--json',str(root/'.cache/corpus/aspnetcore/src/Http')])]
def sha(p):
 h=hashlib.sha256()
 with open(p,'rb') as f:
  for b in iter(lambda:f.read(1<<20),b''):h.update(b)
 return h.hexdigest()
def run(label,which,phase,i,args):
 stem=f'{label}-{phase}-{i:02d}-{which}'; so=out/(stem+'.stdout'); se=out/(stem+'.stderr'); to=out/(stem+'.time')
 cmd=['/usr/bin/time','-l','-o',str(to),str(bins[which]),*args]
 start=time.perf_counter()
 with so.open('wb') as sf,se.open('wb') as ef: p=subprocess.run(cmd,stdout=sf,stderr=ef,cwd=root)
 wall=time.perf_counter()-start; rss=None
 for line in to.read_text(errors='replace').splitlines():
  if 'maximum resident set size' in line:
   try:rss=int(line.strip().split()[0])
   except:pass
 return {'workload':label,'phase':phase,'index':i,'binary':which,'argv':[str(bins[which]),*args],'status':p.returncode,'wall_seconds':wall,'max_rss_bytes':rss,'stdout_sha256':sha(so),'stdout_bytes':so.stat().st_size,'stderr_sha256':sha(se),'stderr_bytes':se.stat().st_size,'time_log':str(to.relative_to(root))}
def stats(rows,n):
 a=[next(r for r in rows if r['index']==i and r['binary']=='released_v101') for i in range(n)]
 b=[next(r for r in rows if r['index']==i and r['binary']=='candidate_110') for i in range(n)]
 diffs=[y['wall_seconds']-x['wall_seconds'] for x,y in zip(a,b)]
 result={}
 for which in bins:
  rs=[r for r in rows if r['binary']==which]; w=[r['wall_seconds'] for r in rs]; rss=[r['max_rss_bytes'] for r in rs if r['max_rss_bytes'] is not None]
  result[which]={'n':len(rs),'median_wall_seconds':statistics.median(w),'mean_wall_seconds':statistics.mean(w),'min_wall_seconds':min(w),'max_wall_seconds':max(w),'stdev_wall_seconds':statistics.stdev(w) if len(w)>1 else 0,'median_max_rss_bytes':statistics.median(rss) if rss else None,'min_max_rss_bytes':min(rss) if rss else None,'max_max_rss_bytes':max(rss) if rss else None,'stdout_hashes':sorted({r['stdout_sha256'] for r in rs}),'stderr_hashes':sorted({r['stderr_sha256'] for r in rs}),'statuses':sorted({r['status'] for r in rs})}
 result['paired_wall_delta_candidate_minus_released']={'n':len(diffs),'median_seconds':statistics.median(diffs),'mean_seconds':statistics.mean(diffs),'min_seconds':min(diffs),'max_seconds':max(diffs),'candidate_slower_pairs':sum(d>0 for d in diffs),'candidate_faster_pairs':sum(d<0 for d in diffs),'ties':sum(d==0 for d in diffs)}
 return result
report={'method':'One warm run per binary, then alternating AB/BA interleaved pairs; wall clock is Python monotonic around /usr/bin/time -l; same source trees as primary run.','source_revision':subprocess.run(['git','rev-parse','HEAD'],cwd=root,capture_output=True,text=True,check=True).stdout.strip(),'candidate_binary_sha256':sha(bins['candidate_110']),'released_v101_binary_sha256':sha(bins['released_v101']),'workloads':[],'runs':[]}
for label,n,args in workloads:
 # Warm both in A/B order and verify exact JSON and no stderr.
 warm=[run(label,which,'warm',0,args) for which in bins]; report['runs'].extend(warm)
 if any(r['status'] or r['stderr_bytes'] for r in warm):raise SystemExit(f'warm failure or stderr: {warm}')
 outs=[(out/f'{label}-warm-00-{which}.stdout').read_bytes() for which in bins]
 if outs[0]!=outs[1]:raise SystemExit(f'output mismatch on warm: {label}')
 for i in range(n):
  order=('released_v101','candidate_110') if i%2==0 else ('candidate_110','released_v101')
  for which in order:
   r=run(label,which,'measure',i,args); report['runs'].append(r)
   if r['status'] or r['stderr_bytes']:raise SystemExit(f'run failure or stderr: {r}')
 rs=[r for r in report['runs'] if r['workload']==label and r['phase']=='measure']
 report['workloads'].append({'name':label,'pairs':n,'args':args,'exact_warm_stdout_equal':True,'stdout_hashes_by_binary':{x:sorted({r['stdout_sha256'] for r in rs if r['binary']==x}) for x in bins},'summary':stats(rs,n)})
 print('finished',label,flush=True)
(out/'extended-series.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
print(json.dumps(report['workloads'],indent=2,sort_keys=True))
