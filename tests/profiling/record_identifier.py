#!/usr/bin/env python3
"""Record identifier-opt1 without rewriting RC1 or treating stale provenance as current."""
import argparse
import importlib.util
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location('rc1_library_auditor', ROOT/'tests/enry-performance/record_library.py')
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
SOURCE_FILES = ('internal/tokenizer/linguist.go', 'internal/tokenizer/linguist_identifier_test.go',
                'internal/tokenizer/linguist_reference_test.go')


def capture():
    original = ROOT/'tests/enry-performance/results/library-rc1/evidence.tar.gz'
    entries = base.unarchive(original.read_bytes())
    entries['experiment/rc1-evidence.sha256'] = (base.sha(original.read_bytes())+'\n').encode()
    for scenario in base.SCENARIOS:
        entries[f'experiment/rc1-{scenario}.json'] = entries[f'reports/{scenario}.json']
        entries[f'reports/{scenario}.json'] = (ROOT/f'.cache/identifier-opt1/library-{scenario}.json').read_bytes()
        for path in (ROOT/f'.cache/identifier-opt1/library-{scenario}-artifacts').glob('*.json'):
            entries[f'artifacts/{scenario}/{path.name}'] = path.read_bytes()
    entries['experiment/rc1-build-receipt.json'] = entries['provenance/build-receipt.json']
    builds = ROOT/'.cache/enry-builds-identifier-opt1'
    entries['provenance/build-receipt.json'] = (builds/'build-receipt.json').read_bytes()
    receipt = json.loads(entries['provenance/build-receipt.json'])
    captured = json.loads(entries['provenance/capture-check.json'])
    captured['binary_files_rehashed_at_capture'] = {name:base.sha((builds/name).read_bytes()) for name in base.NAMES}
    for name in base.NAMES:
        base.require(captured['binary_files_rehashed_at_capture'][name] == receipt['builds'][name]['sha256'], 'experimental binary changed')
    entries['provenance/capture-check.json'] = base.encode(captured)
    for name in ['execution-receipt.json', 'source-before-build.json', 'source-build-verification.json',
                 'samples.json', 'samples.md', 'samples.log', 'maintained-tests.log', 'tokenizer-tests.log']:
        entries['experiment/'+name] = (ROOT/'.cache/identifier-opt1'/name).read_bytes()
    before = json.loads(entries['experiment/source-before-build.json'])
    source = ROOT/'third_party/go-enry'
    actual = {p.relative_to(source).as_posix():base.sha(p.read_bytes()) for p in source.rglob('*') if p.is_file()}
    base.require(actual == before['files'], 'current source differs from experimental build inventory; capture exact original bytes before integration')
    for name in SOURCE_FILES:
        entries['experiment/source/'+name] = (source/name).read_bytes()
    entries['experiment/capture-check.json'] = base.encode({
        'actual_source_files_verified':actual,
        'retained_probe_sha256':base.sha((ROOT/'.cache/identifier-opt1/classifier-probe').read_bytes()),
        'source_policy':'Managed PROVENANCE remains the RC1 manifest and is not an attestation of experimental runtime source. Full actual file inventory was verified before/after building and at capture.',
    })
    entries['experiment/samples-runner.py'] = (ROOT/'tests/conformance/samples.py').read_bytes()
    entries['experiment/recorder.py'] = Path(__file__).read_bytes()
    return entries


