#!/usr/bin/env python3
"""Capture/audit real opt2 receipts; normalize warm passes, never amortize cold overhead."""
import argparse
import importlib.util
import json
import math
from pathlib import Path
import statistics as stats

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location('library_evidence_helpers', ROOT/'tests/enry-performance/record_library.py')
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
require, equal, sha, encode = helper.require, helper.equal, helper.sha, helper.encode
NAMES = helper.NAMES
SCENARIOS = ('full', 'prefix')
RUNTIME = 'internal/tokenizer/linguist.go'
NEW_TEST = 'internal/tokenizer/linguist_subset_test.go'


def capture(args):
    require(not args.output.exists(), 'capture requires a fresh output directory')
    entries = {}
    for label, folder in [('rc1', ROOT/'tests/enry-performance/results/library-rc1'),
                          ('opt1', ROOT/'tests/profiling/results/identifier-opt1')]:
        raw = (folder/'evidence.tar.gz').read_bytes()
        hashes = json.loads((folder/'SHA256SUMS.json').read_text())
        require(sha(raw) == hashes['evidence.tar.gz'], 'historical archive hash: '+label)
        historical = helper.unarchive(raw)
        entries[f'history/{label}/archive.sha256'] = (sha(raw)+'\n').encode()
        entries[f'history/{label}/build.json'] = historical['provenance/build-receipt.json']
        for scenario in SCENARIOS:
            entries[f'history/{label}/{scenario}.json'] = historical[f'reports/{scenario}.json']
        if label == 'rc1':
            entries['oracle/rc1-samples.json'] = historical['oracle/samples.json']
        else:
            entries['history/opt1/source.json'] = historical['experiment/source-before-build.json']
    for scenario, manifest_name in [('full', 'linux-full.json'), ('prefix', 'linux-prefix-128k.json')]:
        entries[f'reports/{scenario}.json'] = (args.experiment/f'library-{scenario}.json').read_bytes()
        entries[f'inputs/{scenario}.json'] = (args.inputs/manifest_name).read_bytes()
        for path in (args.experiment/f'library-{scenario}-artifacts').glob('*.json'):
            entries[f'artifacts/{scenario}/{path.name}'] = path.read_bytes()
    for name in ['source-before-build.json', 'source-build-verification.json', 'base-receipt.json',
                 'experiment-receipt.json', 'proof.md', 'subset.patch', 'ruleids-test-only.patch']:
        entries['experiment/'+name] = (args.experiment/name).read_bytes()
    if args.diagnostic:
        for name in ['library-full.json', 'source-before-build.json', 'source-build-verification.json']:
            entries['diagnostic/retained-ids/'+name] = (args.diagnostic/name).read_bytes()
        for path in (args.diagnostic/'library-full-artifacts').glob('*.json'):
            entries['diagnostic/retained-ids/artifacts/'+path.name] = path.read_bytes()
        entries['diagnostic/retained-ids/samples.json'] = (args.diagnostic/'validation/samples.json').read_bytes()
        entries['diagnostic/retained-ids/build-receipt.json'] = (args.diagnostic_builds/'build-receipt.json').read_bytes()
    for path in args.validation.glob('*'):
        if path.is_file() and path.suffix in ('.json', '.md', '.log'):
            entries['validation/'+path.name] = path.read_bytes()
    entries['build/receipt.json'] = (args.builds/'build-receipt.json').read_bytes()
    entries['source/managed-PROVENANCE.json'] = (args.source/'PROVENANCE.json').read_bytes()
    before = json.loads(entries['experiment/source-before-build.json'])
    actual = {p.relative_to(args.source).as_posix():sha(p.read_bytes()) for p in args.source.rglob('*') if p.is_file()}
    require(actual == before['files'], 'actual source differs from before-build inventory; capture original source before integration')
    # Retain every changed/added file relative to the committed opt1 experiment.
    old = json.loads(entries['history/opt1/source.json'])['files']
    snapshots = {name for name,digest in actual.items() if old.get(name) != digest}
    snapshots |= {RUNTIME, NEW_TEST, 'internal/tokenizer/linguist_identifier_test.go', 'internal/tokenizer/linguist_reference_test.go'}
    for name in sorted(snapshots):
        entries['source/fork/'+name] = (args.source/name).read_bytes()
    for name in ['library.py', 'compare.py', 'library.go.txt', 'build.py']:
        entries['harness/'+name] = (ROOT/'tests/enry-performance'/name).read_bytes()
    entries['harness/measurement.py'] = (ROOT/'tests/stress/compare.py').read_bytes()
    entries['harness/samples.py'] = (ROOT/'tests/conformance/samples.py').read_bytes()
    entries['harness/helpers.py'] = Path(helper.__file__).read_bytes()
    entries['harness/recorder.py'] = Path(__file__).read_bytes()
    receipt = json.loads(entries['build/receipt.json'])
    binaries = {name:sha((args.builds/name).read_bytes()) for name in receipt['builds']}
    for name,digest in binaries.items():
        require(digest == receipt['builds'][name]['sha256'], 'binary changed after build: '+name)
    execution = json.loads(entries['validation/execution-receipt.json'])
    linked = {}
    for key,digest in execution['files'].items():
        if key.startswith('third_party/go-enry/'):
            relative = key.removeprefix('third_party/go-enry/')
            destination = 'source/fork/'+relative
            data = (args.source/relative).read_bytes()
        elif key == 'tests/conformance/samples.py':
            destination, data = 'harness/samples.py', entries['harness/samples.py']
        elif Path(key).name == 'classifier-probe':
            linked[key] = {'binary_sha256':sha((args.validation/'classifier-probe').read_bytes())}
            require(linked[key]['binary_sha256'] == digest, 'validation probe changed')
            continue
        else:
            relative = Path(key).name
            path = args.validation/relative
            if not path.is_file():
                path = args.experiment/relative
            destination, data = 'validation/linked/'+relative, path.read_bytes()
        require(sha(data) == digest, 'validation artifact changed: '+key)
        require(destination not in entries or entries[destination] == data, 'artifact filename collision')
        entries[destination] = data
        linked[key] = {'artifact':destination}
    entries['capture.json'] = encode({'actual_source_files':actual, 'binaries':binaries,
        'probe_sha256':sha((args.validation/'classifier-probe').read_bytes()), 'validation_links':linked,
        'source_policy':'Experimental actual file inventories, not the stale managed manifest, identify final opt2. subset.patch and experiment-receipt.json describe the initial source-only proposal; ruleids-test-only.patch records its subsequent cleanup.'})
    return entries


