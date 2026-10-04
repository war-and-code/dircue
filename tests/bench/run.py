#!/usr/bin/env python3
"""Compare two dircue binaries only after strict output conformance checks.

Manifest format: {"schema_version": 1, "scenarios": [{"name": str,
"args": [str, ...], "cwd": str, "inputs": [relative paths, ...],
"expected_exit": int, "timeout_seconds": number, "env": {str: str},
"provenance": object}]}. The binary path is argv[0]; {corpus} and {cwd}
are expanded in scenario arguments, cwd, and environment values. `{worker}` is
available only when `--worker` is supplied. No shell is used. Every scenario
must declare the input trees that affect its result.
"""

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import signal
import statistics
import subprocess
import sys
import tempfile
import time


class HarnessError(Exception):
    pass


MAX_MANIFEST_BYTES = 8 * 1024 * 1024


def read_manifest(path):
    with path.open('rb') as stream:
        raw = stream.read(MAX_MANIFEST_BYTES + 1)
    if len(raw) > MAX_MANIFEST_BYTES:
        raise HarnessError(f'manifest exceeds the {MAX_MANIFEST_BYTES}-byte limit')
    return raw


def sha256_file(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def digest_tree(root, relative, excludes=()):
    """Hash names, file bytes, and symlink targets under a declared input."""
    base = resolve_input(root, relative)
    digest = hashlib.sha256()

    def add(kind, name, content=b''):
        encoded = name.encode('utf-8', errors='surrogateescape')
        digest.update(kind + len(encoded).to_bytes(8, 'big') + encoded)
        digest.update(len(content).to_bytes(8, 'big'))
        digest.update(content)

    if base.is_file() or base.is_symlink():
        paths = [base]
        prefix = base.parent
    else:
        prefix = base.parent
        paths = sorted(base.rglob('*'), key=lambda item: item.as_posix())
        paths.insert(0, base)
    for path in paths:
        name = path.relative_to(prefix).as_posix()
        tree_name = path.relative_to(base).as_posix() if base.is_dir() else path.name
        if any(tree_name == item or tree_name.startswith(item.rstrip('/') + '/') for item in excludes):
            continue
        if path.is_symlink():
            add(b'L', name, os.readlink(path).encode('utf-8', errors='surrogateescape'))
        elif path.is_dir():
            add(b'D', name)
        elif path.is_file():
            encoded = name.encode('utf-8', errors='surrogateescape')
            size = path.stat().st_size
            digest.update(b'F' + len(encoded).to_bytes(8, 'big') + encoded + size.to_bytes(8, 'big'))
            with path.open('rb') as stream:
                for block in iter(lambda: stream.read(1024 * 1024), b''):
                    digest.update(block)
        else:
            raise HarnessError(f'unsupported input filesystem object: {path}')
    return digest.hexdigest()


def resolve_input(root, relative):
    base = (root / relative).resolve()
    try:
        base.relative_to(root.resolve())
    except ValueError as error:
        raise HarnessError(f'input escapes corpus root: {relative}') from error
    if not base.exists():
        raise HarnessError(f'declared input does not exist: {relative}')
    return base


def validate_output_paths(output, details, root, input_paths, protected_paths):
    destinations = {'report': output.resolve(), 'details': details.resolve()}
    for label, destination in destinations.items():
        for protected in protected_paths:
            protected = protected.resolve()
            if (destination == protected or destination in protected.parents
                    or protected in destination.parents):
                raise HarnessError(f'{label} output overlaps protected path: {protected}')
        for relative in input_paths:
            source = (root / relative).resolve()
            if (destination == source or destination in source.parents or source in destination.parents):
                raise HarnessError(f'{label} output overlaps measured input tree {relative}: {source}')
    if output.is_symlink() or (output.exists() and not output.is_file()):
        raise HarnessError(f'report output must be a regular file path: {output}')
    if details.is_symlink() or details.exists():
        raise HarnessError(f'correctness output directory must be fresh and absent: {details}')


def invalidate_previous_report(output, failure, protected_paths):
    """Keep a prior passing report from surviving a failed reuse attempt."""
    output = output.absolute()
    if output.is_symlink() or not output.is_file():
        return
    try:
        previous = json.loads(output.read_text(encoding='utf-8'))
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError):
        return
    if not isinstance(previous, dict) or previous.get('passed') is not True:
        return
    corpus_root = previous.get('corpus_root')
    scenarios = previous.get('scenarios')
    if not isinstance(corpus_root, str) or not isinstance(scenarios, list):
        return
    corpus = Path(corpus_root).resolve()
    input_paths = []
    for scenario in scenarios:
        inputs = scenario.get('inputs') if isinstance(scenario, dict) else None
        if isinstance(inputs, dict):
            input_paths.extend(path for path in inputs if isinstance(path, str))
    for path in protected_paths:
        resolved = path.resolve()
        if output.resolve() == resolved or output.resolve() in resolved.parents or resolved in output.resolve().parents:
            return
    if corpus.is_dir():
        for relative in input_paths:
            source = (corpus / relative).resolve()
            if output.resolve() == source or source in output.resolve().parents or output.resolve() in source.parents:
                return
    previous['passed'] = False
    previous['performance_passed'] = None
    previous['status'] = 'failed'
    previous['failure'] = str(failure)
    previous['finished_at_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    for scenario in scenarios:
        if not isinstance(scenario, dict):
            continue
        scenario['timing'] = None
        scenario['paired_speedup_median'] = None
        scenario.pop('paired_speedups', None)
    output.write_text(json.dumps(previous, indent=2) + '\n', encoding='utf-8')


def load_manifest(path, corpus_root, raw=None):
    try:
        if raw is None:
            raw = read_manifest(path)
        value = json.loads(raw)
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise HarnessError(f'cannot read manifest: {error}') from error
    if (not isinstance(value, dict) or type(value.get('schema_version')) is not int
            or value.get('schema_version') != 1):
        raise HarnessError('manifest must be an object with schema_version 1')
    unknown = set(value) - {'schema_version', 'description', 'scenarios'}
    if unknown:
        raise HarnessError(f'manifest has unknown keys: {", ".join(sorted(unknown))}')
    if 'description' in value and not isinstance(value['description'], str):
        raise HarnessError('manifest description must be a string')
    scenarios = value.get('scenarios')
    if not isinstance(scenarios, list) or not scenarios:
        raise HarnessError('manifest must contain at least one scenario')
    names = set()
    root = corpus_root.resolve()
    for scenario in scenarios:
        if not isinstance(scenario, dict):
            raise HarnessError('each scenario must be an object')
        unknown = set(scenario) - {'name', 'args', 'cwd', 'inputs', 'exclude', 'expected_exit',
                                   'timeout_seconds', 'env', 'provenance'}
        if unknown:
            raise HarnessError(f'scenario has unknown keys: {", ".join(sorted(unknown))}')
        name, args = scenario.get('name'), scenario.get('args')
        if (not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', name)
                or name in names):
            raise HarnessError('scenario names must be nonempty and unique')
        names.add(name)
        if not isinstance(args, list) or any(not isinstance(item, str) for item in args):
            raise HarnessError(f'{name}: args must be a list of strings')
        inputs = scenario.get('inputs')
        if not isinstance(inputs, list) or not inputs or any(not isinstance(item, str) or not item for item in inputs):
            raise HarnessError(f'{name}: declare at least one measured input path')
        for item in inputs:
            resolve_input(root, item)
        excludes = scenario.get('exclude', [])
        if (not isinstance(excludes, list) or any(not isinstance(item, str) or not item
                                                  or Path(item).is_absolute() or '..' in Path(item).parts
                                                  for item in excludes)):
            raise HarnessError(f'{name}: exclude must contain relative paths without ..')
        scenario['_exclude'] = excludes
        code = scenario.get('expected_exit', 0)
        if type(code) is not int or code != 0:
            raise HarnessError(f'{name}: benchmark scenarios must expect successful exit 0')
        timeout = scenario.get('timeout_seconds', 300)
        try:
            timeout_seconds = float(timeout)
        except (TypeError, ValueError, OverflowError):
            timeout_seconds = math.nan
        if (not isinstance(timeout, (int, float)) or isinstance(timeout, bool)
                or not math.isfinite(timeout_seconds) or timeout_seconds <= 0):
            raise HarnessError(f'{name}: timeout_seconds must be finite and positive')
        env = scenario.get('env', {})
        if not isinstance(env, dict) or any(not isinstance(k, str) or not isinstance(v, str) for k, v in env.items()):
            raise HarnessError(f'{name}: env must map strings to strings')
        cwd = scenario.get('cwd', '{corpus}')
        if not isinstance(cwd, str):
            raise HarnessError(f'{name}: cwd must be a string')
        scenario['_provenance'] = scenario.get('provenance', {})
        if not isinstance(scenario['_provenance'], dict):
            raise HarnessError(f'{name}: provenance must be an object')
        scenario['_timeout_seconds'] = timeout_seconds
        scenario['_expected_exit'] = code
        scenario['_cwd_template'] = cwd
    return value, scenarios


def expand(value, corpus, cwd, worker=None):
    if '{worker}' in value and worker is None:
        raise HarnessError('manifest requires {worker}; pass --worker with an explicit structural worker')
    return (value.replace('{corpus}', str(corpus)).replace('{cwd}', str(cwd))
            .replace('{worker}', str(worker) if worker is not None else ''))


def resolve_cwd(template, corpus, worker=None):
    cwd = Path(expand(template, corpus, corpus, worker))
    if not cwd.is_absolute():
        cwd = corpus / cwd
    return cwd.resolve(strict=True)


def run_process(binary, scenario, corpus, worker=None):
    cwd = resolve_cwd(scenario['_cwd_template'], corpus, worker)
    args = [str(binary)] + [expand(arg, corpus, cwd, worker) for arg in scenario['args']]
    env = os.environ.copy()
    env.update({key: expand(value, corpus, cwd, worker) for key, value in scenario.get('env', {}).items()})
    with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
        kwargs = {'cwd': cwd, 'env': env, 'stdout': stdout, 'stderr': stderr}
        if os.name == 'nt':
            kwargs['creationflags'] = subprocess.CREATE_NEW_PROCESS_GROUP
        else:
            kwargs['start_new_session'] = True
        try:
            started = time.perf_counter()
            process = subprocess.Popen(args, **kwargs)
        except OSError as error:
            raise HarnessError(f'cannot start {args!r}: {error}') from error
        try:
            process.wait(timeout=scenario['_timeout_seconds'])
        except subprocess.TimeoutExpired as error:
            if os.name == 'nt':
                subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            else:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            process.wait()
            raise HarnessError(f'command timed out after {scenario["_timeout_seconds"]}s: {args!r}') from error
        elapsed = time.perf_counter() - started
        stdout.seek(0)
        out = stdout.read()
        stderr.seek(0)
        err = stderr.read()
    return {'exit_code': process.returncode, 'stdout': out, 'stderr': err,
            'seconds': elapsed, 'command': args, 'cwd': str(cwd)}


def output_signature(result):
    return (result['exit_code'], hashlib.sha256(result['stdout']).hexdigest(), len(result['stdout']),
            hashlib.sha256(result['stderr']).hexdigest(), len(result['stderr']))


def describe(result):
    return {'exit_code': result['exit_code'], 'stdout_bytes': len(result['stdout']),
            'stdout_sha256': hashlib.sha256(result['stdout']).hexdigest(),
            'stderr_bytes': len(result['stderr']),
            'stderr_sha256': hashlib.sha256(result['stderr']).hexdigest()}


def check_result(scenario, result, reference, label):
    if type(result.get('exit_code')) is not int:
        raise HarnessError(f'{scenario["name"]}/{label} returned an invalid exit code')
    if not isinstance(result.get('stdout'), bytes) or not isinstance(result.get('stderr'), bytes):
        raise HarnessError(f'{scenario["name"]}/{label} returned non-byte output')
    seconds = result.get('seconds')
    try:
        seconds = float(seconds)
    except (TypeError, ValueError, OverflowError):
        seconds = math.nan
    if (not isinstance(result.get('seconds'), (int, float)) or isinstance(result.get('seconds'), bool)
            or not math.isfinite(seconds) or seconds <= 0):
        raise HarnessError(f'{scenario["name"]}/{label} returned an invalid wall-time sample')
    if result['exit_code'] != scenario['_expected_exit']:
        raise HarnessError(f'{scenario["name"]}/{label} exited {result["exit_code"]}; expected {scenario["_expected_exit"]}')
    if reference is not None and output_signature(result) != output_signature(reference):
        raise HarnessError(f'{scenario["name"]}/{label} output changed between executions')


def preflight_scenario(scenario, binaries, corpus, execute=run_process, worker=None):
    """Prove both binaries succeed and emit identical bytes before any timing."""
    outputs = {}
    for name in ('baseline', 'candidate'):
        if execute is run_process:
            result = execute(binaries[name], scenario, corpus, worker)
        else:
            result = execute(binaries[name], scenario, corpus)
        check_result(scenario, result, None, name)
        outputs[name] = result
    if (outputs['baseline']['stdout'], outputs['baseline']['stderr']) != (
            outputs['candidate']['stdout'], outputs['candidate']['stderr']):
        raise HarnessError(f'{scenario["name"]}: baseline and candidate stdout/stderr differ; timing withheld')
    return outputs


def percentile(values, fraction):
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, math.ceil(fraction * len(ordered)) - 1)]


