#!/usr/bin/env python3
"""Build then serially measure existing comparison and Python workspace benchmarks."""
import os,subprocess,tarfile,json
from pathlib import Path
from benchmark import ROOT,OUT,CACHE,save,digest,utc

def main():
 source=CACHE/'baseline/source'
 if not source.exists():
  source.mkdir()
  with tarfile.open(CACHE/'baseline/source.tar') as archive: archive.extractall(source,filter='data')
 manifest=json.loads(__import__('gzip').decompress((OUT/'tracked-source-manifest.json.gz').read_bytes()))['files']
 assert all(digest(source/p)==h for p,h in manifest.items())
 env={k:v for k,v in os.environ.items() if k not in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG']};records=[]
 for name,pkg,pattern in [('reportdiff','./pkg/reportdiff','Benchmark(SavedReportLoad500Projects|Compare500Projects)$'),('python-workspace','./pkg/declarations','^BenchmarkPythonWorkspace$')]:
  binary=CACHE/'baseline'/(name+'.test');cmd=['go','test','-c','-mod=readonly','-buildvcs=false','-trimpath','-o',str(binary),pkg];subprocess.run(cmd,env=env,check=True,cwd=source)
  records.append({'name':name,'build_command':cmd,'binary_sha256':digest(binary),'built_at':utc(),'benchmark':pattern})
 # No builds overlap any measurement.
 for rec in records:
  binary=CACHE/'baseline'/(rec['name']+'.test');dest=OUT/(rec['name']+'-bench.txt')
  cmd=[str(binary),'-test.run=^$','-test.bench='+rec['benchmark'],'-test.benchtime=100ms','-test.count=20','-test.benchmem']
  with dest.open('w') as f:subprocess.run(cmd,env=env,stdout=f,stderr=subprocess.STDOUT,check=True,cwd=ROOT)
  rec['baseline_command']=cmd
  for sampler,flag in [('cpu','cpuprofile'),('alloc','memprofile')]:
   profile=CACHE/'perf'/(rec['name']+'-'+sampler+'.pprof')
   cmd=[str(binary),'-test.run=^$','-test.bench='+rec['benchmark'],'-test.benchtime=2s','-test.count=1','-test.benchmem','-test.'+flag+'='+str(profile)]
   with (CACHE/'perf'/(rec['name']+'-'+sampler+'-run.txt')).open('w') as f:subprocess.run(cmd,env=env,stdout=f,stderr=subprocess.STDOUT,check=True,cwd=ROOT)
   with (OUT/'profiles'/(rec['name']+'-'+sampler+'-top.txt')).open('w') as f:subprocess.run(['go','tool','pprof','-top','-nodecount=30',*(['-sample_index=alloc_space'] if sampler=='alloc' else []),str(binary),str(profile)],stdout=f,check=True,cwd=ROOT)
  assert digest(binary)==rec['binary_sha256']
 save(OUT/'micro-receipt.json',{'at':utc(),'runs':records,'scope':'Existing synthetic API benchmarks. Twenty benchmark iteration means, not twenty individual CLI latencies; do not infer tail percentiles. Profiles include benchmark setup/calibration; interpret concrete API call stacks. Builds precede all microbench measurements. Fixed released source; source manifest in tracked-source-manifest.json.gz.'})
if __name__=='__main__':main()
