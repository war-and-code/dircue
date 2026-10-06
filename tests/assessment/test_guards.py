"""Verify acceptance guards reject independently planted output corruptions."""
import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('assessment_acceptance', Path(__file__).with_name('run.py'))
h = importlib.util.module_from_spec(spec)
spec.loader.exec_module(h)

CURRENT_ASSESSMENT_VERSION = '1.1.0'
CURRENT_PROFILE_SCHEMA_VERSION = '1.10.0'


def metric(n):
    return dict(count=n, scope='synthetic population', completeness='complete', reasons=[])


def assessment():
    lock = {'ecosystem': 'npm', **{k: metric(0) for k in ('projects', 'eligible', *h.STATES)}}
    for k in ('projects', 'eligible', 'covered'):
        lock[k] = metric(2)
    lock['by_role'] = [dict(role='primary', projects=2, eligible=2, covered=2, missing=0, not_applicable=0,
                            unsupported=0, unknown=0)]
    lock['outcome_reasons'] = []
    overall = copy.deepcopy(lock); overall['ecosystem'] = 'all'
    return dict(version=CURRENT_ASSESSMENT_VERSION,
                source=dict(mode='directory', consistency='live_directory_metadata'),
                inventory=dict(files=metric(6), bytes=metric(80), vendored_files=metric(1), vendored_bytes=metric(10)),
                manifest_candidates=[dict(filename='package.json', kind='manifest', ecosystem='npm', files=2, bytes=30)],
                candidate_evidence=[dict(path='original/a/package.json', root='original/a', kind='manifest')],
                projects=metric(2), project_roots=metric(2), workspace_membership=metric(0),
                projects_by_role=[dict(role='primary', count=2)], project_roots_by_role=[dict(role='primary', count=2)],
                local_dependencies=metric(1), lockfiles=[lock], lockfiles_overall=overall)


class Guards(unittest.TestCase):
    def test_noninteger_and_negative_counts_rejected(self):
        for v in (-1, True, 1.5):
            with self.assertRaises(AssertionError):
                h.count(metric(v))

    def test_addition_guard_checks_both_bytes_and_population(self):
        a = assessment(); b = copy.deepcopy(a)
        b['inventory']['files'] = metric(7); b['inventory']['bytes'] = metric(85)
        h.assert_added_files(a, b, 1, 5)
        b['projects']['count'] += 1
        with self.assertRaises(AssertionError):
            h.assert_added_files(a, b, 1, 5)

    def test_replication_checks_missing_state_not_just_project_total(self):
        a = assessment(); b = copy.deepcopy(a)
        for m in b['inventory'].values(): m['count'] *= 2
        b['manifest_candidates'][0]['files'] *= 2
        for k in h.METRICS: b[k]['count'] *= 2
        for m in b['lockfiles'][0].values():
            if isinstance(m, dict): m['count'] *= 2
        h.assert_scaled(a, b, 2)
        b['lockfiles'][0]['missing']['count'] += 1
        with self.assertRaises(AssertionError): h.assert_scaled(a, b, 2)

    def test_rename_guard_checks_roots_and_evidence_paths(self):
        a = assessment(); b = copy.deepcopy(a)
        b['candidate_evidence'][0].update(path='renamed/a/package.json', root='renamed/a')
        h.assert_renamed(a, b, 'original', 'renamed')
        b['candidate_evidence'][0]['root'] = 'outside'
        with self.assertRaises(AssertionError): h.assert_renamed(a, b, 'original', 'renamed')

    def test_subtree_guard_rejects_parent_workspace_leak(self):
        child = assessment(); parent = copy.deepcopy(child)
        parent['inventory']['files']['count'] += 1; parent['inventory']['bytes']['count'] += 1
        h.assert_subtree(parent, child, 6, 80, 2)
        child['workspace_membership']['count'] = 1
        with self.assertRaises(AssertionError): h.assert_subtree(parent, child, 6, 80, 2)

    def test_lock_removal_guard_checks_state_transfer(self):
        a = assessment(); b = copy.deepcopy(a)
        b['lockfiles'][0]['covered']['count'] -= 1; b['lockfiles'][0]['missing']['count'] += 1
        h.assert_lock_removed(a, b)
        b['lockfiles'][0]['unknown']['count'] += 1
        with self.assertRaises(AssertionError): h.assert_lock_removed(a, b)

    def test_partition_guard_rejects_double_counted_overall(self):
        report = dict(schema_version=CURRENT_PROFILE_SCHEMA_VERSION, assessment=assessment(), languages=[], summary={'language_bytes': 0})
        h.Runner.basic(report)
        report['assessment']['lockfiles_overall']['covered']['count'] += 1
        with self.assertRaises(AssertionError): h.Runner.basic(report)

    def test_partition_guard_rejects_inconsistent_roles_reasons_and_eligibility(self):
        def report():
            return dict(schema_version=CURRENT_PROFILE_SCHEMA_VERSION, assessment=assessment(), languages=[], summary={'language_bytes': 0})
        h.Runner.basic(report())
        corruptions = [
            lambda a: a['inventory']['vendored_files'].update(count=7),
            lambda a: a['projects_by_role'][0].update(count=3),
            lambda a: a['lockfiles'][0]['by_role'][0].update(covered=1),
            lambda a: (a['lockfiles'][0]['eligible'].update(count=1), a['lockfiles_overall']['eligible'].update(count=1),
                       a['lockfiles'][0]['by_role'][0].update(eligible=1), a['lockfiles_overall']['by_role'][0].update(eligible=1)),
            lambda a: a['lockfiles'][0]['outcome_reasons'].append(dict(state='missing', reason='lockfile-not-present', count=1)),
        ]
        for corrupt in corruptions:
            r = report(); corrupt(r['assessment'])
            with self.assertRaises(AssertionError): h.Runner.basic(r)

    def test_equivalence_guard_checks_local_relationships(self):
        a = assessment(); b = copy.deepcopy(a)
        h.assert_same_populations(a, b)
        b['local_dependencies']['count'] += 1
        with self.assertRaises(AssertionError): h.assert_same_populations(a, b)


if __name__ == '__main__':
    unittest.main()