def verify_metrics(report, manifest, entries, scenario):
    require(report['complete'] and not report['profiled'] and report['api']=='language' and report['workers']==1, 'not a complete unprofiled GetLanguage scenario')
    passes = report['iterations']
    require(isinstance(passes, int) and passes >= 1, 'invalid warm corpus pass count')
    paths = [f['path'] for f in manifest['files']]
    require(len(paths)==len(set(paths))==3388, 'incomplete sample population')
    require(report['manifest_sha256']==sha(entries[f'inputs/{scenario}.json']), 'input manifest hash')
    require(manifest['prefix']==report['prefix']==(0 if scenario=='full' else 131072), 'input window')
    for key,name in [('harness_sha256','library.py'), ('shared_runner_sha256','compare.py'), ('measurement_helper_sha256','measurement.py')]:
        require(report[key]==sha(entries['harness/'+name]), 'measurement harness changed')
    require(report['methodology']['warmup']>=3 and report['methodology']['runs']>=20, 'insufficient declared warmup/sample floor')
    controls = report['environment']['runtime_controls']
    require(controls['GOGC']=='100' and controls['GOMEMLIMIT']=='off' and controls['GODEBUG'] is None, 'runtime control drift')
    cells, label_maps = {}, {}
    for name in NAMES:
        correctness = report['correctness'][name]
        raw = entries[f'artifacts/{scenario}/'+correctness['artifact']]
        require(sha(raw)==correctness['sha256'], 'correctness artifact hash')
        process = json.loads(raw)
        require(process['exit_code']==0 and not process['timed_out'], 'correctness process failed')
        pilot = json.loads(process['stdout'])
        labels = pilot['labels']
        require(set(labels)==set(paths) and all(isinstance(v,str) for v in labels.values()), 'label population')
        digest = helper.checksum(paths, labels)
        require(digest==correctness['checksum']==pilot['checksum'], 'ordered output checksum')
        label_maps[name] = labels
        # Correctness pilot always performs one pass, independent of calibration.
        values = report['results'][name]
        require(len(values)>=20 and len(values)<=report['methodology']['max_runs'], 'invalid retained sample count')
        for sample, expected_passes in [(pilot,1)]+[(sample,passes) for sample in values]:
            for key, expected in [('api','language'), ('workers',1), ('profiled',False), ('files',len(paths)),
                                  ('iterations',expected_passes), ('calls',expected_passes*len(paths)),
                                  ('checksum',digest), ('manifest_sha256',report['manifest_sha256']),
                                  ('prefix',report['prefix']), ('input_bytes',sum(f['measured_bytes'] for f in manifest['files']))]:
                equal(sample[key], expected, 'sample identity: '+key)
            for key in ['warm_seconds','first_pass_seconds','preload_seconds','ns_per_call']:
                require(math.isfinite(sample[key]) and sample[key]>0, 'invalid elapsed metric')
            require(sample['allocated_bytes']>=0 and sample['allocations']>=0, 'invalid allocation metric')
            equal(sample['ns_per_call'], sample['warm_seconds']*1e9/sample['calls'], 'ns per call')
        for sample in values:
            require(all(math.isfinite(sample['process'][key]) and sample['process'][key]>=0 for key in ['seconds','user_seconds','system_seconds','max_rss_kib']), 'invalid process resources')
            require(sample['process']['seconds']>=sample['warm_seconds'], 'process shorter than contained warm timer')
        warm = [v['warm_seconds'] for v in values]
        rss = [v['process']['max_rss_kib'] for v in values]
        summary = {'warm':helper.summary(warm,rss), 'process':helper.summary([v['process']['seconds'] for v in values],rss),
                   'median_ns_per_call':stats.median(v['ns_per_call'] for v in values),
                   'median_bytes_per_call':stats.median(v['allocated_bytes']/v['calls'] for v in values),
                   'median_allocations_per_call':stats.median(v['allocations']/v['calls'] for v in values),
                   'median_first_pass_seconds':stats.median(v['first_pass_seconds'] for v in values),
                   'sample_floor_met':sum(warm)>=10}
        require(summary['sample_floor_met'], 'less than ten warm measurement seconds')
        for key,expected in summary.items():
            if isinstance(expected,dict):
                for metric,value in expected.items():equal(report['summary'][name][key][metric],value,'summary metric')
            else:equal(report['summary'][name][key],expected,'summary metric')
        summary.update(warm_passes_per_process=passes,
            warm_per_corpus_pass=helper.summary([v/passes for v in warm],rss),
            first_pass=helper.summary([v['first_pass_seconds'] for v in values],rss),
            nonwarm_process=helper.summary([v['process']['seconds']-v['warm_seconds'] for v in values],rss),
            median_cpu_seconds=stats.median(v['process']['user_seconds']+v['process']['system_seconds'] for v in values),
            median_preload_seconds=stats.median(v['preload_seconds'] for v in values),
            measured_warm_seconds=sum(warm), gomaxprocs=sorted({v['gomaxprocs'] for v in values}))
        require(len(summary['gomaxprocs'])==1, 'runtime parallelism changed during samples')
        cells[name] = summary
    require(len(report['results'][NAMES[0]])==len(report['results'][NAMES[1]]), 'unequal paired samples')
    require(cells[NAMES[0]]['gomaxprocs']==cells[NAMES[1]]['gomaxprocs'], 'asymmetric runtime parallelism')
    left,right = ([v['warm_seconds'] for v in report['results'][name]] for name in NAMES)
    ratio = stats.median(left)/stats.median(right)
    equal(report['warm_speedup'],ratio,'paired median ratio')
    interval = helper.bootstrap(left,right)
    require(len(report['paired_bootstrap_95_ci'])==2, 'confidence interval shape')
    for actual,expected in zip(report['paired_bootstrap_95_ci'],interval):equal(actual,expected,'paired bootstrap interval')
    differences = [{'path':path,'official':label_maps[NAMES[0]][path],'maintained':label_maps[NAMES[1]][path]} for path in sorted(paths) if label_maps[NAMES[0]][path]!=label_maps[NAMES[1]][path]]
    require(differences==report['label_differences'], 'dropped or changed label differences')
    current_p95 = cells[NAMES[1]]['warm_per_corpus_pass']['p95_seconds']
    official_p95 = cells[NAMES[0]]['warm_per_corpus_pass']['p95_seconds']
    verdict = 'faster with margin' if ratio>=1.10 and interval[0]>1 and current_p95<=official_p95 else 'slower with margin' if ratio<=1/1.10 and interval[1]<1 else 'inconclusive'
    process_left,process_right = ([v['process']['seconds'] for v in report['results'][name]] for name in NAMES)
    return {'cells':cells, 'speedup_official_over_candidate':ratio, 'paired_bootstrap_95_ci':interval,
            'warm_verdict':verdict, 'process_speedup_official_over_candidate':stats.median(process_left)/stats.median(process_right),
            'process_paired_bootstrap_95_ci':helper.bootstrap(process_left,process_right),
            'label_differences':differences, 'warm_classification':'BelowParity' if ratio<.8 else 'HealthyMargin' if ratio>=1.10 else 'ParityToMargin'}, label_maps