def audit(entries):
    result = base.audit(entries)
    original = (ROOT/'tests/enry-performance/results/library-rc1/evidence.tar.gz').read_bytes()
    base.require(entries['experiment/rc1-evidence.sha256'].decode().strip() == base.sha(original), 'immutable RC1 archive changed')
    historical = base.unarchive(original)
    for scenario in base.SCENARIOS:
        base.require(entries[f'experiment/rc1-{scenario}.json'] == historical[f'reports/{scenario}.json'], 'historical RC1 report changed')
    base.require(entries['experiment/rc1-build-receipt.json'] == historical['provenance/build-receipt.json'], 'historical RC1 build changed')
    load = lambda name:json.loads(entries[name])
    before, after = load('experiment/source-before-build.json'), load('experiment/source-build-verification.json')
    checked = load('experiment/capture-check.json')
    receipt = load('provenance/build-receipt.json')
    oldreceipt = load('experiment/rc1-build-receipt.json')
    base.require(before['files'] == after['files'] == checked['actual_source_files_verified'], 'source inventory drift')
    base.require(after['source_stable_during_build'] and after['build_receipt_sha256'] == base.sha(entries['provenance/build-receipt.json']), 'build verification receipt')
    base.require(before['samples_sha256'] == after['samples_sha256'] == base.sha(entries['experiment/samples.json']), 'actual Ruby gate linkage')
    stale = load('provenance/fork.json')
    changed = {name for name,digest in stale['files'].items() if before['files'].get(name) != digest}
    added = set(before['files'])-set(stale['files'])-{'GENERATOR_WARNINGS.txt','PROVENANCE.json'}
    base.require(changed == {SOURCE_FILES[0]} and added == set(SOURCE_FILES[1:]), 'unexpected experimental source changes')
    for name in SOURCE_FILES:
        base.require(base.sha(entries['experiment/source/'+name]) == before['files'][name], 'experimental source snapshot')
    for name in receipt['builds']:
        if name != 'library-maintained':
            base.require(receipt['builds'][name]['sha256'] == oldreceipt['builds'][name]['sha256'], 'official binary changed: '+name)
    gate, execution = load('experiment/samples.json'), load('experiment/execution-receipt.json')
    base.require(execution['targeted_tests_passed'] and execution['maintained_full_tests_passed'] and execution['samples_gate_status']=='passed', 'correctness receipt failed')
    for name,digest in execution['files'].items():
        if name.startswith('third_party/go-enry/'):
            data = entries['experiment/source/'+name.removeprefix('third_party/go-enry/')]
        elif name.endswith('/classifier-probe'):
            base.require(digest == checked['retained_probe_sha256'] == gate['provenance']['probe_sha256'], 'rebuilt probe hash')
            continue
        else:
            data = entries['experiment/'+Path(name).name]
        base.require(base.sha(data)==digest, 'execution artifact linkage: '+name)
    oldgate = load('oracle/samples.json')
    oldrows = {r['path']:r for r in oldgate['results']}
    base.require(gate['provenance']['archive_sha256']==oldgate['provenance']['archive_sha256'] and gate['provenance']['upstream_git_ref']==oldgate['provenance']['upstream_git_ref'], 'Ruby source oracle changed')
    base.require(gate['provenance']['generator_sha256']==base.sha(entries['experiment/samples-runner.py']), 'sample harness changed')
    for key in ['Path','Version','Sum','GoModSum']:
        base.require(gate['provenance']['official_enry_module'][key]==oldgate['provenance']['official_enry_module'][key], 'official sample module changed')
    base.require('Replace' not in gate['provenance']['official_enry_module'], 'official sample module contaminated')
    base.require(len(gate['results'])==3388 and len({r['path'] for r in gate['results']})==3388 and gate['summary']['reference_errors']==0, 'Ruby sample count/errors')
    for row in gate['results']:
        old = oldrows[row['path']]
        base.require(row['reference']==old['reference'] and row['bytes']==old['bytes'] and row['enry']==old['enry'], 'reference sample changed')
        base.require(row['auragaze']==row['reference']['language'] and row['tokenizer']['hash']==row['reference']['token_hash'] and row['tokenizer']['count']==row['reference']['token_count'] and row['tokens_match'], 'actual Ruby label/token mismatch')
    result['experiment'] = {'phase':'experimental source; managed patch/provenance stale until integration',
        'source_before_sha256':base.sha(entries['experiment/source-before-build.json']),
        'source_after_sha256':base.sha(entries['experiment/source-build-verification.json']),
        'source_files_verified':len(before['files']), 'changed_runtime_files':sorted(changed), 'added_test_files':sorted(added),
        'sample_labels_and_ordered_tokens_match_ruby':3388, 'reference_errors':0,
        'retained_probe_sha256':checked['retained_probe_sha256'], 'official_timing_binaries_identical_to_rc1':True,
        'conformance_official_probe':{'rc1_sha256':oldgate['provenance']['official_enry_probe_sha256'], 'opt1_sha256':gate['provenance']['official_enry_probe_sha256'], 'same_binary':False, 'module_and_all_labels_identical':True, 'scope':'This separately rebuilt conformance probe is not a timing baseline; binary equality is not claimed.'}}
    for scenario,value in result['scenarios'].items():
        old = load(f'experiment/rc1-{scenario}.json')['summary']['library-maintained']
        candidate = value['cells']['library-maintained']
        value['historical_rc1'] = old
        value['historical_time_reduction_fraction'] = 1-candidate['warm']['median_seconds']/old['warm']['median_seconds']
        value['historical_allocation_reduction_fraction'] = 1-candidate['median_bytes_per_call']/old['median_bytes_per_call']
    result['evidence_members_sha256'] = {name:base.sha(data) for name,data in sorted(entries.items())}
    result['recorder_sha256'] = base.sha(Path(__file__).read_bytes())
    base.require(result['recorder_sha256']==base.sha(entries['experiment/recorder.py']), 'use archived recorder source')
    return result


