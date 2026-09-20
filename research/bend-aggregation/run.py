#!/usr/bin/env python3
"""Check a pinned optional Bend experiment and compare its native/JS observations."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def proof_verdict(stdout, stderr, code):
    text = (stdout + stderr).decode('utf-8', errors='replace')
    if code != 0:
        return {'status': 'rejected', 'unsafe_count': None}
    clean = stdout.strip() == b'All terms check.' and not stderr.strip()
    if clean:
        return {'status': 'checked', 'unsafe_count': 0}
    counts = re.findall(r'All terms check, with (\d+) unsafe annotations?\.', text)
    if len(counts) == 1:
        return {'status': 'partial', 'unsafe_count': int(counts[0])}
    return {'status': 'unrecognized', 'unsafe_count': None}


def load_certificate(raw):
    def unique_pairs(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'duplicate certificate key')
            value[key] = item
        return value
    def reject_constant(_):
        raise ValueError('non-finite certificate value')
    return json.loads(raw, object_pairs_hook=unique_pairs, parse_constant=reject_constant)


def validate_certificate(raw, *, typed=False):
    document = load_certificate(raw)
    require(isinstance(document, dict), 'certificate must be an object')
    fields = {'cases', 'admitted_pair_certificate'} | ({'out_of_range_rejected', 'all_indices_histogram', 'singleton_histograms'} if typed else set())
    require(set(document) == fields, 'unexpected certificate fields')
    if typed:
        require(document['out_of_range_rejected'] is True, 'typed index conversion accepted out-of-range index')
        require(document['all_indices_histogram'] == [1] * 65 and all(type(v) is int for v in document['all_indices_histogram']), 'typed all-bin execution failed')
        singletons = document['singleton_histograms']
        expected = [[int(row == column) for column in range(65)] for row in range(65)]
        require(singletons == expected and all(type(v) is int for row in singletons for v in row), 'typed singleton admission selected the wrong bin')
    require(document['admitted_pair_certificate'] is True, 'finite pair certificate failed')
    cases = document.get('cases')
    require(isinstance(cases, list) and 1 <= len(cases) <= 128, 'certificate cases missing or unbounded')
    total = 0
    for case in cases:
        fields = {'values', 'bins', 'histogram', 'count'} | ({'merged_histogram'} if typed else set())
        require(isinstance(case, dict) and set(case) == fields, 'unexpected certificate case fields')
        values = case['values']
        require(isinstance(values, list) and len(values) <= 4096, 'invalid case values')
        require(all(type(v) is int and 0 <= v < 2**32 for v in values), 'case exceeds admitted U32 domain')
        bins = [v.bit_length() for v in values]
        counts = [bins.count(i) for i in range(65)]
        require(type(case['count']) is int and case['count'] == len(values), 'population count mismatch')
        require(isinstance(case['bins'], list) and all(type(v) is int for v in case['bins']) and case['bins'] == bins, 'bit-length mapping mismatch')
        require(isinstance(case['histogram'], list) and all(type(v) is int for v in case['histogram']) and case['histogram'] == counts, 'dense histogram mismatch')
        if typed:
            require(isinstance(case['merged_histogram'], list) and all(type(v) is int for v in case['merged_histogram']) and case['merged_histogram'] == counts, 'independent partition merge mismatch')
        total += len(values)
    expected_values = [[], [0], [1],
        [0, 1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 63, 64, 127, 128, 255, 256, 257, 65535, 65536, 2147483647, 2147483648, 4294967295],
        list(range(257)), [7] * 257, list(range(4294967279, 4294967296))]
    require([case['values'] for case in cases] == expected_values, 'runtime corpus differs or is incomplete')
    return document, total


def validate_topk(raw):
    document = load_certificate(raw)
    require(isinstance(document, dict) and set(document) == {'topk_cases'}, 'unexpected top-K certificate fields')
    cases = document['topk_cases']
    populations = [[{'value': (i * 17) % 13, 'identity': i} for i in range(128)],
                   [{'value': 42, 'identity': i} for i in range(36, -1, -1)], []]
    expected = [(entries, limit) for entries in populations for limit in (0, 1, 10, 129)]
    require(isinstance(cases, list) and len(cases) == len(expected), 'top-K corpus differs or is incomplete')
    for case, (entries, limit) in zip(cases, expected):
        require(isinstance(case, dict) and set(case) == {'limit', 'entries', 'bounded', 'full_prefix', 'partitioned', 'many_partitioned'}, 'unexpected top-K case fields')
        require(type(case['limit']) is int and case['limit'] == limit and case['entries'] == entries, 'top-K input corpus differs')
        for field in ('entries', 'bounded', 'full_prefix', 'partitioned', 'many_partitioned'):
            require(isinstance(case[field], list) and all(isinstance(item, dict) and set(item) == {'value', 'identity'} and all(type(v) is int for v in item.values()) for item in case[field]), 'top-K entries must have integer values and identities')
        oracle = sorted(entries, key=lambda item: (-item['value'], item['identity']))[:limit]
        require(all(case[field] == oracle for field in ('bounded', 'full_prefix', 'partitioned', 'many_partitioned')), 'top-K result differs from independent full sorting')
    return document


def require_json_equal(raw, expected, message):
    # Canonical encoding also distinguishes true from 1 and 1.0 from 1.
    actual = load_certificate(raw)
    canonical = lambda value: json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False)
    require(canonical(actual) == canonical(expected), message)


class Experiment:
    def __init__(self, args):
        self.args = args
        self.output = args.output.resolve()
        require(not self.output.exists(), 'output directory must be fresh')
        self.output.mkdir(parents=True)
        self.logs = self.output / 'logs'
        self.logs.mkdir()
        self.env = dict(os.environ, BEND_NO_TELEMETRY='1', GOPROXY='off', GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='', CGO_ENABLED='0')
        self.steps = []

    def call(self, name, argv, *, cwd=ROOT, data=None, timeout=120, success=True):
        start = time.monotonic()
        with subprocess.Popen([str(a) for a in argv], cwd=cwd, env=self.env,
                              stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, start_new_session=True) as child:
            try:
                stdout, stderr = child.communicate(data, timeout=timeout)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                stdout, stderr = child.communicate()
                (self.logs / (name + '.stdout')).write_bytes(stdout)
                (self.logs / (name + '.stderr')).write_bytes(stderr)
                raise ValueError(f'{name} exceeded {timeout}s')
        elapsed = time.monotonic() - start
        (self.logs / (name + '.stdout')).write_bytes(stdout)
        (self.logs / (name + '.stderr')).write_bytes(stderr)
        self.steps.append({'name': name, 'exit_code': child.returncode, 'seconds': elapsed,
                           'stdout_sha256': digest(stdout), 'stderr_sha256': digest(stderr)})
        if success:
            require(child.returncode == 0, f'{name} failed; see its captured logs')
        return stdout, stderr, child.returncode

    def run(self):
        harness_hash = digest(Path(__file__).read_bytes())
        pin_hash = digest((HERE / 'pin.json').read_bytes())
        pin = json.loads((HERE / 'pin.json').read_text())
        checkout = self.args.bend_checkout.resolve()
        bun = self.args.bun.resolve()
        bun_hash = digest(bun.read_bytes())
        cli = checkout / 'bend2/main.ts'
        revision = self.call('bend-revision', ['git', '-C', checkout, 'rev-parse', 'HEAD'])[0].decode().strip()
        require(revision == pin['revision'], 'Bend revision differs from pin')
        status = self.call('bend-status', ['git', '-C', checkout, 'status', '--porcelain', '--untracked-files=all'])[0]
        require(not status, 'Bend checkout must be clean')
        version = self.call('bend-version', [bun, cli, '--version'])[0].decode().strip()
        require(version == pin['version'], 'Bend version differs from pin')
        bun_version = self.call('bun-version', [bun, '--version'])[0].decode().strip()
        require(bun_version == pin['bun_version'], 'Bun version differs from pin')
        self.call('bend-guide', [bun, cli, 'guide'])
        self.call('clang-version', ['clang', '--version'])
        self.call('go-version', ['go', 'version'])
        source = self.output / 'source'
        source.mkdir()
        names = [p.name for p in HERE.glob('*.bend')]
        require({'model.bend', 'LAWS.bend', 'PROOF.bend', 'main.bend'} <= set(names), 'experiment source incomplete')
        for name in names:
            shutil.copyfile(HERE / name, source / name)
        shutil.copytree(HERE / 'typed', source / 'typed', ignore=shutil.ignore_patterns('__pycache__'))
        shutil.copytree(HERE / 'topk', source / 'topk', ignore=shutil.ignore_patterns('__pycache__'))
        hashes = {p.relative_to(source).as_posix(): digest(p.read_bytes()) for p in sorted(source.rglob('*.bend'))}
        laws = {name: re.findall(r'^law ([a-zA-Z_][a-zA-Z_0-9]*):', (source / relative / 'LAWS.bend').read_text(), flags=re.MULTILINE)
                for name, relative in [('baseline', '.'), ('typed_histogram', 'typed'), ('topk', 'topk')]}
        require(all(names and len(names) == len(set(names)) for names in laws.values()), 'law inventory is empty or duplicated')
        license_hash = digest((source / 'topk/LICENSE-APACHE-2.0').read_bytes())
        require(license_hash == digest((checkout / 'LICENSE').read_bytes()), 'retained upstream license differs from pinned checkout')
        out, err, code = self.call('proof', [bun, cli, source / 'PROOF.bend'], success=False)
        verdict = proof_verdict(out, err, code)
        require(verdict['status'] == 'checked', f'proof is {verdict["status"]}; zero-unsafe complete check required')
        self.call('emit-js', [bun, cli, source / 'main.bend', '-o', self.output / 'model.js'])
        js = self.call('execute-js', [bun, self.output / 'model.js'])[0]
        certificate, observations = validate_certificate(js)
        self.call('compile-cpu', [bun, cli, source / 'main.bend', '-o', self.output / 'model-cpu'], timeout=180)
        cpu = self.call('execute-cpu', [self.output / 'model-cpu', '--threads', '1', '--gpu', 'off'])[0]
        require(cpu == js, 'compiled CPU and JS outputs differ')
        validate_certificate(cpu)
        bridge = self.output / 'production-bridge'
        shutil.copyfile(HERE / 'production.go', source / 'production.go')
        def selected_production_inputs(step):
            dependencies = self.call(step, ['go', 'list', '-mod=readonly', '-deps', '-json', source / 'production.go'])[0].decode()
            decoder = json.JSONDecoder()
            files = {ROOT / 'go.mod', ROOT / 'go.sum'}
            while dependencies.strip():
                package, end = decoder.raw_decode(dependencies.lstrip())
                dependencies = dependencies.lstrip()[end:]
                directory = Path(package.get('Dir', '/'))
                if directory.is_relative_to(ROOT) and directory != source:
                    for field in ('GoFiles', 'CgoFiles', 'CFiles', 'HFiles', 'SFiles', 'SysoFiles', 'EmbedFiles'):
                        files.update(directory / name for name in package.get(field, []))
            return files
        production_files = selected_production_inputs('production-inputs')
        production_hashes = {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in sorted(production_files)}
        self.call('compile-production-bridge', ['go', 'build', '-mod=readonly', '-trimpath', '-buildvcs=false', '-o', bridge, source / 'production.go'])
        require(production_hashes == {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in sorted(production_files)}, 'production source changed during build')
        for index, case in enumerate(certificate['cases']):
            actual = self.call(f'production-{index:03d}', [bridge], data=json.dumps(case['values']).encode())[0]
            require_json_equal(actual, {'count': case['count'], 'histogram': case['histogram']}, 'production aggregate differs from checked model execution')
        out, err, code = self.call('typed-proof', [bun, cli, source / 'typed/PROOF.bend'], success=False)
        typed_verdict = proof_verdict(out, err, code)
        require(typed_verdict['status'] == 'checked', 'typed proof requires a zero-unsafe complete check')
        self.call('typed-emit-js', [bun, cli, source / 'typed/main.bend', '-o', self.output / 'typed-model.js'])
        typed_js = self.call('typed-execute-js', [bun, self.output / 'typed-model.js'])[0]
        typed_certificate, typed_observations = validate_certificate(typed_js, typed=True)
        self.call('typed-compile-cpu', [bun, cli, source / 'typed/main.bend', '-o', self.output / 'typed-model-cpu'], timeout=180)
        typed_cpu = self.call('typed-execute-cpu', [self.output / 'typed-model-cpu', '--threads', '1', '--gpu', 'off'])[0]
        require(typed_cpu == typed_js, 'typed CPU and JS outputs differ')
        for index, case in enumerate(typed_certificate['cases']):
            actual = self.call(f'typed-production-{index:03d}', [bridge], data=json.dumps(case['values']).encode())[0]
            require_json_equal(actual, {'count': case['count'], 'histogram': case['histogram']}, 'production aggregate differs from typed model execution')
        for index, histogram in enumerate(typed_certificate['singleton_histograms']):
            value = 0 if index == 0 else 1 << (index - 1)
            actual = self.call(f'production-singleton-{index:02d}', [bridge], data=json.dumps([value]).encode())[0]
            require_json_equal(actual, {'count': 1, 'histogram': histogram}, 'production U64 singleton differs from typed bin admission')
        out, err, code = self.call('topk-proof', [bun, cli, source / 'topk/PROOF.bend'], success=False)
        topk_verdict = proof_verdict(out, err, code)
        require(topk_verdict['status'] == 'checked', 'top-K proof requires a zero-unsafe complete check')
        self.call('topk-emit-js', [bun, cli, source / 'topk/main.bend', '-o', self.output / 'topk-model.js'])
        topk_js = self.call('topk-execute-js', [bun, self.output / 'topk-model.js'])[0]
        topk_certificate = validate_topk(topk_js)
        self.call('topk-compile-cpu', [bun, cli, source / 'topk/main.bend', '-o', self.output / 'topk-model-cpu'], timeout=180)
        topk_cpu = self.call('topk-execute-cpu', [self.output / 'topk-model-cpu', '--threads', '1', '--gpu', 'off'])[0]
        require(topk_cpu == topk_js, 'top-K CPU and JS outputs differ')
        for index, case in enumerate(topk_certificate['topk_cases']):
            if case['limit'] == 10:
                actual = self.call(f'topk-production-{index:03d}', [bridge, '--topk'], data=json.dumps(case['entries']).encode())[0]
                require_json_equal(actual, {'top': case['bounded']}, 'production top ten differs from model and full sorting')
        negatives = []
        mutations = [
            ('lost-observation', 'Last{1n+count}', 'Last{count}', 'rejected'),
            ('false-pair-certificate', 'pairs_with(64n, 0n)', 'False{}', 'rejected'),
            ('wrong-value-mapping', 'def bit_length(value: U32) -> Nat:\n  bit_length_go(32n, value)',
             'def bit_length(value: U32) -> Nat:\n  0n', 'checked'),
        ]
        for name, before, after, expected in mutations:
            directory = self.output / name
            shutil.copytree(source, directory)
            model = directory / 'model.bend'
            original = model.read_text()
            require(before in original, f'{name}: mutation anchor missing')
            model.write_text(original.replace(before, after))
            out, err, code = self.call(name + '-proof', [bun, cli, directory / 'PROOF.bend'], success=False)
            actual_verdict = proof_verdict(out, err, code)
            require(actual_verdict['status'] == expected, f'{name}: unexpected proof verdict')
            if expected == 'rejected':
                require(b'expected :' in out + err and b'observed :' in out + err,
                        f'{name}: failure must be a type mismatch, not a parser/tool failure')
            entry = {'name': name, 'proof': actual_verdict,
                     'model_sha256': digest(model.read_bytes())}
            if expected == 'checked':
                self.call(name + '-emit', [bun, cli, directory / 'main.bend', '-o', directory / 'main.js'])
                result = self.call(name + '-execute', [bun, directory / 'main.js'])[0]
                try:
                    validate_certificate(result)
                except ValueError as error:
                    entry['runtime_rejected'] = True
                    entry['runtime_reason'] = str(error)
                else:
                    raise ValueError('wrong mapping escaped runtime validation')
            negatives.append(entry)
        directory = self.output / 'typed-wrong-merge'
        shutil.copytree(source, directory)
        model = directory / 'typed/model.bend'
        original = model.read_text()
        require('Nat.add(ac, bc)' in original, 'typed merge mutation anchor missing')
        model.write_text(original.replace('Nat.add(ac, bc)', 'ac'))
        out, err, code = self.call('typed-wrong-merge-proof', [bun, cli, directory / 'typed/PROOF.bend'], success=False)
        require(code != 0 and b'expected :' in out + err and b'observed :' in out + err, 'wrong typed merge did not fail a proof')
        negatives.append({'name': 'typed-wrong-merge', 'proof': proof_verdict(out, err, code), 'model_sha256': digest(model.read_bytes())})
        swap_helpers = '''def swap_case(is40: Bool, is41: Bool, value: Nat) -> Nat:
  match is40 is41:
    case True{} is41:
      41n
    case False{} True{}:
      40n
    case False{} False{}:
      value

def swap_40_41(+value: Nat) -> Nat:
  swap_case(Nat.is_eq(value, 40n), Nat.is_eq(value, 41n), value)

'''
        stronger_mutations = [
            ('typed-swapped-admission', 'typed',
             'cons_index(n, from_nat(n, x), admit(n, rest))',
             'cons_index(n, from_nat(n, swap_40_41(x)), admit(n, rest))'),
            ('topk-wrong-comparator', 'topk',
             'Nat.is_gt(av, bv) || (Nat.is_eq(av, bv) && Nat.is_lt(ai, bi))', 'False{}'),
            ('topk-dropped-input', 'topk',
             'def insert(xs: +List<Entry>, +x: Entry) -> +List<Entry>:\n  match xs:\n    case Nil{}:\n      Con{x, Nil{}}',
             'def insert(xs: +List<Entry>, +x: Entry) -> +List<Entry>:\n  match xs:\n    case Nil{}:\n      Nil{}'),
            ('topk-dropped-merge-input', 'topk',
             'def merge(+xs: +List<Entry>, +ys: +List<Entry>) -> +List<Entry>:\n  match xs ys:\n    case Nil{} ys:\n      ys',
             'def merge(+xs: +List<Entry>, +ys: +List<Entry>) -> +List<Entry>:\n  match xs ys:\n    case Nil{} ys:\n      Nil{}'),
            ('topk-dropped-partition-state', 'topk',
             'def all_bounded(+k: Nat, parts: +List<+List<Entry>>, state: +List<Entry>) -> +List<Entry>:\n  match parts:\n    case Nil{}:\n      state',
             'def all_bounded(+k: Nat, parts: +List<+List<Entry>>, state: +List<Entry>) -> +List<Entry>:\n  match parts:\n    case Nil{}:\n      Nil{}'),
        ]
        for name, module, before, after in stronger_mutations:
            directory = self.output / name
            shutil.copytree(source, directory)
            model = directory / module / 'model.bend'
            original = model.read_text()
            require(original.count(before) == 1, f'{name}: mutation anchor must be unique')
            changed = original.replace(before, after)
            if module == 'typed':
                anchor = 'def admit(+n: Nat, xs: +List<Nat>)'
                require(changed.count(anchor) == 1, 'admission helper insertion anchor must be unique')
                changed = changed.replace(anchor, swap_helpers + anchor)
            model.write_text(changed)
            out, err, code = self.call(name + '-model', [bun, cli, model], success=False)
            require(proof_verdict(out, err, code)['status'] == 'checked', f'{name}: mutated model must still typecheck')
            out, err, code = self.call(name + '-proof', [bun, cli, directory / module / 'PROOF.bend'], success=False)
            require(code != 0 and b'expected :' in out + err and b'observed :' in out + err, f'{name}: mutation did not fail a proof equation')
            negatives.append({'name': name, 'model_checked': True, 'proof': proof_verdict(out, err, code), 'model_sha256': digest(model.read_bytes())})
        for name, expression in [
                ('invalid-index', 'def invalid() -> M.Index(0n):\n  M.First{}\n'),
                ('incompatible-widths', 'def invalid() -> M.Histogram(65n):\n  M.merge(65n, M.zero(65n), M.zero(64n))\n')]:
            bad = source / 'typed' / (name + '.bend')
            bad.write_text('import Base\nimport ./model.bend as M\n\n' + expression)
            out, err, code = self.call(name, [bun, cli, bad], success=False)
            require(code != 0 and b'Error:' in out + err, f'{name}: invalid typed input was accepted')
            negatives.append({'name': name, 'proof': proof_verdict(out, err, code), 'source_sha256': digest(bad.read_bytes())})
        out, err, code = self.call('unfilled-laws' , [bun, cli, source / 'LAWS.bend'], success=False)
        require(code != 0 and b'TODO' in out + err, 'unfilled laws were not rejected as incomplete')
        negatives.append({'name': 'unfilled-laws', 'proof': proof_verdict(out, err, code)})
        require(selected_production_inputs('production-final-inputs') == production_files, 'selected production input set changed during experiment')
        require(production_hashes == {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in sorted(production_files)}, 'production source changed during experiment')
        require(digest(bun.read_bytes()) == bun_hash, 'Bun executable changed during experiment')
        require(digest(Path(__file__).read_bytes()) == harness_hash and digest((HERE / 'pin.json').read_bytes()) == pin_hash, 'harness or pin changed during experiment')
        require(self.call('bend-final-revision', ['git', '-C', checkout, 'rev-parse', 'HEAD'])[0].decode().strip() == revision, 'Bend revision changed during experiment')
        require(not self.call('bend-final-status', ['git', '-C', checkout, 'status', '--porcelain', '--untracked-files=all'])[0], 'Bend checkout changed during experiment')
        receipt = {'schema_version': 1, 'at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                   'platform': platform.platform(), 'pin': pin, 'pin_sha256': pin_hash, 'bun_sha256': bun_hash,
                   'experiment_sha256': hashes, 'harness_sha256': harness_hash,
                   'declared_laws': laws, 'upstream_license_sha256': license_hash,
                   'production_bridge_sha256': digest((source / 'production.go').read_bytes()),
                   'production_inputs_sha256': production_hashes,
                   'executables_sha256': {name: digest((self.output / name).read_bytes()) for name in ('model.js', 'model-cpu', 'typed-model.js', 'typed-model-cpu', 'topk-model.js', 'topk-model-cpu', 'production-bridge')},
                   'production_aggregator_sha256': digest((ROOT / 'pkg/structure/hotspots.go').read_bytes()),
                   'proof': verdict, 'typed_proof': typed_verdict, 'typed_cases': len(typed_certificate['cases']), 'typed_observations': typed_observations, 'counterexamples': negatives, 'cases': len(certificate['cases']), 'observations': observations,
                   'typed_singleton_bins': len(typed_certificate['singleton_histograms']),
                   'topk_proof': topk_verdict, 'topk_cases': len(topk_certificate['topk_cases']), 'production_top_ten_cases': 3,
                   'backend_outputs_identical': True, 'production_histograms_identical': True,
                   'scope': 'Model laws under the pinned checker; finite U32 backend and synthetic Go-aggregation checks. No parser, whole-production, compiler, GPU or full-u64 proof.',
                   'steps': self.steps, 'passed': True}
        (self.output / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
        print(json.dumps({key: receipt[key] for key in ('passed', 'proof', 'cases', 'observations')}, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bend-checkout', type=Path, required=True)
    parser.add_argument('--bun', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        Experiment(args).run()
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f'error: {error}\n')


if __name__ == '__main__':
    main()
