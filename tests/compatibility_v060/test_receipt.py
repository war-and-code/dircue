"""Receipt accounting checks use synthetic captures, never execution evidence."""
import copy
import hashlib
import unittest

import verify


def capture(output=b'{}\n', status=0):
    return {'stdout': output.decode(), 'stdout_sha256': hashlib.sha256(output).hexdigest(),
            'stderr': '', 'stderr_sha256': hashlib.sha256(b'').hexdigest(), 'exit': status}


def receipt():
    groups = {'v020-retained': 118, 'v030-projects': 55, 'v030-structure-validation': 6,
              'v040-modules': 32, 'v050-declarations': 37, 'v030-structure-native': 30}
    rows = [{'id': f'{group}-{number}', 'group': group, 'baseline': capture(),
             'candidate': {'identical_to_baseline': True}, 'equal': True}
            for group, count in groups.items() for number in range(count)]
    return {'schema_version': '1.0.0', 'baseline_release': 'v0.5.0', 'candidate_sha256': 'a' * 64,
            'build_receipt': {'candidate_sha256': 'a' * 64}, 'worker_sha256': 'b' * 64,
            'untested': [], 'cases': rows, 'interface_changes': [], 'total': 278,
            'exact_matches': 278, 'passed': True}


class ReceiptAccounting(unittest.TestCase):
    def test_complete_accounting(self):
        result = verify.verify(receipt())
        self.assertEqual(278, result['exact_matches'])
        self.assertTrue(result['passed'])

    def test_mutated_capture_or_claim_cannot_pass(self):
        for mutate in (
            lambda r: r['cases'][0]['baseline'].update(stdout='different'),
            lambda r: r['cases'][0].update(equal=False),
            lambda r: r['cases'][0].update(candidate=capture(b'changed\n')),
            lambda r: r['cases'][0].update(candidate=capture(status=1)),
            lambda r: r['cases'][1].update(id=r['cases'][0]['id']),
            lambda r: r['cases'].pop(),
            lambda r: r.update(total=277),
            lambda r: r.update(exact_matches=277),
            lambda r: r.update(untested=['native']),
            lambda r: r['build_receipt'].update(candidate_sha256='c' * 64),
        ):
            changed = copy.deepcopy(receipt())
            mutate(changed)
            with self.assertRaises(AssertionError):
                verify.verify(changed)

    def test_honest_mismatch_is_retained(self):
        value = receipt()
        value['cases'][0].update(candidate=capture(status=1), equal=False)
        value.update(passed=False, exact_matches=277)
        self.assertFalse(verify.verify(value)['passed'])


if __name__ == '__main__':
    unittest.main()