def render(result):
    lines = ['# Identifier opt1 library measurements', '',
        '**The identifier experiment improves the historical RC1 measurements, but remains slower than the official library measured alongside it.** This is experimental source, not a managed release candidate.', '',
        '| Input | Historical RC1 median (s) | Opt1 median / p95 (s) | Current official median / p95 (s) | Historical time decrease | Opt1 slower than current official |',
        '|---|---:|---:|---:|---:|---:|']
    for scenario,value in result['scenarios'].items():
        current=value['cells']['library-maintained'];official=value['cells']['library-official'];old=value['historical_rc1']
        lines.append(f"| {scenario} | {old['warm']['median_seconds']:.6f} | {current['warm']['median_seconds']:.6f} / {current['warm']['p95_seconds']:.6f} | {official['warm']['median_seconds']:.6f} / {official['warm']['p95_seconds']:.6f} | {100*value['historical_time_reduction_fraction']:.1f}% | {(1/value['speedup_official_over_maintained']-1)*100:.1f}% |")
    lines += ['', '| Input | Library | First-pass median (s) | Warm bytes / allocations per call | Peak process RSS (MiB) | Warm CV |', '|---|---|---:|---:|---:|---:|']
    for scenario,value in result['scenarios'].items():
        for name,cell in value['cells'].items():
            lines.append(f"| {scenario} | {name.removeprefix('library-')} | {cell['median_first_pass_seconds']:.3f} | {cell['median_bytes_per_call']:,.0f} / {cell['median_allocations_per_call']:.1f} | {cell['warm']['peak_rss_kib']/1024:.1f} | {cell['warm']['coefficient_of_variation']:.3f} |")
    lines += ['', 'All four cells retain 20 paired samples after 3 symmetric warmups and exceed 10 seconds of warm measurement. No timed sample was discarded. Official versus opt1 measurements are paired within each run; comparisons against RC1 are **historical separate windows**, not a paired before/after experiment. Both current scenarios are `ParityToMargin` under the declared three-tier policy, despite remaining slower than official Enry; neither is a speed win.', '',
        'The identical preloaded GetLanguage API sees all 3,388 original sample files. Full content is 28,231,769 bytes; the 128 KiB window is 22,922,457 bytes. All 3,388 actual Ruby full-content labels and ordered token hashes/counts match, with zero reference errors. The retained rebuilt probe hash matches the gate receipt. Both runtime label maps match the unchanged RC1 maps, preserving all 147 differences from official Enry. There is no independent prefix Ruby oracle.', '',
        'The managed `PROVENANCE.json` intentionally still describes RC1 at experiment time. It must not be used as proof of opt1 source. Separate before/after build inventories and capture hashes verify all 42 actual fork files: one runtime file changed and two tests were added. Those exact changed files, correctness logs, source inventories, build receipt, original reports and stdout artifacts are archived. All official timing binaries are byte-identical to RC1; the maintained experiment binary differs. The separately rebuilt conformance official probe has a different hash but identical pinned module/sums and all labels; it is not one of the timing binaries. Managed regeneration and broader pipeline acceptance remain separate work.', '',
        'Warm allocation counters exclude setup; first pass includes lazy initialization; process RSS/CPU include preload and setup. The same optimized Go 1.26.6 static Linux arm64 drivers, source hash, runtime controls, manifests and five-vCPU Docker VM were used. VM host governor/turbo/SMT and unrelated applications were not experimentally isolated; only one window per input was measured. No universal speed or three-window stability claim is made. See `honest-gate.json` for explicit waivers.', '',
        'The archive retains original RC1 reports as historical comparators without changing the separately recorded RC1 evidence. Audit replay performs no build, profile, or benchmark:', '', '```sh', 'python3 tests/profiling/record_identifier.py --audit-only', '```', '']
    return '\n'.join(lines).encode()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',type=Path,default=HERE/'results/identifier-opt1')
    parser.add_argument('--audit-only',action='store_true')
    args=parser.parse_args()
    if args.audit_only:
        sums=json.loads((args.output/'SHA256SUMS.json').read_text())
        base.require(set(sums)=={'README.md','audit.json','honest-gate.json','evidence.tar.gz'},'unexpected output file list')
        for name,digest in sums.items():base.require(base.sha((args.output/name).read_bytes())==digest,'record artifact hash')
        entries=base.unarchive((args.output/'evidence.tar.gz').read_bytes())
    else:
        base.require(not args.output.exists(),'fresh output required')
        entries=capture()
    result=audit(entries)
    gate=base.attestation()
    gate.update(scenario='Experimental identifier opt1 vs official Enry, with separate historical RC1 comparator')
    gate['questions']['11_three_tier_reporting']='pass: both current paired scenarios ParityToMargin but slower than official; historical decreases are explicitly separate windows'
    gate['questions']['14_reproducible']='waive: experimental managed provenance stale; actual source inventories and changed files retained. Managed regeneration and three repeat windows remain pending.'
    outputs={'README.md':render(result),'audit.json':base.encode(result),'honest-gate.json':base.encode(gate)}
    if args.audit_only:
        for name,data in outputs.items():base.require((args.output/name).read_bytes()==data,'re-audit differs: '+name)
    else:
        args.output.mkdir(parents=True)
        outputs['evidence.tar.gz']=base.archive(entries)
        for name,data in outputs.items():(args.output/name).write_bytes(data)
        (args.output/'SHA256SUMS.json').write_bytes(base.encode({name:base.sha(data) for name,data in outputs.items()}))
    print('PASS: opt1 80 timing samples, 3388 Ruby labels/tokens, actual source inventories, unchanged official timing binaries; RC1 retained.')


if __name__=='__main__':main()
