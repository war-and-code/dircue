#!/usr/bin/env python3
import hashlib,json,pathlib,statistics,subprocess,time
root=pathlib.Path.cwd(); out=root/'.cache/v110-validation/perf-run-final'; out.mkdir(parents=True,exist_ok=True)
base=root/'.cache/v110-validation/released-v101/dircue'; cand=root/'.cache/v110-validation/candidate'
work=pathlib.Path('/tmp/dircue-v110-validation-xml128')
args=['analyze','formats','--source','directory','--max-file-bytes','0','--json',str(work)]
def sha(p):
 h=hashlib.sha256()
 with open(p,'rb') as f:
  for b in iter(lambda:f.read(1<<20),b''):h.update(b)
 return h.hexdigest()
def tree_sha(p):
 h=hashlib.sha256(); files=sorted(x for x in p.rglob('*') if x.is_file() and not x.is_symlink())
 for f in files:
  rel=f.relative_to(p).as_posix().encode(); h.update(len(rel).to_bytes(8,'big')); h.update(rel); h.update(f.stat().st_size.to_bytes(8,'big')); h.update(bytes.fromhex(sha(f)))
 return {'file_count':len(files),'sha256':h.hexdigest()}
def run(which,phase,i):
 binary=base if which=='released_v101' else cand
 stem=f'xml128_formats-{phase}-{i:02d}-{which}'; so=out/(stem+'.stdout'); se=out/(stem+'.stderr'); to=out/(stem+'.time')
 cmd=['/usr/bin/time','-l','-o',str(to),str(binary),*args]
 start=time.perf_counter()
 with so.open('wb') as sf,se.open('wb') as ef: proc=subprocess.run(cmd,stdout=sf,stderr=ef,cwd=root)
 wall=time.perf_counter()-start; maxrss=None
 for line in to.read_text(errors='replace').splitlines():
  if 'maximum resident set size' in line:
   try:maxrss=int(line.strip().split()[0])
   except:pass
 return {'workload':'xml128_formats','phase':phase,'index':i,'binary':which,'argv':[str(binary),*args],'status':proc.returncode,'wall_seconds':wall,'max_rss_bytes':maxrss,'stdout_sha256':sha(so),'stdout_bytes':so.stat().st_size,'stderr_sha256':sha(se),'stderr_bytes':se.stat().st_size,'time_log':str(to.relative_to(root))}
rows=[]; warm=[run(x,'warm',0) for x in ('released_v101','candidate_110')]; rows.extend(warm)
if any(r['status'] for r in warm):raise SystemExit(warm)
if (out/'xml128_formats-warm-00-released_v101.stdout').read_bytes() != (out/'xml128_formats-warm-00-candidate_110.stdout').read_bytes():raise SystemExit('XML formats outputs differ')
for i in range(5):
 for which in (('released_v101','candidate_110') if i%2==0 else ('candidate_110','released_v101')):
  row=run(which,'measure',i); rows.append(row)
  if row['status']!=0:raise SystemExit(row)
report=json.loads((out/'report.json').read_text())
# The first XML language pilot returned an empty language document: XML is not in that analyzer's language scope. Keep its raw capture files but exclude those runs from the measured workload set.
report['runs']=[r for r in report['runs'] if r['workload']!='xml128_languages']
report['workloads']=[w for w in report['workloads'] if w['name']!='xml128_languages']
report['excluded_pilots']=[{'workload':'xml128_languages','reason':'The language analyzer excluded the XML data file and returned an empty JSON object; not a meaningful content-processing workload. Raw captures remain in this directory.'}]
report['workloads'].append({'name':'xml128_formats','argv_args':args,'path':str(work),'tree':tree_sha(work),'git_revision':None,'git_status':False,'git_note':'Synthetic corpus outside any owned Git tree; content tree digest is authoritative.','exact_legacy_stdout_equal':True,'legacy_stdout_sha256':warm[0]['stdout_sha256'],'formats_read_scope':'128 MiB selected XML file; format analyzer inspected a bounded 65,537-byte prefix.'})
report['runs'].extend(rows)
summary={}
for w in report['workloads']:
 label=w['name']; summary[label]={}
 for which in ('released_v101','candidate_110'):
  rs=[r for r in report['runs'] if r['workload']==label and r['binary']==which and r['phase']=='measure']
  if not rs:continue
  walls=[r['wall_seconds'] for r in rs];rss=[r['max_rss_bytes'] for r in rs if r['max_rss_bytes'] is not None]
  summary[label][which]={'n':len(rs),'median_wall_seconds':statistics.median(walls),'min_wall_seconds':min(walls),'max_wall_seconds':max(walls),'median_max_rss_bytes':statistics.median(rss) if rss else None,'min_max_rss_bytes':min(rss) if rss else None,'max_max_rss_bytes':max(rss) if rss else None,'stdout_hashes':sorted({r['stdout_sha256'] for r in rs}),'stderr_hashes':sorted({r['stderr_sha256'] for r in rs})}
 if 'released_v101' in summary[label] and 'candidate_110' in summary[label]:
  summary[label]['candidate_vs_released_median_wall_ratio']=summary[label]['candidate_110']['median_wall_seconds']/summary[label]['released_v101']['median_wall_seconds']
report['summary']=summary
report['candidate_binary_sha256_after_runs']=sha(cand);report['released_v101_binary_sha256_after_runs']=sha(base)
(out/'report.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
print(json.dumps({'xml128_formats':summary['xml128_formats'],'report':str((out/'report.json').relative_to(root))},indent=2,sort_keys=True))
