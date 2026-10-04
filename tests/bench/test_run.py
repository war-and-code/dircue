"""Self-tests for the correctness gate in the paired CLI benchmark."""

import hashlib
import json
import tempfile
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import run


class BenchmarkPreflightTests(unittest.TestCase):
    def setUp(self):
        self.scenario = {'name': 'sample', '_expected_exit': 0}
        self.binaries = {'baseline': 'baseline', 'candidate': 'candidate'}
        self.calls = []

    def execute(self, binary, _scenario, _corpus):
        self.calls.append(binary)
        return {'exit_code': 0, 'stdout': b'valid output', 'stderr': b'',
                'seconds': .001, 'command': [binary], 'cwd': '/fixture'}

    def test_matching_outputs_pass_preflight(self):
        values = run.preflight_scenario(self.scenario, self.binaries, Path('/fixture'), self.execute)
        self.assertEqual(tuple(values), ('baseline', 'candidate'))
        self.assertEqual(self.calls, ['baseline', 'candidate'])

    def test_wrong_output_faster_mutant_is_rejected_before_timing(self):
        def wrong_output(binary, scenario, corpus):
            result = self.execute(binary, scenario, corpus)
            if binary == 'candidate':
                result['stdout'] = b'wrong but fast'
                result['seconds'] = .000001
            return result

        with self.assertRaisesRegex(run.HarnessError, 'stdout/stderr differ; timing withheld'):
            run.preflight_scenario(self.scenario, self.binaries, Path('/fixture'), wrong_output)
        self.assertEqual(self.calls, ['baseline', 'candidate'])

    def test_faster_error_mutant_is_rejected_before_timing(self):
        def failed(binary, scenario, corpus):
            result = self.execute(binary, scenario, corpus)
            if binary == 'candidate':
                result['exit_code'] = 2
                result['stdout'] = b''
                result['seconds'] = .000001
            return result

        with self.assertRaisesRegex(run.HarnessError, 'candidate exited 2; expected 0'):
            run.preflight_scenario(self.scenario, self.binaries, Path('/fixture'), failed)
        self.assertEqual(self.calls, ['baseline', 'candidate'])

    def test_manifest_rejects_output_tree_as_measured_input(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'repo').mkdir()
            with self.assertRaisesRegex(run.HarnessError, 'overlaps measured input tree'):
                run.validate_output_paths(root / 'repo' / 'report.json', root / 'repo' / 'report-outputs',
                                          root, ['repo'], [])

    def test_report_and_artifact_paths_cannot_overwrite_binary_or_manifest(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            protected = root / 'baseline'
            protected.write_text('binary')
            with self.assertRaisesRegex(run.HarnessError, 'overlaps protected path'):
                run.validate_output_paths(protected, root / 'details', root, [], [protected])
            with self.assertRaisesRegex(run.HarnessError, 'overlaps protected path'):
                run.validate_output_paths(root / 'report.json', protected, root, [], [protected])

    def test_manifest_rejects_boolean_schema_and_unknown_keys(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'input').mkdir()
            path = root / 'manifest.json'
            for value in ({'schema_version': True, 'scenarios': []},
                          {'schema_version': 1, 'unexpected': True, 'scenarios': []}):
                path.write_text(json.dumps(value))
                with self.subTest(value=value), self.assertRaises(run.HarnessError):
                    run.load_manifest(path, root)
            huge_timeout = {'schema_version': 1, 'scenarios': [{
                'name': 'timeout', 'args': [], 'inputs': ['input'], 'timeout_seconds': 10 ** 1000}]}
            path.write_text(json.dumps(huge_timeout))
            with self.assertRaisesRegex(run.HarnessError, 'finite and positive'):
                run.load_manifest(path, root)

    def test_command_timeout_kills_the_process_group(self):
        with tempfile.TemporaryDirectory() as temporary:
            scenario = {'name': 'sleeper', 'args': ['-c', 'import time; time.sleep(5)'],
                        '_cwd_template': '{corpus}', '_timeout_seconds': .02, '_expected_exit': 0}
            with self.assertRaisesRegex(run.HarnessError, 'timed out'):
                run.run_process(Path(sys.executable), scenario, Path(temporary))

    def test_committed_manifests_cover_release_paths_and_optional_worker(self):
        root = Path(__file__).resolve().parents[2]
        normal, scenarios = run.load_manifest(root / 'tests/bench/scenarios.json', root)
        names = {item['name'] for item in scenarios}
        self.assertTrue({'legacy-languages', 'analyze-all-default', 'map', 'saved-report-compare',
                         'map-attachment'} <= names)
        self.assertTrue({'analyze-projects', 'analyze-declarations', 'analyze-metrics',
                         'analyze-formats', 'analyze-discovery', 'analyze-ecosystems',
                         'analyze-frameworks'} <= names)
        self.assertEqual(normal['schema_version'], 1)
        _, worker_scenarios = run.load_manifest(root / 'tests/bench/scenarios.worker.example.json', root)
        self.assertTrue(any('{worker}' in arg for scenario in worker_scenarios for arg in scenario['args']))
        _, topology = run.load_manifest(root / 'tests/bench/scenarios.topology.json', root)
        self.assertEqual({item['name'] for item in topology}, {'topology-map', 'topology-projects'})

    def main_fixture(self, root):
        corpus = root / 'corpus'
        measured = corpus / 'input'
        measured.mkdir(parents=True)
        (measured / 'source.txt').write_text('corpus')
        binaries = {}
        for name, content in (('baseline', b'baseline'), ('candidate', b'candidate')):
            path = root / name
            path.write_bytes(content)
            path.chmod(0o755)
            binaries[name] = path
        manifest = root / 'manifest.json'
        manifest.write_text(json.dumps({'schema_version': 1, 'scenarios': [{
            'name': 'sample', 'args': ['{cwd}'], 'cwd': '{corpus}/input', 'inputs': ['input']}]}))
        output = root / 'report.json'
        argv = ['--baseline', str(binaries['baseline']), '--candidate', str(binaries['candidate']),
                '--manifest', str(manifest), '--corpus-root', str(corpus), '--runs', '1',
                '--warmup', '1', '--output', str(output)]
        return corpus, measured, binaries, manifest, output, argv

    def test_main_warmup_mismatch_writes_failed_receipt_without_ratios(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, _, _, _, output, argv = self.main_fixture(root)
            calls = {'baseline': 0, 'candidate': 0}
            def execute(binary, scenario, corpus):
                name = Path(binary).name
                calls[name] += 1
                value = b'stable'
                if name == 'candidate' and calls[name] == 2:
                    value = b'warmup drift'
                return {'exit_code': 0, 'stdout': value, 'stderr': b'', 'seconds': .01,
                        'command': [str(binary), *scenario['args']], 'cwd': str(corpus / 'input')}

            self.assertEqual(run.main(argv, execute=execute), 1)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertEqual(report['status'], 'failed')
            self.assertIsNone(report['scenarios'][0]['timing'])
            self.assertIsNone(report['scenarios'][0]['paired_speedup_median'])

    def test_main_sample_mismatch_writes_failed_receipt_without_ratios(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, _, _, _, output, argv = self.main_fixture(root)
            calls = {'baseline': 0, 'candidate': 0}
            def execute(binary, scenario, corpus):
                name = Path(binary).name
                calls[name] += 1
                value = b'stable'
                if name == 'candidate' and calls[name] == 3:
                    value = b'measured drift'
                return {'exit_code': 0, 'stdout': value, 'stderr': b'', 'seconds': .01,
                        'command': [str(binary), *scenario['args']], 'cwd': str(corpus / 'input')}

            self.assertEqual(run.main(argv, execute=execute), 1)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertIsNone(report['scenarios'][0]['timing'])

    def test_main_success_records_raw_paired_times_and_output_signatures(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, _, _, _, output, argv = self.main_fixture(root)
            calls = {'baseline': 0, 'candidate': 0}
            def execute(binary, scenario, corpus):
                name = Path(binary).name
                calls[name] += 1
                return {'exit_code': 0, 'stdout': b'stable', 'stderr': b'',
                        'seconds': calls[name] / 1000,
                        'command': [str(binary), *scenario['args']], 'cwd': str(corpus / 'input')}

            self.assertEqual(run.main(argv, execute=execute), 0)
            report = json.loads(output.read_text())
            self.assertTrue(report['passed'])
            self.assertEqual(report['status'], 'complete')
            for tool in ('baseline', 'candidate'):
                sample = report['scenarios'][0]['timing'][tool]['samples'][0]
                self.assertEqual(sample['order'], ['baseline', 'candidate'])
                self.assertEqual(sample['stdout_sha256'], hashlib.sha256(b'stable').hexdigest())
                self.assertEqual(sample['seconds'], 3 / 1000)
            self.assertTrue(Path(report['scenarios'][0]['output_artifacts']['baseline']['stdout']).is_file())

    def test_main_runs_real_executables_with_corpus_relative_cwd(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            corpus, _, binaries, manifest, output, argv = self.main_fixture(root)
            for name, label in (('baseline', 'baseline stub'), ('candidate', 'candidate stub')):
                binaries[name].write_text(
                    '#!/usr/bin/env python3\n# ' + label + '\nprint("same output")\n')
                binaries[name].chmod(0o755)
            manifest.write_text(json.dumps({'schema_version': 1, 'scenarios': [{
                'name': 'relative-cwd', 'args': ['{cwd}'], 'cwd': 'input', 'inputs': ['input']}]}))
            argv.extend(['--warmup', '0'])

            self.assertEqual(run.main(argv), 0)
            report = json.loads(output.read_text())
            self.assertTrue(report['passed'])
            expected_cwd = str((corpus / 'input').resolve())
            self.assertEqual(report['scenarios'][0]['commands']['baseline']['cwd'], expected_cwd)
            self.assertEqual(report['scenarios'][0]['commands']['candidate']['cwd'], expected_cwd)

    def test_main_mutated_input_invalidates_samples(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, measured, _, _, output, argv = self.main_fixture(root)
            calls = {'baseline': 0, 'candidate': 0}
            def execute(binary, scenario, corpus):
                name = Path(binary).name
                calls[name] += 1
                if name == 'baseline' and calls[name] == 1:
                    (measured / 'source.txt').write_text('mutated')
                return {'exit_code': 0, 'stdout': b'stable', 'stderr': b'', 'seconds': .01,
                        'command': [str(binary), *scenario['args']], 'cwd': str(corpus / 'input')}

            self.assertEqual(run.main(argv, execute=execute), 1)
            report = json.loads(output.read_text())
            self.assertIn('corpus input changed', report['failure'])
            self.assertIsNone(report['scenarios'][0]['timing'])

    def test_main_manifest_mutation_invalidates_samples(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, _, _, manifest, output, argv = self.main_fixture(root)
            calls = {'baseline': 0, 'candidate': 0}
            def execute(binary, scenario, corpus):
                name = Path(binary).name
                calls[name] += 1
                if name == 'baseline' and calls[name] == 1:
                    manifest.write_text(manifest.read_text() + '\n')
                return {'exit_code': 0, 'stdout': b'stable', 'stderr': b'', 'seconds': .01,
                        'command': [str(binary), *scenario['args']], 'cwd': str(corpus / 'input')}

            self.assertEqual(run.main(argv, execute=execute), 1)
            report = json.loads(output.read_text())
            self.assertIn('manifest changed', report['failure'])
            self.assertIsNone(report['scenarios'][0]['timing'])

    def test_invalid_manifest_invalidates_old_success_receipt(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            corpus, _, binaries, manifest, output, argv = self.main_fixture(root)
            old = {'passed': True, 'performance_passed': True, 'status': 'complete',
                   'corpus_root': str(corpus), 'scenarios': [{'inputs': {'input': 'old-hash'},
                   'timing': {'baseline': {'median_seconds': .1}}, 'paired_speedup_median': 2.0}]}
            output.write_text(json.dumps(old))
            manifest.write_text('{ malformed')
            self.assertEqual(run.main(argv), 1)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertIsNone(report['performance_passed'])
            self.assertIsNone(report['scenarios'][0]['timing'])

    def test_deeply_nested_manifest_invalidates_old_success_receipt(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            corpus, _, _, manifest, output, argv = self.main_fixture(root)
            old = {'passed': True, 'performance_passed': True, 'status': 'complete',
                   'corpus_root': str(corpus), 'scenarios': []}
            output.write_text(json.dumps(old))
            nesting = 10_000
            manifest.write_text('[' * nesting + '0' + ']' * nesting)

            self.assertEqual(run.main(argv), 1)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertIsNone(report['performance_passed'])
            self.assertEqual(report['status'], 'failed')
            self.assertIn('cannot read manifest', report['failure'])

    def test_manifest_byte_limit_invalidates_old_success_receipt(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            corpus, _, _, manifest, output, argv = self.main_fixture(root)
            old = {'passed': True, 'performance_passed': True, 'status': 'complete',
                   'corpus_root': str(corpus), 'scenarios': []}
            output.write_text(json.dumps(old))
            manifest.write_bytes(b' ' * (run.MAX_MANIFEST_BYTES + 1))

            self.assertEqual(run.main(argv), 1)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertIsNone(report['performance_passed'])
            self.assertIn('manifest exceeds', report['failure'])

    def test_existing_details_directory_is_preserved_and_old_success_invalidated(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            corpus, _, _, _, output, argv = self.main_fixture(root)
            old = {'passed': True, 'performance_passed': True, 'status': 'complete',
                   'corpus_root': str(corpus), 'scenarios': [{'inputs': {'input': 'old-hash'},
                   'timing': {'baseline': {'median_seconds': .1}}, 'paired_speedup_median': 2.0}]}
            output.write_text(json.dumps(old))
            details = output.parent / (output.stem + '-outputs')
            details.mkdir()
            sentinel = details / 'unowned.txt'
            sentinel.write_text('preserve me')

            self.assertEqual(run.main(argv), 1)
            self.assertEqual(sentinel.read_text(), 'preserve me')
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertIsNone(report['performance_passed'])
            self.assertIsNone(report['scenarios'][0]['timing'])
            self.assertIn('must be fresh and absent', report['failure'])


if __name__ == '__main__':
    unittest.main()
