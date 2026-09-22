#!/usr/bin/env python3
"""Separate sampler runs against the frozen optimized scanner test binary."""
import json, os, subprocess
from pathlib import Path
from benchmark import ROOT, OUT, CACHE, save, digest, utc, inventory

def main():
 cases=json.loads((OUT/'scenarios.json').read_text()); records=[]
 receipt=json.loads((OUT/'candidate-scanner-build.json').read_text())
 if (OUT/'candidate-profiles.json').exists():raise RuntimeError('Refusing to overwrite candidate profiles')
 assert digest(CACHE/'optimization/scanner.test')==receipt['candidate_sha256']
 for c in cases: assert inventory(Path(c['path']),c['source'])['manifest_sha256']==c['input']['manifest_sha256']
 for name,mode,sampler in [(name,'all',sampler) for name in ['spring-framework'] for sampler in ['cpu','heap']]:
  case=next(c for c in cases if c['name']==name); label=f'candidate-{name}-{mode}-{sampler}'; folder=CACHE/'perf'/label; folder.mkdir()
  env={k:v for k,v in os.environ.items() if not k.startswith('DIRCUE_PROFILE_')}
  for key in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG']: env.pop(key,None)
  config={'EXPECTED_SHA256':'845a938dbc7588b8d88090138e2bb24fa605243e510131ed362d8952fbad502e','ROOT':case['path'],'SOURCE':case['source'],'FIXTURE_ID':name,'RUN_ID':label,'OUTPUT_DIR':str(folder),'FIXTURE_RECEIPT':str(CACHE/'perf'/('fixture-'+name+'.json')),'BUILD_RECEIPT':str(OUT/'candidate-scanner-build.json'),'HOST_RECEIPT':str(OUT/'host-receipt.json'),'WORKERS':'8','INIT':'warm','MODE':mode,'MAX_TREE_SIZE':'1000000','INCLUDE_FILES':'0','MUTEX_FRACTION':'0','BLOCK_RATE_NS':'0','OS_CACHE':'warm uncontrolled macOS filesystem; dedicated agent benchmark window'}
  if case['source']=='git': config['REVISION']=case['input']['commit']
  else: env.pop('DIRCUE_PROFILE_REVISION',None)
  if sampler=='mutex': config['MUTEX_FRACTION']='5'
  if sampler=='block': config['BLOCK_RATE_NS']='10000'
  env.update({'DIRCUE_PROFILE_'+k:v for k,v in config.items()})
  flag={'cpu':'cpuprofile','heap':'memprofile','mutex':'mutexprofile','block':'blockprofile','trace':'trace'}[sampler]
  path=folder/(sampler+'.pprof' if sampler!='trace' else 'trace.out')
  cmd=[str(CACHE/'optimization/scanner.test'),'-test.run=^$','-test.bench=^BenchmarkProfileScanner$','-test.benchtime='+('1x' if sampler in ['trace','mutex','block'] else '3x' if name=='roslyn' else '10x'),'-test.count=1','-test.benchmem','-test.'+flag+'='+str(path)]
  start=utc()
  with (folder/'run.txt').open('w') as f: subprocess.run(cmd,env=env,stdout=f,stderr=subprocess.STDOUT,check=True,cwd=ROOT)
  files=[]
  if sampler=='trace':
   for kind in ['sync','syscall','sched']:
    pp=folder/(kind+'.pprof')
    with pp.open('wb') as f: subprocess.run(['go','tool','trace','-pprof='+kind,str(path)],stdout=f,check=True,cwd=ROOT)
    dest=OUT/'profiles'/(label+'-'+kind+'-top.txt'); dest.parent.mkdir(exist_ok=True)
    with dest.open('w') as f: subprocess.run(['go','tool','pprof','-top',str(pp)],stdout=f,check=True,cwd=ROOT)
    files.append(str(dest.relative_to(ROOT)))
  else:
   variants={'cpu':[('flat',['-tagfocus=phase=scan']),('cum',['-cum','-tagfocus=phase=scan'])],'heap':[('alloc',['-sample_index=alloc_space']),('alloc-cum',['-sample_index=alloc_space','-cum']),('live',['-sample_index=inuse_space'])],'mutex':[('delay',[])],'block':[('delay',[])]}[sampler]
   for suffix,flags in variants:
    dest=OUT/'profiles'/(label+'-'+suffix+'.txt'); dest.parent.mkdir(exist_ok=True)
    with dest.open('w') as f: subprocess.run(['go','tool','pprof','-top','-nodecount=45',*flags,str(CACHE/'optimization/scanner.test'),str(path)],stdout=f,check=True,cwd=ROOT)
    files.append(str(dest.relative_to(ROOT)))
  rec={'id':label,'start':start,'end':utc(),'command':cmd,'environment':config,'profile_sha256':digest(path),'profile_path':str(path.relative_to(ROOT)),'scope':'CPU phase=scan excludes warmup/validation. Heap, mutex, block and trace are cumulative process evidence including warmup, calibration and harness/validation; interpret concrete scanner caller stacks only.','tables':files,'run_output':(folder/'run.txt').read_text()}; records.append(rec); save(OUT/'candidate-profiles.json',records); print(label,'complete',flush=True)
 assert digest(CACHE/'optimization/scanner.test')==receipt['candidate_sha256']
 for c in cases: assert inventory(Path(c['path']),c['source'])['manifest_sha256']==c['input']['manifest_sha256']
 save(OUT/'candidate-profile-validation.json',{'passed':True,'at':utc(),'harness_sha256':digest(Path(__file__)),'source_commit':receipt['base_commit']})
if __name__=='__main__':main()
