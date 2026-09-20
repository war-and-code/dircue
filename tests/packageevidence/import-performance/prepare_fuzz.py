#!/usr/bin/env python3
"""Materialize an explicit old revision for a temporary differential fuzz overlay."""
import argparse,json,pathlib,subprocess
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--baseline',required=True);a=p.parse_args()
root=pathlib.Path.cwd();out=root/'.cache/import-capacity';target=out/'baseline';target.mkdir(parents=True,exist_ok=True)
for old in target.glob('*.go'):
 old.unlink()
paths=subprocess.check_output(['git','ls-tree','-r','--name-only',a.baseline,'pkg/packageevidence'],text=True).splitlines()
for name in paths:
 if name.endswith('.go') and not name.endswith('_test.go'):(target/pathlib.Path(name).name).write_bytes(subprocess.check_output(['git','show',a.baseline+':'+name]))
source=root/'tests/packageevidence/import-performance/differential_test.go.txt'
overlay={'Replace':{str(root/'pkg/packageevidence/import_capacity_differential_test.go'):str(source)}}
(out/'overlay.json').write_text(json.dumps(overlay,indent=2)+'\n')
print(out/'overlay.json')
