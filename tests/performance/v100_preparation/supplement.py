#!/usr/bin/env python3
"""Bounded supplemental paths: mixed uv/.NET, saved report and structural worker."""
import argparse,gzip,hashlib,json,os,subprocess,tarfile
from pathlib import Path
from benchmark import ROOT,OUT,CACHE,inventory,measure,summary,save,digest,utc

def main():
 p=argparse.ArgumentParser();p.add_argument('--mixed',type=Path,required=True);p.add_argument('--worker-archive',type=Path,required=True);p.add_argument('--checksums',type=Path,required=True);a=p.parse_args()
 baseline=CACHE/'baseline/dircue';receipt=json.loads((OUT/'build-receipt.json').read_text());assert digest(baseline)==receipt['binaries']['dircue']['sha256']
 expected=next(line.split()[0] for line in a.checksums.read_text().splitlines() if line.split()[-1]==a.worker_archive.name);assert digest(a.worker_archive)==expected
 worker=CACHE/'baseline/dircue-structural-worker'
 with tarfile.open(a.worker_archive) as archive:
  members=[m for m in archive.getmembers() if m.isfile() and Path(m.name).name=='dircue-structural-worker'];assert len(members)==1
  worker.write_bytes(archive.extractfile(members[0]).read());worker.chmod(0o755)
 inv=inventory(a.mixed,'directory');structure=ROOT/'tests/structural_breadth/testdata';sinv=inventory(structure,'directory')
 common=['--json','--source','directory','--workers','8',str(a.mixed)]
 reportpath=CACHE/'perf/saved-report.json'; subprocess.run([str(baseline),'analyze','all','--declarations','--projects',*common],stdout=reportpath.open('wb'),check=True)
 cmds={'mixed_languages':[str(baseline),*common],'mixed_optional':[str(baseline),'analyze','all','--projects','--graph','--declarations','--environments',*common],'saved_compare_self':[str(baseline),'compare',str(reportpath),str(reportpath),'--json'],'structure_only':[str(baseline),'analyze','structure','--functions','--structural-worker',str(worker),'--source','directory','--workers','8','--json',str(structure)]}
 expected_outputs={};samples={x:[] for x in cmds};warmups={x:[] for x in cmds}
 for round in range(23):
  for lane in list(cmds)[::1 if round%2==0 else -1]:
   payload,s=measure(cmds[lane],CACHE/'perf/time-supplement.txt');assert lane not in expected_outputs or payload==expected_outputs[lane];expected_outputs[lane]=payload;s['round']=round-2
   (warmups if round<3 else samples)[lane].append(s)
 for lane,payload in expected_outputs.items():(CACHE/'perf'/('golden-'+lane+'.json.gz')).write_bytes(gzip.compress(payload,mtime=0))
 sizes={'mixed_languages':inv['bytes'],'mixed_optional':inv['bytes'],'saved_compare_self':reportpath.stat().st_size*2,'structure_only':sinv['bytes']}
 assert inventory(a.mixed,'directory')==inv and inventory(structure,'directory')==sinv
 assert digest(baseline)==receipt['binaries']['dircue']['sha256']
 save(OUT/'supplement.json.gz',{'at':utc(),'mixed_input':inv,'structural_input':sinv,'worker_archive_sha256':expected,'worker_sha256':digest(worker),'saved_report_sha256':digest(reportpath),'harness_sha256':digest(Path(__file__)),'commands':cmds,'warmups':warmups,'samples':samples,'summary':{lane:summary(values,sizes[lane]) for lane,values in samples.items()},'scope':'Mixed uv/.NET is a pre-existing authored fixture, not a public-scale independent repo. Saved self-comparison includes load+comparison/serialization; no-change case. Structural tiny 19-language fixture measures bounded optional subprocess cost, not monorepo scale. Same 3-warmup/20-run tail and shared-host caveats as main baseline.'})
 print('supplement complete',flush=True)
if __name__=='__main__':main()
