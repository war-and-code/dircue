#!/usr/bin/env python3
"""Bind a previously built scanner to identical accepted source; never rebuild it."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

ROOT=Path(__file__).resolve().parents[2]

def sha(data):return hashlib.sha256(data).hexdigest()
def source_path(name):return name in ('go.mod','go.sum','main.go') or name.startswith(('internal/','pkg/','third_party/go-enry/'))
def git(*args):return subprocess.run(['git',*args],cwd=ROOT,check=True,stdout=subprocess.PIPE).stdout


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--original-receipt',type=Path,required=True)
    p.add_argument('--binary',type=Path,required=True)
    p.add_argument('--accepted-commit',required=True)
    p.add_argument('--output',type=Path,required=True)
    a=p.parse_args();assert not a.output.exists(),'fresh receipt directory required'
    raw=a.original_receipt.read_bytes();old=json.loads(raw)
    assert old['complete'] and old.get('source_unchanged_after_build',old.get('source_stable_during_build',False))
    digest=sha(a.binary.read_bytes());assert digest==old['binary_sha256']
    commit=git('rev-parse',a.accepted_commit+'^{commit}').decode().strip();assert commit==a.accepted_commit,'use exact full commit ID'
    names=git('ls-tree','-r','--name-only','-z',commit).decode().split('\0')
    inventory={n:sha(git('show',commit+':'+n)) for n in names if source_path(n)}
    original={n:h for n,h in old['source_files_sha256'].items() if source_path(n)}
    assert inventory==original,'accepted root/fork source differs from original built source'
    # Keep the original receipt byte-for-byte; this new view makes no new-build claim.
    result={k:old[k] for k in ['binary_sha256','binary_modules','build_command','go_version','environment']}
    result.update(schema_version='1.0.0',complete=True,commit=commit,
        source_files_sha256=inventory,source_stable_during_build=True,
        normalization={'reused_binary':True,'new_build_performed':False,
            'original_receipt_file':'original-baseline-build-receipt.json','original_receipt_sha256':sha(raw),
            'original_source_base_commit':old.get('base_commit',old.get('commit')),
            'original_candidate_is_clean_commit':old.get('candidate_is_clean_commit'),
            'accepted_source_commit':commit,'accepted_source_file_count':len(inventory),
            'comparison':'All go.mod/go.sum/main.go and internal/, pkg/, third_party/go-enry/ paths and bytes exactly equal original built inventory.',
            'stability':'Build stability assertion is inherited from the preserved original build transaction, not a new compilation.',
            'normalizer_sha256':sha(Path(__file__).read_bytes())})
    a.output.mkdir(parents=True)
    (a.output/'original-baseline-build-receipt.json').write_bytes(raw)
    (a.output/'baseline-build-receipt.json').write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
    (a.output/'normalizer.py').write_bytes(Path(__file__).read_bytes())
    print(json.dumps({'complete':True,'accepted_source_commit':commit,'source_files':len(inventory),'reused_binary_sha256':digest,
        'original_receipt_sha256':sha(raw),'normalized_receipt_sha256':sha((a.output/'baseline-build-receipt.json').read_bytes())},indent=2))

if __name__=='__main__':main()
