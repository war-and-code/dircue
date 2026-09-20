"""Reject incomplete or misleading proof and runtime evidence."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('bend_experiment', Path(__file__).with_name('run.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def certificate():
    values = [[], [0], [1],
        [0, 1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 63, 64, 127, 128, 255, 256, 257, 65535, 65536, 2147483647, 2147483648, 4294967295],
        list(range(257)), [7] * 257, list(range(4294967279, 4294967296))]
    cases = []
    for row in values:
        bins = [v.bit_length() for v in row]
        cases.append({'values': row, 'bins': bins, 'histogram': [bins.count(i) for i in range(65)], 'count': len(row)})
    return {'cases': cases, 'admitted_pair_certificate': True}


class ProofReceiptTests(unittest.TestCase):
    def test_clean_check_requires_exact_verdict(self):
        self.assertEqual(runner.proof_verdict(b'All terms check.\n', b'', 0), {'status': 'checked', 'unsafe_count': 0})
        for out, err, code in [(b'', b'', 0), (b'All terms check.\n', b'', 1),
                               (b'All terms check.\nextra\n', b'', 0),
                               (b'All terms check.\n', b'warning', 0)]:
            with self.subTest(stdout=out, stderr=err, code=code):
                self.assertNotEqual(runner.proof_verdict(out, err, code)['status'], 'checked')

    def test_unsafe_success_is_partial(self):
        for count in (1, 9):
            text = f'All terms check, with {count} unsafe annotations.\n'.encode()
            self.assertEqual(runner.proof_verdict(text, b'', 0), {'status': 'partial', 'unsafe_count': count})

    def test_independent_reference_accepts_fixed_corpus(self):
        document, observations = runner.validate_certificate(json.dumps(certificate()))
        self.assertEqual(len(document['cases']), 7)
        self.assertEqual(observations, 556)

    def test_forged_or_incomplete_runtime_evidence_rejected(self):
        def wrong_bin(d):
            d['cases'][3]['bins'][1] = 0
        def conserve_but_misplace(d):
            d['cases'][3]['histogram'][0] += 1
            d['cases'][3]['histogram'][1] -= 1
        mutations = [lambda d: d.update(admitted_pair_certificate=False),
                     lambda d: d['cases'].pop(),
                     lambda d: d['cases'][0].update(count=True),
                     lambda d: d['cases'][1].update(histogram=[0] * 65),
                     lambda d: d['cases'][0].update(values=[2**32]),
                     lambda d: d['cases'][0].update(histogram=[0.0] * 65),
                     wrong_bin, conserve_but_misplace]
        for index, mutate in enumerate(mutations):
            altered = copy.deepcopy(certificate())
            mutate(altered)
            with self.subTest(index=index), self.assertRaises(ValueError):
                runner.validate_certificate(json.dumps(altered))

    def test_typed_merge_and_index_admission_are_checked(self):
        value = certificate()
        value['out_of_range_rejected'] = True
        value['all_indices_histogram'] = [1] * 65
        value['singleton_histograms'] = [[int(i == j) for j in range(65)] for i in range(65)]
        for case in value['cases']:
            case['merged_histogram'] = case['histogram'][:]
        runner.validate_certificate(json.dumps(value), typed=True)
        for mutate in (lambda d: d.update(out_of_range_rejected=False),
                       lambda d: d.update(all_indices_histogram=[True] * 65),
                       lambda d: d['singleton_histograms'][40].reverse(),
                       lambda d: d['cases'][3]['merged_histogram'].reverse()):
            altered = copy.deepcopy(value)
            mutate(altered)
            with self.assertRaises(ValueError):
                runner.validate_certificate(json.dumps(altered), typed=True)

    def test_topk_certificate_requires_independent_sort_and_complete_corpus(self):
        cases = []
        for entries in ([{'value': (i * 17) % 13, 'identity': i} for i in range(128)],
                        [{'value': 42, 'identity': i} for i in range(36, -1, -1)], []):
            for limit in (0, 1, 10, 129):
                expected = sorted(entries, key=lambda item: (-item['value'], item['identity']))[:limit]
                cases.append({'entries': entries, 'limit': limit, 'bounded': expected, 'full_prefix': expected, 'partitioned': expected, 'many_partitioned': expected})
        value = {'topk_cases': cases}
        runner.validate_topk(json.dumps(value))
        for mutate in (lambda d: d['topk_cases'].pop(),
                       lambda d: d['topk_cases'][0].update(limit=False),
                       lambda d: d['topk_cases'][2].update(bounded=d['topk_cases'][2]['entries'][:10], full_prefix=d['topk_cases'][2]['entries'][:10]),
                       lambda d: d['topk_cases'][6]['bounded'].reverse(),
                       lambda d: d['topk_cases'][2].update(partitioned=d['topk_cases'][2]['bounded'][:-1]),
                       lambda d: d['topk_cases'][2].update(many_partitioned=d['topk_cases'][2]['bounded'][:-1])):
            altered = copy.deepcopy(value)
            mutate(altered)
            with self.assertRaises(ValueError):
                runner.validate_topk(json.dumps(altered))

    def test_duplicate_keys_and_nonfinite_values_rejected(self):
        for payload in ('{"cases":[],"cases":[]}', '{"cases":NaN}', '{"cases":Infinity}'):
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                runner.validate_certificate(payload)
        with self.assertRaises(ValueError):
            runner.load_certificate('{"topk_cases":[],"topk_cases":[]}')

    def test_production_comparison_rejects_json_type_and_duplicate_key_changes(self):
        expected = {'count': 1, 'histogram': [0, 1]}
        runner.require_json_equal(json.dumps(expected), expected, 'mismatch')
        for payload in ('{"count":true,"histogram":[0,1]}',
                        '{"count":1.0,"histogram":[0,1]}',
                        '{"count":1,"histogram":[false,true]}',
                        '{"count":0,"count":1,"histogram":[0,1]}'):
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                runner.require_json_equal(payload, expected, 'mismatch')


if __name__ == '__main__':
    unittest.main()
