#!/usr/bin/env python3
"""Verify final-candidate performance receipts without executing the candidate."""
import argparse
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import statistics

ROOT=Path(__file__).resolve().parents[3]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def verify_registry(path):
    report=json.loads(gzip.decompress(path.read_bytes()))
    assert report['passed'] and report['method']['repetitions']==5
    assert sorted(c['name'] for c in report['cases'])==['cobra-directory','roslyn-directory','synthetic-registry-project','xml-and-dotnet']
    assert report['candidate_sha256']==report['candidate_build_provenance']['candidate_sha256']
    total=0
    for case in report['cases']:
        outputs={}
        for key in ('projects','projects_registries','registries'):
            name=f'registry-{case["name"]}-{key}.json.gz'
            compressed=(path.parent/name).read_bytes()
            assert sha(compressed)==case['artifacts'][name]
            payload=gzip.decompress(compressed)
            outputs[key]=json.loads(payload)
            samples=case['samples'][key]
            assert [s['round'] for s in samples]==[1,2,3,4,5]
            assert case['execution_order'].count(key)==5
            for sample in [case['warmups'][key],*samples]:
                assert sample['stdout_sha256']==sha(payload)
                assert sample['stderr_sha256']==sha(b'') and sample['exit_code']==0
                assert sample['seconds']>0 and sample['peak_rss_bytes']>0
            assert case['summary'][key]=={'median_seconds':statistics.median(s['seconds'] for s in samples),'median_peak_rss_bytes':statistics.median(s['peak_rss_bytes'] for s in samples)}
            total+=len(samples)
        old,new,only=(outputs[k] for k in ('projects','projects_registries','registries'))
        assert old['schema_version']=='1.2.0' and new['schema_version']==only['schema_version']=='1.3.0'
        assert {k:v for k,v in old.items() if k!='schema_version'}=={k:v for k,v in new.items() if k not in ('schema_version','registries')}
        assert new['registries']==only['registries']==case['registries']
        module=only['registries']
        assert [c['path'] for c in module['configurations']]==case['expected_configurations']
        assert module['coverage']['bytes_read']==case['expected_content_bytes']
        assert module['coverage']['candidate_files']==module['coverage']['admitted_files']==module['coverage']['read_files']==len(case['expected_configurations'])
        for field in ('external_configuration','environment_expansion','network_access'):
            assert module['scope'][field] is False
        baseline,candidate=(case['summary'][k] for k in ('projects','projects_registries'))
        assert case['incremental']=={
            'median_seconds_added':candidate['median_seconds']-baseline['median_seconds'],
            'median_time_change_percent':100*(candidate['median_seconds']/baseline['median_seconds']-1),
            'median_rss_bytes_added':candidate['median_peak_rss_bytes']-baseline['median_peak_rss_bytes'],
            'paired_time_change_percent':[100*(n['seconds']/o['seconds']-1) for o,n in zip(case['samples']['projects'],case['samples']['projects_registries'])]}
    return total


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--default',type=Path,required=True)
    parser.add_argument('--registries',type=Path,required=True)
    args=parser.parse_args()
    spec=importlib.util.spec_from_file_location('default_verifier',ROOT/'tests/performance/default_paths/verify.py')
    verifier=importlib.util.module_from_spec(spec);spec.loader.exec_module(verifier)
    print(verifier.verify(args.default),'ordinary-path samples verified')
    print(verify_registry(args.registries),'registry-cost samples verified')