def audit(entries):
    load = lambda name:json.loads(entries[name])
    require(sha(entries['harness/helpers.py'])==sha(Path(helper.__file__).read_bytes()), 'use retained helper source')
    require(sha(entries['harness/recorder.py'])==sha(Path(__file__).read_bytes()), 'use retained recorder source')
    before,after,capture = load('experiment/source-before-build.json'), load('experiment/source-build-verification.json'), load('capture.json')
    require(before['files']==after['files']==capture['actual_source_files'], 'source inventory changed during/after build')
    require(after['source_stable_during_build'] and after['build_receipt_sha256']==sha(entries['build/receipt.json']), 'before/after build receipt linkage')
    previous = load('history/opt1/source.json')['files']
    changed = {name for name,digest in previous.items() if before['files'].get(name)!=digest}
    added = set(before['files'])-set(previous)
    require(changed=={RUNTIME,'PROVENANCE.json'} and added=={NEW_TEST}, 'unexpected changes relative to opt1 source inventory')
    require(not set(previous)-set(before['files']), 'source file disappeared')
    for name in changed|added:
        require(sha(entries['source/fork/'+name])==before['files'][name], 'actual source snapshot hash')
    managed = load('source/managed-PROVENANCE.json')
    require(sha(entries['source/managed-PROVENANCE.json'])==before['files']['PROVENANCE.json'], 'managed manifest identity')
    require({name for name,digest in managed['files'].items() if before['files'].get(name)!=digest}=={RUNTIME}, 'unexpected stale managed-source differences')
    require(set(before['files'])-set(managed['files'])-{'PROVENANCE.json','GENERATOR_WARNINGS.txt','README.md'}=={NEW_TEST}, 'unexpected unmanaged files')
    receipt = load('build/receipt.json')
    require(receipt['complete'] and not receipt['profile'] and receipt['goos']=='linux' and receipt['goarch']=='arm64', 'wrong build purpose/platform')
    require(receipt['builder_sha256']==sha(entries['harness/build.py']), 'builder source drift')
    require(receipt['toolchain_selection']['GOTOOLCHAIN']=='go1.26.6', 'compiler selection drift')
    for name,build in receipt['builds'].items():
        require(capture['binaries'][name]==build['sha256'], 'captured binary hash')
        if name!='library-maintained':
            for older in ['rc1','opt1']:
                require(build['sha256']==load(f'history/{older}/build.json')['builds'][name]['sha256'], 'official timing binary changed')
    for name in NAMES:
        build = receipt['builds'][name]
        require(build['driver_sha256']==sha(entries['harness/library.go.txt']), 'asymmetric driver source')
        require('go1.26.6' in build['binary_modules'] and 'CGO_ENABLED=0' in build['binary_modules'], 'compiled toolchain drift')
    require(receipt['builds'][NAMES[0]]['command'][1:-2]==receipt['builds'][NAMES[1]]['command'][1:-2], 'asymmetric build flags')
    require('Replace' not in receipt['builds'][NAMES[0]]['module'], 'official module replacement contamination')
    require(receipt['builds'][NAMES[0]]['module']['Sum']==receipt['pins']['library']['sum'], 'official module checksum')
    require(receipt['builds'][NAMES[1]]['fork_provenance_sha256']==sha(entries['source/managed-PROVENANCE.json']), 'stale managed manifest linkage')
    execution,gate,reference = load('validation/execution-receipt.json'),load('validation/samples.json'),load('oracle/rc1-samples.json')
    require(execution['maintained_full_tests_status']=='passed' and execution['samples_gate_status']=='passed', 'correctness work still pending or failed')
    require(execution.get('completed_at_utc') is not None, 'unfinished correctness receipt')
    require(execution['source_manifest_unchanged_after_validation'], 'validation source changed')
    require(execution['source_manifest_sha256']==sha(entries['validation/source-manifest.json']), 'validation source manifest hash')
    validation_source = load('validation/source-manifest.json')['source_files']
    require({name.removeprefix('third_party/go-enry/'):digest for name,digest in validation_source.items() if name.startswith('third_party/go-enry/')}==before['files'], 'validation/build source mismatch')
    require(after['samples_sha256']==sha(entries['validation/samples.json']), 'build verification/sample gate linkage')
    for key,digest in execution['files'].items():
        linked = capture['validation_links'][key]
        require((linked.get('binary_sha256') or sha(entries[linked['artifact']]))==digest, 'execution artifact linkage')
    require(capture['probe_sha256']==gate['provenance']['probe_sha256'], 'rebuilt validation probe identity')
    require(gate['provenance']['generator_sha256']==sha(entries['harness/samples.py']), 'actual sample harness identity')
    for key in ['archive_sha256','upstream_git_ref']:
        require(gate['provenance'][key]==reference['provenance'][key], 'Ruby population provenance drift')
    require(len(gate['results'])==len({r['path'] for r in gate['results']})==3388 and gate['summary']['reference_errors']==0, 'incomplete Ruby population')
    prior = {r['path']:r for r in reference['results']}
    for row in gate['results']:
        require(row['reference']==prior[row['path']]['reference'] and row['bytes']==prior[row['path']]['bytes'] and row['enry']==prior[row['path']]['enry'], 'reference bytes/labels/tokens changed')
        require(row['auragaze']==row['reference']['language'] and row['tokens_match'] and row['tokenizer']['hash']==row['reference']['token_hash'] and row['tokenizer']['count']==row['reference']['token_count'], 'actual Ruby label/token mismatch')
    output = {'complete':True, 'phase':'experimental subset source, pending managed integration and broader release acceptance',
        'proposal_receipt_scope':'Initial source-only proposal, superseded by the test-only rule-ID cleanup. Final source identity comes from matching actual inventories before/after build and at capture.',
        'proposal_patch_sha256':sha(entries['experiment/subset.patch']),
        'cleanup_patch_sha256':sha(entries['experiment/ruleids-test-only.patch']),
        'actual_source_file_count':len(before['files']), 'source_changes_from_opt1':sorted(changed), 'source_additions':sorted(added),
        'official_timing_binaries_identical_to_rc1_and_opt1':True, 'actual_ruby_labels_and_ordered_tokens':3388,
        'probe_sha256':capture['probe_sha256'], 'scenarios':{}, 'historical_comparison_scope':'Separate windows. Warm time normalized per complete corpus pass; raw process time comparable only for equal warm-pass counts.'}
    for scenario in SCENARIOS:
        report,manifest = load(f'reports/{scenario}.json'),load(f'inputs/{scenario}.json')
        require(report['build_receipt']==receipt,'report/build linkage')
        cell,labels = verify_metrics(report,manifest,entries,scenario)
        expected_labels = {r['path']:r['reference']['language'] or '' for r in gate['results']}
        # Prefix agreement is recorded against full labels, never called a prefix Ruby oracle.
        require(labels['library-maintained']==expected_labels,'candidate library labels changed from validated full corpus')
        require(labels['library-official']=={r['path']:r['enry'] for r in gate['results']}, 'official library labels changed from validated population')
        cell['ruby_oracle_scope']='full-content actual oracle' if scenario=='full' else 'No independent prefix Ruby oracle; these label maps equal the full-content maps.'
        cell['historical'] = {}
        current = cell['cells']['library-maintained']
        for older in ['rc1','opt1']:
            old = load(f'history/{older}/{scenario}.json')
            require(old['manifest_sha256']==report['manifest_sha256'] and old['workers']==report['workers'], 'historical workload differs')
            old_values = old['results']['library-maintained']
            old_passes = old['iterations']
            normalized_old = stats.median(v['warm_seconds']/v['iterations'] for v in old_values)
            first_old = stats.median(v['first_pass_seconds'] for v in old_values)
            process_old = stats.median(v['process']['seconds'] for v in old_values)
            same_passes = old_passes==report['iterations']
            cell['historical'][older] = {'warm_passes_per_process':old_passes,'warm_per_corpus_pass_median':normalized_old,
                'warm_time_reduction_fraction':1-current['warm_per_corpus_pass']['median_seconds']/normalized_old,
                'first_pass_median':first_old,'first_pass_change_fraction':current['first_pass']['median_seconds']/first_old-1,
                'process_median':process_old,'process_comparable':same_passes,
                'process_change_fraction':current['process']['median_seconds']/process_old-1 if same_passes else None,
                'peak_process_rss_kib':max(v['process']['max_rss_kib'] for v in old_values),
                'note':'First-pass and RSS scopes are retained; process timings are not divided by iterations because that would amortize setup differently.'}
        output['scenarios'][scenario] = cell
    # The first completed variant retained test-only rule IDs in runtime state.
    # Keep its full run independently auditable; never pool it with final samples.
    diagnostic_prefix = 'diagnostic/retained-ids/'
    if diagnostic_prefix+'library-full.json' in entries:
        old_report = load(diagnostic_prefix+'library-full.json')
        old_before = load(diagnostic_prefix+'source-before-build.json')
        old_after = load(diagnostic_prefix+'source-build-verification.json')
        require(old_before['files']==old_after['files'] and old_after['source_stable_during_build'], 'diagnostic source changed during build')
        require(old_after['build_receipt_sha256']==sha(entries[diagnostic_prefix+'build-receipt.json']), 'diagnostic build linkage')
        require(old_report['build_receipt']==load(diagnostic_prefix+'build-receipt.json'), 'diagnostic report/build linkage')
        require(old_after['samples_sha256']==sha(entries[diagnostic_prefix+'samples.json']), 'diagnostic correctness linkage')
        old_gate = load(diagnostic_prefix+'samples.json')
        require(len(old_gate['results'])==3388 and old_gate['summary']['reference_errors']==0, 'diagnostic correctness incomplete')
        require({r['path']:r['reference'] for r in old_gate['results']}=={r['path']:r['reference'] for r in gate['results']}, 'diagnostic Ruby oracle changed')
        require(all(r['tokens_match'] and r['auragaze']==r['reference']['language'] for r in old_gate['results']), 'diagnostic label/token mismatch')
        require(old_before['files'][RUNTIME]!=before['files'][RUNTIME], 'diagnostic variant must remain distinct')
        for name in NAMES:
            old_build = old_report['build_receipt']['builds'][name]
            require(old_build['driver_sha256']==sha(entries['harness/library.go.txt']), 'diagnostic driver changed')
            if name=='library-official':require(old_build['sha256']==receipt['builds'][name]['sha256'], 'diagnostic official timing binary changed')
        diagnostic_entries = dict(entries)
        for name,data in entries.items():
            if name.startswith(diagnostic_prefix+'artifacts/'):
                diagnostic_entries['artifacts/full/'+name.removeprefix(diagnostic_prefix+'artifacts/')] = data
        diagnostic, labels = verify_metrics(old_report,load('inputs/full.json'),diagnostic_entries,'full')
        require(labels['library-maintained']=={r['path']:r['reference']['language'] or '' for r in old_gate['results']}, 'diagnostic output map changed')
        diagnostic['scope']='Initial retained-ID variant, full input only. Diagnostic historical evidence; excluded from all final cells and verdicts.'
        diagnostic['source_files_sha256']=sha(encode(old_before['files']))
        diagnostic['maintained_binary_sha256']=old_report['build_receipt']['builds']['library-maintained']['sha256']
        output['diagnostic_retained_ids']=diagnostic
    output['archive_members_sha256']={name:sha(data) for name,data in sorted(entries.items())}
    return output


