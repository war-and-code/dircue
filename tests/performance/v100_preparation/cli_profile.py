#!/usr/bin/env python3
"""Profile optional CLI modules from the frozen source archive, separate from timing."""
import gzip,json,os,subprocess,tarfile
from pathlib import Path
from benchmark import ROOT,OUT,CACHE,save,digest,utc,command,inventory

def main():
 archive=CACHE/'baseline/source.tar';bound=json.loads((OUT/'provenance-validation.json').read_text())['frozen_source_archive'];assert digest(archive)==bound['sha256']
 source=CACHE/'baseline/source'
 if not source.exists():
  source.mkdir()
  with tarfile.open(archive) as a:a.extractall(source,filter='data')
 manifest=json.loads(gzip.decompress((OUT/'tracked-source-manifest.json.gz').read_bytes()))['files'];assert all(digest(source/p)==sha for p,sha in manifest.items())
 wrapper=source/'profile_main.go';wrapper.write_bytes((OUT/'profile_main.go.txt').read_bytes());binary=CACHE/'baseline/cli-profile'
 build=['go','build','-mod=readonly','-buildvcs=false','-trimpath','-ldflags','-X dircue/internal/cli.Version=0.8.0','-o',str(binary),str(wrapper)]
 subprocess.run(build,cwd=source,check=True);binaryhash=digest(binary)
 receipt={'commit':bound['commit'],'source_archive_sha256':bound['sha256'],'wrapper_sha256':digest(wrapper),'binary_sha256':binaryhash,'build':build,'built_at':utc(),'runs':[]};save(OUT/'cli-profile-receipt.json',receipt)
 cases=json.loads((OUT/'scenarios.json').read_text())
 for name in ['aspnetcore','xml-2gib']:
  case=next(c for c in cases if c['name']==name);assert inventory(Path(case['path']),case['source'])['manifest_sha256']==case['input']['manifest_sha256']
  for kind in ['cpu','alloc']:
   folder=CACHE/'perf'/(name+'-optional-'+kind);folder.mkdir();env={k:v for k,v in os.environ.items() if not k.startswith(('DIRCUE_PROFILE_','V100_PROFILE_')) and k not in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG']};env.update(V100_PROFILE_DIR=str(folder),V100_PROFILE_KIND=kind,V100_PROFILE_ITERATIONS='3' if name=='aspnetcore' else '30')
   cmd=[str(binary),*command(case,'optional')[1:]]
   with (folder/'run.txt').open('w') as f:subprocess.run(cmd,cwd=ROOT,env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
   for suffix,flags in [('flat',[]),('cum',['-cum'])]:
    dest=OUT/'profiles'/(name+'-optional-'+kind+'-'+suffix+'.txt')
    with dest.open('w') as f:subprocess.run(['go','tool','pprof','-top','-nodecount=45',*(['-sample_index=alloc_space'] if kind=='alloc' else ['-tagfocus=phase=cli']),*flags,str(binary),str(folder/(kind+'.pprof'))],stdout=f,cwd=ROOT,check=True)
   receipt['runs'].append({'scenario':name,'kind':kind,'command':cmd,'iterations':env['V100_PROFILE_ITERATIONS'],'profile_sha256':digest(folder/(kind+'.pprof')),'stats':json.loads((folder/'stats.json').read_text()),'scope':'CLI repeated after one warmup; CPU starts after warmup; alloc cumulative includes warmup. Startup excluded and stdout discarded. Not latency evidence.'});save(OUT/'cli-profile-receipt.json',receipt)
  assert inventory(Path(case['path']),case['source'])['manifest_sha256']==case['input']['manifest_sha256']
 assert digest(binary)==binaryhash
 print('optional CLI profiles complete',flush=True)
if __name__=='__main__':main()
