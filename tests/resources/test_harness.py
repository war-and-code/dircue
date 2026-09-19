#!/usr/bin/env python3
"""Regressions for refusing incomplete or failed resource-probe reports."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import run
import supervisor


class AcceptanceTests(unittest.TestCase):
    def test_rejects_failure_even_with_valid_complete_json(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            data = b'{"languages":[],"projects":{"status":"partial"}}\n'
            (root / 'stdout.txt').write_bytes(data)
            base = {'docker': {'State': {'OOMKilled': False, 'ExitCode': 0}},
                    'probe': {'exit_code': 0, 'failure': None,
                              'before': {'memory.events': 'oom 0\noom_kill 0'},
                              'after': {'memory.events': 'oom 0\noom_kill 0'},
                              'output_sha256': {'stdout': hashlib.sha256(data).hexdigest()}}}
            valid = run.assess(copy.deepcopy(base), root)
            self.assertTrue(valid['accepted'])
            self.assertEqual(valid['module_statuses'], {'projects': 'partial'})
            for mutate in [lambda r: r['docker']['State'].update(OOMKilled=True),
                           lambda r: r['docker']['State'].update(ExitCode=137),
                           lambda r: r.update(probe=None),
                           lambda r: r['probe'].update(exit_code=-9),
                           lambda r: r['probe'].update(failure='timeout'),
                           lambda r: r['probe'].update(failure='output_limit'),
                           lambda r: r['probe']['after'].update({'memory.events': 'oom 1\noom_kill 1'})]:
                record = copy.deepcopy(base)
                mutate(record)
                self.assertFalse(run.assess(record, root)['accepted'])
            (root / 'stdout.txt').write_bytes(b'{')
            self.assertFalse(run.assess(copy.deepcopy(base), root)['accepted'])

    def test_supervisor_bounds_output_and_timeout(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(supervisor, 'snapshot', return_value={}):
            root = Path(temporary)
            output = supervisor.run([sys.executable, '-c', 'import os; os.write(1,b"x"*100000)'],
                                    root, timeout=5, output_limit=1024)
            self.assertEqual(output['failure'], 'output_limit')
            self.assertEqual((root / 'stdout.txt').stat().st_size, 1024)
            timeout = supervisor.run([sys.executable, '-c', 'import time; time.sleep(30)'],
                                     root, timeout=0.05, output_limit=1024)
            self.assertEqual(timeout['failure'], 'timeout')
            self.assertNotEqual(timeout['exit_code'], 0)
            self.assertLess(timeout['wall_seconds'], 5)


if __name__ == '__main__':
    unittest.main()
