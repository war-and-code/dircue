"""Exercise the harness's rejection boundaries, not candidate metric formulas."""
import copy
import unittest

from run import GROUPS, compare_default, physical_lines, validate_functions


class HarnessTests(unittest.TestCase):
    def test_only_duration_field_may_change_in_default_protocol(self):
        old = (0, b'{"timings_ns":{"parse":1},"value":2}', b'')
        compare_default(old, (0, b'{"timings_ns":{"parse":999},"value":2}', b''))
        for changed in [(0, b'{"timings_ns":{"parse":1},"value":3}', b''),
                        (1, old[1], b''), (0, old[1], b'warning'),
                        (0, old[1] + b'\n', b'')]:
            with self.subTest(changed=changed), self.assertRaises(AssertionError):
                compare_default(old, changed)

    def test_rust_physical_line_semantics(self):
        for text, count in [('', 0), ('x', 1), ('x\n', 1), ('x\r\n', 1),
                            ('x\r', 1), ('x\r\ny', 2), ('\n', 1), ('x\n\n', 2), ('x\u2028y', 1)]:
            self.assertEqual(physical_lines(text), count)

    def test_incomplete_evidence_cannot_claim_complete(self):
        fixture = {'provider': 'big-code-analysis@2.2.0', 'rule': 'space-kind-function',
                   'rule_version': '1.0.0', 'scope': 'file', 'limit': 128, 'name_max_bytes': 256,
                   'metric_scope': 'includes_nested_spaces', 'order': 'provider_preorder',
                   'status': 'complete', 'syntax_errors': False, 'total_spaces': 1,
                   'omitted_spaces': 0, 'invalid_span_spaces': 0,
                   'entries': [{'index': 1, 'name': 'f', 'name_status': 'present', 'start_line': 1,
                                'end_line': 2, 'metrics': {name: {} for name in GROUPS}}]}
        validate_functions(fixture, 'def f():\n    pass\n')
        mutations = [('population', lambda x: x.update(total_spaces=2)),
                     ('span', lambda x: x['entries'][0].update(end_line=3)),
                     ('group', lambda x: x['entries'][0]['metrics'].update(wmc={})),
                     ('omission', lambda x: x.update(omitted_spaces=1, total_spaces=2)),
                     ('recovery', lambda x: x.update(syntax_errors=True)),
                     ('name', lambda x: x['entries'][0].update(name_status='unavailable'))]
        for name, mutate in mutations:
            changed = copy.deepcopy(fixture)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(AssertionError):
                validate_functions(changed, 'def f():\n    pass\n')


if __name__ == '__main__':
    unittest.main()
