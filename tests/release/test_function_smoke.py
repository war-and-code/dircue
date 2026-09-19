"""Receipt contract tests; native execution is covered by the real smoke helper."""
import copy
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import function_release_smoke as smoke


class FunctionReleaseContracts(unittest.TestCase):
    def receipt(self, version='0.4.0-rc.1'):
        # Synthetic receipt data tests rejection paths; it is never represented
        # as proof that an executable or a packaged worker was run.
        required = smoke.functions_required(version)
        cases = ['Examples.java', 'Examples.cs', 'examples.py', 'empty', 'recovery',
                 'many', 'name_bound', 'suppression', "unicode_''", "unicode_'\\n'", "unicode_'\\r\\n'"]
        rows = [{'case': name, 'source_sha256': 'c' * 64, 'status': 'complete',
                 'total_spaces': 1, 'omitted_spaces': 0, 'retained_spaces': 1} for name in cases]
        for row in rows:
            if row['case'] in {'recovery', 'name_bound', 'many'}:
                row['status'] = 'partial'
            if row['case'] == 'many':
                row.update(total_spaces=140, omitted_spaces=12, retained_spaces=128)
        receipt = {'schema_version': '1.0.0', 'passed': True, 'version': version,
                   'functions_required': required, 'candidate_sha256': 'a' * 64,
                   'worker_sha256': 'b' * 64, 'source_sha256': smoke.source_inputs(),
                   'fixture_count': 21, 'language_count': 20,
                   'checks': sorted(smoke.DEFAULT_CHECKS | (smoke.FUNCTION_CHECKS if required else set())),
                   'default_stdout_sha256': 'd' * 64, 'counterexamples': rows if required else []}
        if required:
            receipt.update(known_span_languages=['C#', 'Java', 'Python'],
                           generated_source_omission=True, parent_coverage_partial=True)
        return receipt

    def test_function_support_is_versioned_without_breaking_prior_artifacts(self):
        for version in ['0.1.0', '0.2.0', '0.3.0', '0.3.99']:
            self.assertFalse(smoke.functions_required(version))
            receipt = self.receipt(version)
            self.assertEqual(receipt, smoke.validate_receipt(receipt, version, 'a' * 64, 'b' * 64))
        for version in ['0.4.0-alpha.1', '0.4.0-beta.0', '0.4.0-rc.1', '0.4.0', '1.0.0']:
            self.assertTrue(smoke.functions_required(version))
        for version in ['', '0.4', '00.4.0', '0.4.0-dev', '0.4.0;command']:
            with self.assertRaises(ValueError):
                smoke.functions_required(version)

    def test_complete_receipt_and_identity_rejection(self):
        receipt = self.receipt()
        self.assertEqual(receipt, smoke.validate_receipt(receipt, '0.4.0-rc.1', 'a' * 64, 'b' * 64))
        for name, value in [('passed', False), ('functions_required', False), ('version', '0.3.0'),
                            ('candidate_sha256', 'e' * 64), ('worker_sha256', 'f' * 64),
                            ('fixture_count', 20), ('language_count', 19),
                            ('default_stdout_sha256', ''), ('source_sha256', {})]:
            changed = copy.deepcopy(receipt)
            changed[name] = value
            with self.assertRaises(ValueError, msg=name):
                smoke.validate_receipt(changed, '0.4.0-rc.1', 'a' * 64, 'b' * 64)

    def test_missing_function_checks_and_incomplete_evidence_rejected(self):
        for mutate in [
            lambda r: r['checks'].remove('known_java_csharp_python_spans'),
            lambda r: r['checks'].append('single_parse'),
            lambda r: r['counterexamples'].pop(),
            lambda r: r['counterexamples'][5].update(total_spaces=128, omitted_spaces=0),
            lambda r: r['counterexamples'][4].update(status='complete'),
            lambda r: r['counterexamples'][0].update(source_sha256=''),
            lambda r: r['checks'].append({}),
            lambda r: r['counterexamples'].__setitem__(0, None),
            lambda r: r.update(default_stdout_sha256=42),
            lambda r: r.update(known_span_languages=['Java', 'Python']),
            lambda r: r.update(parent_coverage_partial=False),
            lambda r: r.update(generated_source_omission=False),
        ]:
            receipt = self.receipt()
            mutate(receipt)
            with self.assertRaises(ValueError):
                smoke.validate_receipt(receipt, '0.4.0-rc.1', 'a' * 64, 'b' * 64)

    def test_test_source_hashes_cover_probe_and_fixture_inputs(self):
        inputs = smoke.source_inputs()
        for name in ['scripts/function_release_smoke.py', 'tests/functions/run.py',
                     'tests/functions/fixtures/Examples.java', 'tests/functions/fixtures/Examples.cs',
                     'tests/functions/fixtures/examples.py', 'tests/structural_breadth/fixtures.json',
                     'tests/structural_breadth/testdata/Example.java']:
            self.assertIn(name, inputs)
        self.assertEqual(21, sum(name.startswith('tests/structural_breadth/testdata/') for name in inputs))


if __name__ == '__main__':
    unittest.main()
