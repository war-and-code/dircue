#!/usr/bin/env python3
"""Measure the frozen import benchmark after its untimed validation import."""
import argparse,datetime,hashlib,json,os,pathlib,random,re,statistics,subprocess

def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--before',type=pathlib.Path,required=True);p.add_argument('--after',type=pathlib.Path,required=True);p.add_argument('--report',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True);p.add_argument('--pairs',type=int,default=20);a=p.parse_args()
 out=a.output.resolve();out.mkdir(parents=True,exist_ok=True);binary={'before':a.before.resolve(),'after':a.after.resolve()};hashes={k:sha(v) for k,v in binary.items()};source={str(p):sha(p) for p in pathlib.Path('pkg/packageevidence').glob('*.go')};inputhash=sha(a.report);samples={'before':[],'after':[]};rng=random.Random(20260919);started=datetime.datetime.now(datetime.timezone.utc).isoformat()
 for i in range(a.pairs+3):
  order=list(binary);rng.shuffle(order)
  for variant in order:
   result=subprocess.run(['/usr/bin/time','-l',str(binary[variant]),'--report',str(a.report),'--iterations','1'],capture_output=True,timeout=60,env=dict(os.environ,GOMAXPROCS='2'))
   name=f'{i}-{variant}';(out/(name+'.stdout.json')).write_bytes(result.stdout);(out/(name+'.stderr.txt')).write_bytes(result.stderr)
   if result.returncode:raise RuntimeError((name,result.stderr.decode()))
   row=json.loads(result.stdout);row['process_peak_rss_bytes']=int(re.search(rb'(\d+)\s+maximum resident set size',result.stderr).group(1));assert row['gomaxprocs']==2
   if i>=3:samples[variant].append(row)
  print(i,flush=True)
 for b,aft in zip(samples['before'],samples['after']):
  for key in ['iterations','report_bytes','packages','relationships','gomaxprocs','go_version']:assert b[key]==aft[key],key
 summary={}
 for variant,rows in samples.items():
  summary[variant]={key:statistics.median(x[key] for x in rows) for key in ['ns_per_import','allocated_bytes_per_import','allocations_per_import','process_peak_rss_bytes']}
 assert inputhash==sha(a.report) and hashes=={k:sha(v) for k,v in binary.items()} and source=={str(p):sha(p) for p in pathlib.Path('pkg/packageevidence').glob('*.go')}
 receipt={'started_utc':started,'completed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'input_path':str(a.report),'input_sha256':inputhash,'input_bytes':a.report.stat().st_size,'binary_sha256':hashes,'source_sha256':source,'harness_sha256':sha(pathlib.Path(__file__)),'source_and_binaries_unchanged':True,'method':'Fresh processes; each imports once untimed, collects garbage, then times one import. Three warmup pairs excluded, followed by20 deterministic shuffled pairs. GOMAXPROCS2. Whole-process RSS includes both imports; import counters exclude input read and output JSON.','samples':samples,'summary':summary}
 (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
if __name__=='__main__':main()
