#!/usr/bin/env python3
"""Bind retained initial samples and scanner profiles to the unchanged v0.8.0 sources."""
import hashlib,json,os,subprocess
from pathlib import Path
from benchmark import ROOT,OUT,CACHE,EXPECTED,digest,save,utc

def main():
 if (OUT/'tracked-source-manifest.json.gz').exists() or (OUT/'provenance-validation.json').exists(): raise RuntimeError('Baseline binding is immutable; refusing to overwrite historical source/validation receipts')
 receipt=json.loads((OUT/'build-receipt.json').read_text())
 checks={name:digest(CACHE/'baseline'/name)==entry['sha256'] for name,entry in receipt['binaries'].items()}
 checks['head']=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()==EXPECTED
 checks['production_inputs']=all(digest(ROOT/name)==sha for name,sha in receipt['production_inputs'].items())
 checks['tracked_worktree_clean']=not subprocess.check_output(['git','diff','HEAD','--'],cwd=ROOT)
 env={k:os.getenv(k) for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG','CGO_ENABLED','GOFLAGS']}
 checks['runtime_controls_match_prepare']=env==json.loads((OUT/'host-receipt.json').read_text())['environment']
 tracked=subprocess.check_output(['git','ls-files','-z'],cwd=ROOT).decode().split('\0')
 # Check everything before publishing either immutable source-binding receipt.
 if not all(checks.values()): raise RuntimeError(checks)
 # Broad source receipt includes scanner tests and embedded/test fixture inputs.
 sources={p:digest(ROOT/p) for p in tracked if p and (ROOT/p).is_file()}
 save(OUT/'tracked-source-manifest.json.gz',{'head':EXPECTED,'captured_at':utc(),'binding':'post-CLI/pre-profile snapshot; verified git diff HEAD empty. Baseline binaries were built before timing while root and audit agents made no production/test edits. This is retrospective corroboration, not a contemporaneous hermetic build attestation.','files':sources})
 checks['all_passed']=all(checks.values())
 result={'at':utc(),'checks':checks,'environment':env,'initial_measurement_note':'Initial CLI harness had prepare-time host controls, exact output/empty-stderr/exit checks and pre/post executable digest equality, but did not compare digest to prepare receipt inside collect. This validator explicitly binds current binary to original receipt and verifies source/test files against released HEAD. No relevant controls were changed during collection; provenance remains retrospective. Raw original samples are retained. Timeout cleanup was hardened while the initial process was running; no timeout occurred.','hardened_harness_sha256':{p.name:digest(p) for p in OUT.glob('*.py')}}
 save(OUT/'provenance-validation.json',result)
 if not checks['all_passed']:raise RuntimeError(checks)
 print(json.dumps(checks))
if __name__=='__main__':main()
