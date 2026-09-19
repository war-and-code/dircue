#!/usr/bin/env python3
"""Prepare bounded synthetic rejection cases; import existing Syft reports only."""
import argparse, hashlib, json, pathlib

def sha(data):return hashlib.sha256(data).hexdigest()
def prepare(root, output):
 output.mkdir(parents=True,exist_ok=True)
 fixture=root/'tests/packageevidence/fixtures/syft-1.52.0.json'
 original=json.loads(fixture.read_text())
 template={'artifacts':[],'artifactRelationships':[],'files':[],'source':{'id':'source','name':'synthetic','version':'','type':'directory','metadata':{}},'descriptor':{'name':'syft','version':'1.52.0','configuration':{}},'schema':original['schema'],'distro':{}}
 cases=[]
 def add(name,document,limits=None,arguments=None,expected=None):
  raw=json.dumps(document,separators=(',',':')).encode()+b'\n';path=output/(name+'.json');path.write_bytes(raw)
  cases.append({'name':name,'path':str(path.resolve()),'input_sha256':sha(raw),'input_bytes':len(raw),'limits':limits or {},'arguments':arguments or [],'expected_error':expected})
 def package(i):return {'id':'p'+str(i),'name':'p','version':'','type':'x','foundBy':'x','language':'','purl':'','locations':[],'licenses':[],'cpes':[]}
 for level,count in [('default',20000),('hard',100000)]:
  limits={} if level=='default' else {'bytes':128<<20,'artifacts':100000}
  for position,index in [('first',0),('second',1),('middle',count//2),('growth-edge',8193 if level=='default' else 32769),('last',count-1)]:
   doc={**template,'artifacts':[package(i) if i<index else {} for i in range(count)]}
   add('packages-'+level+'-invalid-'+position,doc,limits,expected='invalid')
  add('packages-'+level+'-tiny-invalid-rows',{**template,'artifacts':[{}]*count},limits,expected='invalid')
 for level,count in [('default',50000),('hard',250000)]:
  limits={} if level=='default' else {'bytes':128<<20,'relationships':250000}
  for position,index in [('first',0),('second',1),('middle',count//2),('growth-edge',16385 if level=='default' else 65537),('last',count-1)]:
   rows=[{'parent':'source','child':'c'+str(i),'type':'contains'} if i<index else {} for i in range(count)]
   add('relationships-'+level+'-invalid-'+position,{**template,'artifactRelationships':rows},limits,expected='invalid')
  add('relationships-'+level+'-duplicates',{**template,'artifactRelationships':[{'parent':'source','child':'source','type':'contains'}]*count},limits)
 add('empty-report',template)
 actual=original
 add('actual-syft-fixture',actual)
 for count in [1000,20000]:
  path=root/('.cache/next-sprint/package-import-bench/synthetic-%d.json'%count)
  raw=path.read_bytes();cases.append({'name':'synthetic-'+str(count),'path':str(path.resolve()),'input_sha256':sha(raw),'input_bytes':len(raw),'limits':{},'arguments':[],'expected_error':None})
 for checks in [0,1,16,100]:add('cancellation-'+str(checks),actual,arguments=['--cancel-checks',str(checks)],expected='canceled' if checks<=16 else None)
 for count in [0,1,4096]:add('reader-error-'+str(count),actual,arguments=['--reader-error-after',str(count)],expected='reader_unexpected_eof')
 # Malformed spellings are kept byte-exact; no JSON reserialization repairs them.
 valid=json.dumps(template,separators=(',',':')).encode()
 for name,raw in [('duplicate-key',valid.replace(b'"artifacts":',b'"artifacts":[],"artifacts":',1)),('case-alias',valid.replace(b'"artifacts":',b'"Artifacts":',1)),('invalid-utf8',valid.replace(b'synthetic',b'\xff',1)),('trailing-json',valid+b'{}')]:
  path=output/(name+'.json');path.write_bytes(raw);cases.append({'name':name,'path':str(path.resolve()),'input_sha256':sha(raw),'input_bytes':len(raw),'limits':{},'arguments':[],'expected_error':'invalid'})
 (output/'cases.json').write_text(json.dumps(cases,indent=2)+'\n');print('prepared',len(cases),'cases')
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--root',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True);a=p.parse_args();prepare(a.root.resolve(),a.output.resolve())