def timing_summary(samples):
    values = [sample['seconds'] for sample in samples]
    return {'runs': len(values), 'median_seconds': statistics.median(values),
            'p95_seconds': percentile(values, .95), 'min_seconds': min(values),
            'max_seconds': max(values)}


def main(argv=None, execute=run_process):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--corpus-root', type=Path, required=True)
    parser.add_argument('--worker', type=Path, help='explicit structural worker for a worker-enabled manifest')
    parser.add_argument('--runs', type=int, default=20)
    parser.add_argument('--warmup', type=int, default=3)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(argv)
    report = None
    try:
        corpus = args.corpus_root.resolve(strict=True)
        if not corpus.is_dir():
            raise HarnessError('corpus root must be a directory')
        manifest_path = args.manifest.resolve(strict=True)
        manifest_bytes = read_manifest(manifest_path)
        manifest, scenarios = load_manifest(manifest_path, corpus, raw=manifest_bytes)
        binaries = {name: path.resolve() for name, path in
                    (('baseline', args.baseline), ('candidate', args.candidate))}
        worker = args.worker.resolve() if args.worker else None
        if any('{worker}' in json.dumps(scenario) for scenario in scenarios) and worker is None:
            raise HarnessError('manifest requires {worker}; pass --worker with an explicit structural worker')
        cwd_paths = []
        for scenario in scenarios:
            cwd = resolve_cwd(scenario['_cwd_template'], corpus, worker)
            try:
                cwd.relative_to(corpus)
            except ValueError as error:
                raise HarnessError(f'{scenario["name"]}: cwd escapes corpus root: {cwd}') from error
            if not cwd.is_dir():
                raise HarnessError(f'{scenario["name"]}: cwd is not a directory: {cwd}')
            cwd_paths.append(cwd)
        input_paths = [item for scenario in scenarios for item in scenario['inputs']]
        output = args.output.absolute()
        details = output.parent / (output.stem + '-outputs')
        protected = [manifest_path, *binaries.values()]
        if worker is not None:
            protected.append(worker)
        validate_output_paths(output, details, corpus, input_paths, protected)
        for destination in (output.resolve(), details.resolve()):
            for cwd in cwd_paths:
                if destination == cwd or destination in cwd.parents:
                    raise HarnessError(f'output path would replace a scenario working directory: {cwd}')
        output.parent.mkdir(parents=True, exist_ok=True)
        details.mkdir()
        try:
            if output.exists():
                output.unlink()
        except OSError:
            details.rmdir()
            raise

        report = {'schema_version': '1.0.0', 'started_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                  'manifest_path': str(manifest_path), 'manifest_sha256': hashlib.sha256(manifest_bytes).hexdigest(),
                  'corpus_root': str(corpus), 'binaries': {name: {'path': str(path)} for name, path in binaries.items()},
                  'structural_worker': {'path': str(worker)} if worker else None,
                  'methodology': {'runs': args.runs, 'warmup': args.warmup,
                                  'order': 'alternating paired order; baseline first on even pairs',
                                  'correctness': 'expected exit 0 plus exact stdout/stderr, checked against each binary preflight on all warmups and samples',
                                  'timing': 'perf_counter immediately before process start through exit',
                                  'rss': None, 'rss_note': 'portable per-process peak RSS measurement is unavailable in the standard library',
                                  'cache': 'warm after correctness preflight and explicit warmups; no cache flush',
                                  'provenance_scope': 'declared input paths are hashed before and after; declare every file or tree that affects a command',
                                  'performance_gate': 'none; timings are descriptive and never accept a baseline automatically'},
                  'environment': {'platform': platform.platform(), 'machine': platform.machine(),
                                  'python': sys.version.split()[0], 'cpu_count': os.cpu_count()},
                  'scenarios': [], 'passed': False, 'performance_passed': None, 'status': 'running'}
        output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')

        if args.runs < 1 or args.warmup < 0:
            raise HarnessError('runs must be positive and warmup must be nonnegative')
        binaries = {name: path.resolve(strict=True) for name, path in binaries.items()}
        for name, path in binaries.items():
            if not path.is_file() or not os.access(path, os.X_OK):
                raise HarnessError(f'{name} binary is not an executable file: {path}')
        hashes = {name: sha256_file(path) for name, path in binaries.items()}
        if hashes['baseline'] == hashes['candidate']:
            raise HarnessError('baseline and candidate have identical binary hashes')
        if worker is not None and (not worker.is_file() or not os.access(worker, os.X_OK)):
            raise HarnessError(f'structural worker is not an executable file: {worker}')
        report['binaries'] = {name: {'path': str(path), 'sha256': hashes[name], 'size_bytes': path.stat().st_size}
                              for name, path in binaries.items()}
        if worker is not None:
            report['structural_worker'] = {'path': str(worker), 'sha256': sha256_file(worker),
                                           'size_bytes': worker.stat().st_size}

        input_keys = {(item, tuple(scenario['_exclude'])) for scenario in scenarios for item in scenario['inputs']}
        before_digests = {key: digest_tree(corpus, key[0], key[1]) for key in input_keys}
        before = {scenario['name']: {item: before_digests[(item, tuple(scenario['_exclude']))]
                                     for item in scenario['inputs']} for scenario in scenarios}
        report['corpus_inputs_before'] = before

        expected_outputs = {}
        for scenario in scenarios:
            outputs = preflight_scenario(scenario, binaries, corpus, execute=execute, worker=worker)
            commands = {}
            for name in ('baseline', 'candidate'):
                result = outputs[name]
                commands[name] = {'argv': result['command'], 'cwd': result['cwd']}
            expected_outputs[scenario['name']] = {name: output_signature(result) for name, result in outputs.items()}
            for name, result in outputs.items():
                (details / f'{scenario["name"]}-{name}.stdout').write_bytes(result['stdout'])
                (details / f'{scenario["name"]}-{name}.stderr').write_bytes(result['stderr'])
            report['scenarios'].append({'name': scenario['name'], 'provenance': scenario['_provenance'],
                                        'inputs': before[scenario['name']], 'excluded_input_paths': scenario['_exclude'],
                                        'env_overrides': scenario.get('env', {}), 'commands': commands,
                                        'outputs': {name: describe(result) for name, result in outputs.items()},
                                        'timing': None, 'paired_speedup_median': None,
                                        'output_artifacts': {name: {
                                            'stdout': str(details / f'{scenario["name"]}-{name}.stdout'),
                                            'stderr': str(details / f'{scenario["name"]}-{name}.stderr')}
                                            for name in outputs}})

        by_name = {item['name']: item for item in report['scenarios']}
        for scenario in scenarios:
            expected = expected_outputs[scenario['name']]
            for warmup in range(args.warmup):
                order = ('baseline', 'candidate') if warmup % 2 == 0 else ('candidate', 'baseline')
                for name in order:
                    result = execute(binaries[name], scenario, corpus, worker) if execute is run_process else execute(binaries[name], scenario, corpus)
                    check_result(scenario, result, None, name)
                    if output_signature(result) != expected[name]:
                        raise HarnessError(f'{scenario["name"]}/{name} output changed during warmup')
            samples = {'baseline': [], 'candidate': []}
            paired_speedups = []
            for iteration in range(args.runs):
                order = ('baseline', 'candidate') if iteration % 2 == 0 else ('candidate', 'baseline')
                pair = {}
                for name in order:
                    result = execute(binaries[name], scenario, corpus, worker) if execute is run_process else execute(binaries[name], scenario, corpus)
                    check_result(scenario, result, None, name)
                    if output_signature(result) != expected[name]:
                        raise HarnessError(f'{scenario["name"]}/{name} output changed during measured run')
                    samples[name].append({'iteration': iteration, 'order': list(order), **describe(result),
                                          'seconds': result['seconds']})
                    pair[name] = result['seconds']
                paired_speedups.append(pair['baseline'] / pair['candidate'])
            by_name[scenario['name']]['timing'] = {
                name: {**timing_summary(values), 'samples': values} for name, values in samples.items()}
            by_name[scenario['name']]['paired_speedup_median'] = statistics.median(paired_speedups)
            by_name[scenario['name']]['paired_speedups'] = paired_speedups

        after_digests = {key: digest_tree(corpus, key[0], key[1]) for key in input_keys}
        after = {scenario['name']: {item: after_digests[(item, tuple(scenario['_exclude']))]
                                    for item in scenario['inputs']} for scenario in scenarios}
        report['corpus_inputs_after'] = after
        if before != after:
            raise HarnessError('declared corpus input changed during the run; results are not comparable')
        for name, path in binaries.items():
            if sha256_file(path) != hashes[name]:
                raise HarnessError(f'{name} binary changed during the run')
        if worker is not None and sha256_file(worker) != report['structural_worker']['sha256']:
            raise HarnessError('structural worker changed during the run')
        if hashlib.sha256(read_manifest(manifest_path)).hexdigest() != report['manifest_sha256']:
            raise HarnessError('manifest changed during the run')
        report['finished_at_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
        report['passed'] = True
        report['status'] = 'complete'
        output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
        print(f'Correctness passed for {len(scenarios)} scenarios; timings recorded in {args.output}')
        return 0
    except (HarnessError, OSError, ValueError) as error:
        if isinstance(report, dict):
            report['failure'] = str(error)
            report['finished_at_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
            report['passed'] = False
            report['performance_passed'] = None
            report['status'] = 'failed'
            for scenario in report.get('scenarios', []):
                scenario['timing'] = None
                scenario['paired_speedup_median'] = None
                scenario.pop('paired_speedups', None)
            output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
        else:
            invalidate_previous_report(args.output, error,
                                       [args.baseline, args.candidate, args.manifest] +
                                       ([args.worker] if args.worker else []))
        print(f'benchmark not comparable: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