def render(result):
    lines=['# Lexer subset opt2 measurements','',
        'Experimental measurements of the same preloaded GetLanguage API. Warm classifier time, first pass, and total process costs are distinct; a warm improvement alone is not an end-to-end speed claim.','',
        '| Input | Library | Warm passes / process | Warm median / p95 per corpus pass (s) | First-pass median / p95 (s) | Process median / p95 (s) | Nonwarm process median (s) | Peak RSS (MiB) |',
        '|---|---|---:|---:|---:|---:|---:|---:|']
    for scenario,value in result['scenarios'].items():
        for name,cell in value['cells'].items():
            w,f,p=cell['warm_per_corpus_pass'],cell['first_pass'],cell['process']
            lines.append(f"| {scenario} | {name.removeprefix('library-')} | {cell['warm_passes_per_process']} | {w['median_seconds']:.6f} / {w['p95_seconds']:.6f} | {f['median_seconds']:.6f} / {f['p95_seconds']:.6f} | {p['median_seconds']:.6f} / {p['p95_seconds']:.6f} | {cell['nonwarm_process']['median_seconds']:.6f} | {p['peak_rss_kib']/1024:.1f} |")
    lines += ['', '| Input | Library | Warm bytes / allocations per call | Median process CPU (s) | Warm CV | Samples |', '|---|---|---:|---:|---:|---:|']
    for scenario,value in result['scenarios'].items():
        for name,cell in value['cells'].items():
            lines.append(f"| {scenario} | {name.removeprefix('library-')} | {cell['median_bytes_per_call']:,.0f} / {cell['median_allocations_per_call']:.2f} | {cell['median_cpu_seconds']:.3f} | {cell['warm']['coefficient_of_variation']:.3f} | {cell['warm']['runs']} |")
    lines += ['', '| Input | Official/candidate warm ratio | Paired 95% interval | Warm verdict | Process ratio / paired 95% interval |', '|---|---:|---|---|---|']
    for scenario,value in result['scenarios'].items():
        lo,hi=value['paired_bootstrap_95_ci'];plo,phi=value['process_paired_bootstrap_95_ci']
        lines.append(f"| {scenario} | {value['speedup_official_over_candidate']:.3f} | [{lo:.3f}, {hi:.3f}] | {value['warm_verdict']} | {value['process_speedup_official_over_candidate']:.3f} / [{plo:.3f}, {phi:.3f}] |")
    lines += ['', '| Input | Historical baseline | Warm per-pass time decrease | First-pass time change | Process time change |', '|---|---|---:|---:|---|']
    for scenario,value in result['scenarios'].items():
        for older,h in value['historical'].items():
            process=f"{h['process_change_fraction']:+.1%}" if h['process_comparable'] else 'Not comparable: different warm-pass counts'
            lines.append(f"| {scenario} | {older} | {h['warm_time_reduction_fraction']:.1%} | {h['first_pass_change_fraction']:+.1%} | {process} |")
    lines += ['', 'Historical comparisons are separate windows. Warm times are divided by the actual complete corpus pass count. First pass is already one full pass. Process time and peak RSS include preload, package/runtime setup, the first pass, all warm passes, GC and output; process costs are never divided by warm iterations. Nonwarm process time is external process duration minus the measured warm loop, not a pure initialization or classifier metric.','',
        'Every completed cell has at least 20 retained samples after at least 3 symmetric warmups and at least 10 measured warm seconds. No timed samples are removed. Original outputs, discrepancies, raw resources, input/build identities and actual source inventories are archived. All 3,388 full-content Ruby labels and ordered-token hashes/counts passed; prefix agreement does not establish a separate prefix Ruby oracle. Official timing binaries are unchanged.','',
        'The managed manifest predates this experiment. Actual source inventories before/after the build and at capture identify the runtime; the changed tokenizer and new test are archived separately. The opt1-to-opt2 manifest change is expected because opt1 was integrated after its experimental record. Managed opt2 integration and broader release acceptance remain separate gates.','',
        'The original subset.patch and experiment-receipt.json describe the initial source-only proposal. The subsequent ruleids-test-only.patch removes runtime state used only by tests. Those proposal receipts do not identify the final measured runtime: the matching actual source inventories and final build receipt do.','',
        'Environment limits remain: Docker Desktop, no experimentally isolated governor/turbo/SMT, no independent quiet-host trace, and only one measurement window per input. No universal or three-window stability claim is justified.','',
        'Audit only: `python3 tests/profiling/record_subset.py --audit-only`. No benchmark runs during recording or audit.','']
    if 'diagnostic_retained_ids' in result:
        d=result['diagnostic_retained_ids'];c=d['cells']['library-maintained'];lo,hi=d['paired_bootstrap_95_ci']
        lines += ['The archive also retains the completed initial retained-ID full-input run as a separate diagnostic variant. Its samples are not pooled with the final variant.', '',
            '| Diagnostic variant | Warm median per pass (s) | Process median (s) | Peak RSS (MiB) | Official/candidate warm ratio / paired 95% interval |',
            '|---|---:|---:|---:|---|',
            f"| Initial retained IDs, full only | {c['warm_per_corpus_pass']['median_seconds']:.6f} | {c['process']['median_seconds']:.6f} | {c['process']['peak_rss_kib']/1024:.1f} | {d['speedup_official_over_candidate']:.3f} / [{lo:.3f}, {hi:.3f}] |", '']
    return '\n'.join(lines).encode()


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--experiment',type=Path,default=ROOT/'.cache/lexer-subset-opt2')
    p.add_argument('--validation',type=Path,help='default: <experiment>/validation')
    p.add_argument('--diagnostic',type=Path,help='optional completed initial retained-ID experiment; archived separately')
    p.add_argument('--diagnostic-builds',type=Path,default=ROOT/'.cache/enry-builds-lexer-subset-opt2',help='original retained-ID builds; used only with --diagnostic')
    p.add_argument('--builds',type=Path,default=ROOT/'.cache/enry-builds-lexer-subset-opt2')
    p.add_argument('--inputs',type=Path,default=ROOT/'.cache/enry-library-inputs-rc1')
    p.add_argument('--source',type=Path,default=ROOT/'third_party/go-enry')
    p.add_argument('--output',type=Path,default=HERE/'results/lexer-subset-opt2')
    p.add_argument('--audit-only',action='store_true')
    args=p.parse_args();args.validation=args.validation or args.experiment/'validation'
    if args.audit_only:
        hashes=json.loads((args.output/'SHA256SUMS.json').read_text())
        require(set(hashes)=={'README.md','audit.json','honest-gate.json','evidence.tar.gz'},'unexpected published file list')
        for name,digest in hashes.items():require(sha((args.output/name).read_bytes())==digest,'published artifact hash')
        entries=helper.unarchive((args.output/'evidence.tar.gz').read_bytes())
    else:entries=capture(args)
    result=audit(entries)
    gate=helper.attestation();gate.update(scenario='Experimental ASCII lexer subset opt2; warm/first-pass/process scopes separate')
    gate['questions']['11_three_tier_reporting']='pass: computed warm-only classifications retained; cold/process/RSS costs separately disclosed; no universal winner'
    gate['questions']['14_reproducible']='waive: actual experimental source and receipts retained, but managed opt2 regeneration and three independent repeat windows remain pending'
    outputs={'README.md':render(result),'audit.json':encode(result),'honest-gate.json':encode(gate)}
    if args.audit_only:
        for name,data in outputs.items():require((args.output/name).read_bytes()==data,'audit replay differs: '+name)
    else:
        args.output.mkdir(parents=True)
        outputs['evidence.tar.gz']=helper.archive(entries)
        for name,data in outputs.items():(args.output/name).write_bytes(data)
        (args.output/'SHA256SUMS.json').write_bytes(encode({name:sha(data) for name,data in outputs.items()}))
    print('PASS: real opt2 records audited; warm passes normalized; cold/process overhead retained.')


if __name__=='__main__':main()
