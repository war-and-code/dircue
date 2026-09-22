#!/usr/bin/env python3
"""Build only the cache/lifecycle experiment on a verified v0.8.0 archive."""
import gzip,json,os,subprocess,tarfile
from pathlib import Path
from benchmark import ROOT,OUT,CACHE,digest,save,utc

FILES=['pkg/scanner/git.go','pkg/scanner/scanner.go','pkg/scanner/inspect.go','pkg/scanner/git_cache_test.go','third_party/go-git/storage/filesystem/object.go','third_party/go-git/storage/filesystem/packfile_cache_close_regression_test.go','third_party/go-git/PROVENANCE.json','third_party/patches/go-git-reader-delta.patch','third_party/README.md']
def main():
 source=CACHE/'optimization-source';source.mkdir()
 bound=json.loads((OUT/'provenance-validation.json').read_text());archive=CACHE/'baseline/source.tar';assert digest(archive)==bound['frozen_source_archive']['sha256']
 with tarfile.open(archive) as a:a.extractall(source,filter='data')
 manifest=json.loads(gzip.decompress((OUT/'tracked-source-manifest.json.gz').read_bytes()))['files'];assert all(digest(source/p)==sha for p,sha in manifest.items())
 output=CACHE/'optimization';output.mkdir(exist_ok=True)
 common=['go','build','-mod=readonly','-buildvcs=false','-trimpath','-ldflags','-X dircue/internal/cli.Version=0.8.0','-o']
 rebuild=output/'baseline-rebuilt';subprocess.run([*common,str(rebuild),'.'],cwd=source,check=True)
 original=json.loads((OUT/'build-receipt.json').read_text())['binaries']['dircue']['sha256']
 if digest(rebuild)!=original:raise RuntimeError('Frozen archive rebuild differs from baseline executable; investigate before comparison')
 delta={}
 for name in FILES:
  target=source/name;target.parent.mkdir(parents=True,exist_ok=True);data=(ROOT/name).read_bytes();before=digest(target) if target.exists() else None;target.write_bytes(data);delta[name]={'before_sha256':before,'after_sha256':digest(target)}
 assert all(digest(source/p)==sha for p,sha in manifest.items() if p not in FILES)
 binary=output/'dircue';subprocess.run([*common,str(binary),'.'],cwd=source,check=True)
 save(OUT/'candidate-build.json',{'at':utc(),'base_commit':bound['frozen_source_archive']['commit'],'source_archive_sha256':digest(archive),'baseline_rebuild_sha256':digest(rebuild),'baseline_rebuild_identical':True,'candidate_sha256':digest(binary),'binary_path':str(binary),'source_path':str(source),'exact_input_delta':delta,'build_command':[*common,str(binary),'.'],'build_info':subprocess.check_output(['go','version','-m',str(binary)],text=True),'go_version':subprocess.check_output(['go','version'],text=True),'runtime_controls':{k:os.getenv(k) for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG','GOFLAGS','CGO_ENABLED']},'scope':'Only bounded packfile cache + required lifetime cleanup and its maintained-fork regression fix. Excludes concurrent CLI/schema changes. Identical v0.8.0 version override and baseline build flags.'})
 print('Frozen baseline reproduced byte-for-byte; isolated cache candidate built',flush=True)
if __name__=='__main__':main()
