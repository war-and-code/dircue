#!/usr/bin/env python3
"""Capture bounded CLI help and source-defined surfaces without running scans.

Usage: python3 capture_inventory.py BINARY
Writes audit/surface_inventory.jsonl and audit/evidence/baseline_help.jsonl.
Run from any directory. Uses only the supplied binary, local source and Python.
"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

SUITE = Path(__file__).resolve().parents[1]
ROOT = Path(__file__).resolve().parents[3]
AUDIT = SUITE / 'audit'
NOW = datetime.datetime.now(datetime.timezone.utc).isoformat()
SHA = '29b57bd3c126e0f545f361d580b3c397625ce02a'


def source_text(file):
    return subprocess.run(['git', 'show', SHA + ':' + file], cwd=ROOT, check=True,
                          capture_output=True, text=True, timeout=5).stdout


def source_files(directory, suffix):
    result = subprocess.run(['git', 'ls-tree', '-r', '--name-only', SHA, '--', directory], cwd=ROOT,
                            check=True, capture_output=True, text=True, timeout=5)
    return [ROOT / name for name in result.stdout.splitlines() if name.endswith(suffix)]


def segment(raw):
    clean = re.sub(r'[^A-Za-z0-9._-]', '_', raw)
    clean = re.sub(r'^[^A-Za-z0-9]+', '', clean)
    clean = re.sub('_+', '_', clean).rstrip('_')
    suffix = hashlib.sha256(raw.encode()).hexdigest()[:8]
    return ('x' + suffix) if not clean else clean + ('_h' + suffix if clean != raw else '')


def sid(kind, name, subtree=None):
    if kind == 'flag':
        name = name.lstrip('-')
    return '__'.join([kind] + ([segment(subtree)] if subtree else []) + [segment(name)])


def line_of(file, needle):
    for i, line in enumerate(source_text(file).splitlines(), 1):
        if ' '.join(needle.split()) in ' '.join(line.split()):
            return i
    raise ValueError((file, needle))


def dump(path, rows):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(''.join(json.dumps(r, ensure_ascii=True, sort_keys=True) + '\n' for r in rows))


def main():
    global SHA
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', help='Binary whose runtime help is captured')
    parser.add_argument('--source-ref', default=SHA, help='Recorded source reference, read through git show')
    parser.add_argument('--stage', choices=['baseline', 'post'], default='baseline', help='Post capture never overwrites baseline artifacts')
    args = parser.parse_args()
    SHA = subprocess.run(['git', 'rev-parse', args.source_ref], cwd=ROOT, check=True,
                         capture_output=True, text=True, timeout=5).stdout.strip()
    binary = str(Path(args.binary).resolve())
    binary_hash = hashlib.sha256(Path(binary).read_bytes()).hexdigest()
    evidence_name = f'evidence/{args.stage}_help.jsonl'
    previous = AUDIT / evidence_name
    if args.stage == 'baseline' and previous.exists():
        first = json.loads(previous.read_text().splitlines()[0])
        if first.get('binary_sha256') not in (None, binary_hash):
            raise RuntimeError('Refusing to overwrite a baseline captured from another binary; use --stage post.')
    queue = [[]]
    helps = []
    seen = set()
    while queue:
        cmd = queue.pop(0)
        key = ' '.join(cmd)
        if key in seen:
            continue
        seen.add(key)
        p = subprocess.run([binary, *cmd, '--help'], cwd=ROOT, capture_output=True, text=True, timeout=5)
        if p.returncode:
            raise RuntimeError((cmd, p.returncode, p.stderr))
        helps.append(dict(command=cmd, argv=['dircue', *cmd, '--help'], exit_code=p.returncode,
                          stdout=p.stdout, stderr=p.stderr, captured_at=NOW, target_sha=SHA, binary_sha256=binary_hash))
        in_commands = False
        for line in p.stdout.splitlines():
            if line == 'Available Commands:':
                in_commands = True
            elif in_commands:
                m = re.match(r'^  ([a-z][a-z-]*)\s+', line)
                if m:
                    queue.append([*cmd, m.group(1)])
                elif not line.strip():
                    in_commands = False
    dump(AUDIT / evidence_name, helps)
    by_cmd = {' '.join(r['command']): r for r in helps}
    rows = []

    def add(kind, name, subtree, file, line, description, runtime=None, **extra):
        rows.append(dict(surface_id=sid(kind, name, subtree), kind=kind, name=name,
                         subtree=subtree, source=dict(file=file, line=line),
                         description=description, required=False, deprecated=False,
                         mutates=False, target_sha=SHA, discovered_at=NOW,
                         runtime=runtime or {}, **extra))

    for key, h in by_cmd.items():
        cmd = h['command']
        if not cmd:
            file, line = 'internal/cli/cli.go', line_of('internal/cli/cli.go', 'Use:           "dircue')
        elif key == 'compare':
            file, line = 'internal/cli/compare.go', line_of('internal/cli/compare.go', 'Use:   "compare')
        elif key in ('capabilities', 'plan'):
            file, line = 'internal/cli/planning.go', line_of('internal/cli/planning.go', 'Use: "' + key)
        elif key == 'analyze explain':
            file, line = 'internal/cli/explain.go', line_of('internal/cli/explain.go', 'Use: "explain')
        elif key == 'analyze':
            file, line = 'internal/cli/cli.go', line_of('internal/cli/cli.go', 'Use:   "analyze')
        else:
            file, line = 'internal/cli/cli.go', line_of('internal/cli/cli.go', 'Use:   mode') if key != 'help' else line_of('internal/cli/cli.go', 'root.ExecuteContext')
        usage = re.search(r'Usage:\n([^\n]+)', h['stdout']).group(1).strip()
        add('verb', cmd[-1] if cmd else 'dircue', ' '.join(cmd[:-1]) or None,
            file, line, h['stdout'].split('\n\n')[0],
            dict(invocation=' '.join(h['argv']), help_excerpt=h['stdout'], exit_code=0),
            command=cmd, usage=usage, evidence_path='audit/' + evidence_name,
            side_effects='Only structure, or all --structure, executes an explicitly supplied worker; other commands read only. No inspected code is executed.')

    # One record per flag registration, not per repeated inherited help display.
    # Source registrations reused by several analyzers retain every context.
    flags = []
    for file in source_files('internal/cli', '.go'):
        if file.name.endswith('_test.go'):
            continue
        for i, line in enumerate(source_text(str(file.relative_to(ROOT))).splitlines(), 1):
            m = re.search(r'(?:flags|\.Flags\(\))\.(\w+VarP?)\([^,]+, "([a-z0-9-]+)"', line)
            if not m:
                continue
            method, name = m.groups()
            quoted = re.findall(r'"((?:[^"\\]|\\.)*)"', line)
            shorthand = quoted[1] if method.endswith('P') else None
            if file.name == 'planning.go':
                contexts = ['capabilities'] if name in ('cli', 'guide', 'schema') else ['plan']
            elif file.name == 'targeted.go':
                contexts = ['analyze focus']
            elif file.name == 'explain.go':
                contexts = ['analyze explain']
            elif file.name == 'packages.go':
                contexts = ['analyze all', 'analyze packages']
            elif line.strip().startswith('flags.'):
                contexts = list(by_cmd)
            else:
                contexts = [key for key, h in by_cmd.items()
                            if key.startswith('analyze ') and re.search(r'^\s+(?:-\w, )?--' + re.escape(name) + r'(?:\s|$)', h['stdout'], re.M)]
                if name == 'metrics':
                    contexts = ['analyze all']  # focus has a distinct registration.
            subtree = None if line.strip().startswith('flags.') else (contexts[0] if len(contexts) == 1 else 'analyze')
            # Global read-side flags shown on saved commands are rejected at runtime.
            rejected = []
            if subtree is None and name != 'json':
                rejected = ['capabilities', 'plan', 'compare']
            accepted = [key or 'dircue' for key in contexts if key not in rejected]
            restrictions = []
            if rejected:
                restrictions.append('Capabilities/plan/compare reject this inherited scan flag.' +
                                    (' Their help still advertises it.' if args.stage == 'baseline' else ' Their help omits it.'))
            if name in ('source', 'rev', 'tree', 'workers', 'max-file-bytes', 'tree-size', 'on-error'):
                restrictions.append('analyze explain rejects this flag when --report is selected.')
            if name in ('breakdown', 'strategies'):
                restrictions.append('focus and explain reject this flag; several extended modules retain inherited compatibility handling.')
            h = by_cmd[contexts[0]]
            excerpt = next((l for l in h['stdout'].splitlines() if re.search(r'--' + re.escape(name) + r'(?:\s|$)', l)), '')
            dtype = method.removesuffix('P').removesuffix('Var').lower()
            enum = {'source': ['auto', 'git', 'directory'], 'on-error': ['fail', 'continue'], 'metrics-scope': ['source', 'text']}.get(name)
            for visible in ['--' + name] + (['-' + shorthand] if shorthand else []):
                add('flag', visible, subtree, str(file.relative_to(ROOT)), i, quoted[-1],
                    dict(invocation=' '.join(h['argv']), help_excerpt=excerpt, exit_code=0),
                    type='enum' if enum else dtype, enum_values=enum, canonical='--' + name,
                    applies_to=accepted, rejected_by=rejected, restrictions=restrictions)
            flags.append(name)
    for name, short in [('help', 'h'), ('version', 'v')]:
        h = by_cmd['']
        excerpt = next(l for l in h['stdout'].splitlines() if '--' + name in l)
        for visible in ['--' + name, '-' + short]:
            add('flag', visible, None, 'internal/cli/cli.go', line_of('internal/cli/cli.go', 'Version:       Version'),
                'Cobra-generated ' + name + ' flag.', dict(invocation='dircue --help', help_excerpt=excerpt, exit_code=0),
                canonical='--' + name, applies_to=list(by_cmd) if name == 'help' else ['dircue'], type='bool')

    add('exit', '0', None, 'main.go', 16, 'Successful command; differences, partial reports and legacy tree-limit warnings can still be success.', value=0)
    add('exit', '1', None, 'main.go', 18, 'All handled CLI errors print diagnostics to stderr and exit 1.', value=1)
    for signal in ('SIGINT', 'SIGTERM'):
        add('signal', signal, None, 'main.go', 14, 'Cancel the shared context; handled cancellation follows the existing error/exit-1 path.', behavior='graceful_shutdown')
    add('env', 'GOMAXPROCS', None, 'pkg/scanner/scanner.go', 210, 'Go runtime concurrency controls automatic file workers, capped at 16; --workers overrides.', applies_to=['scan commands'])
    add('env', 'SystemRoot', None, 'pkg/structure/worker_process_windows.go', 23, 'Windows worker cancellation locates System32/taskkill.exe under SystemRoot, falling back to C:\\Windows. Source-reviewed only; no Windows runtime probe.', applies_to=['Windows structural worker cancellation'])
    if args.stage == 'post':
        add('env', 'GOMEMLIMIT', None, 'internal/cli/capability_cli.go', line_of('internal/cli/capability_cli.go', '"GOMEMLIMIT"'),
            'Existing Go runtime cooperative memory target, now documented in CLI; not a hard RSS limit or a native worker limit.',
            applies_to=['Go runtime'], inventory_note='Existing runtime surface omitted from the initial inventory; not a newly implemented memory control.')
    for file in source_files('schema', '.schema.json'):
        data = json.loads(source_text(str(file.relative_to(ROOT))))
        name = file.name.removesuffix('.schema.json')
        add('schema', name, None, str(file.relative_to(ROOT)), 1, data.get('title', name),
            schema_dialect=data.get('$schema'), exported_by_binary=args.stage == 'post',
            scope='Schema file may describe a module inside an aggregate; not necessarily the entire matching command output.')
    errors = [
        ('unknown-flag', 'internal/cli/cli.go', 'root.ExecuteContext', 'Parser error lacks a suggested flag.'),
        ('unknown-analysis', 'internal/cli/cli.go', 'choose an analysis', 'Missing or incorrect analyze mode produces a generic choice list.'),
        ('path-arguments', 'internal/cli/cli.go', 'expected at most one', 'More than one scan path is rejected with count only.'),
        ('plan-arguments', 'internal/cli/planning.go', 'Use: "plan', 'Generic Cobra ExactArgs error for missing saved report.'),
        ('compare-arguments', 'internal/cli/compare.go', 'Args:  cobra.ExactArgs', 'Generic Cobra ExactArgs error for missing saved reports.'),
        ('plan-selection', 'pkg/planning/build.go', 'func validateInput', 'Invalid module/question/input/project returns an undifferentiated sentinel.'),
        ('saved-report-open', 'internal/cli/planning.go', 'cannot open saved report', 'Privacy-safe report read failure gives no remediation.'),
        ('scan-flag-scope', 'internal/cli/planning.go', 'does not apply to saved-report', 'Rejects incompatible inherited scan flags.'),
        ('structure-worker', 'internal/cli/cli.go', 'structure.New', 'Missing explicit worker path is rejected without naming CLI flag.'),
        ('focus-selection', 'internal/cli/targeted.go', 'exactly one of', 'Focus requires exactly one explicit project or affected input selector.'),
        ('metrics-prerequisite', 'internal/cli/cli.go', 'requires --metrics', 'Per-file metrics options need explicit --metrics.'),
    ]
    if args.stage == 'post':
        updated = {
            'unknown-flag': ('internal/cli/diagnostics.go', 'func flagErrorWithHint', 'Bounded command-local flag hints preserve failure and do not print supplied values.'),
            'unknown-analysis': ('internal/cli/diagnostics.go', 'func analysisSelectionError', 'Incorrect analysis mode receives canonical guidance without executing a guess.'),
            'plan-arguments': ('internal/cli/planning.go', 'plan requires one', 'Missing saved report names its required input and a valid invocation.'),
            'compare-arguments': ('internal/cli/compare.go', 'compare requires', 'Missing saved reports name the required pair and a valid invocation.'),
            'plan-selection': ('internal/cli/planning.go', 'func validatePlanningSelectionForCLI', 'CLI selection errors identify the invalid selector; library sentinels remain unchanged.'),
            'saved-report-open': ('internal/cli/planning.go', 'cannot open saved report', 'Privacy-safe read failure explains the required report and how to produce it.'),
            'scan-flag-scope': ('internal/cli/planning.go', 'does not apply to', 'Rejects incompatible inherited scan flags and names the scan workflow.'),
            'structure-worker': ('internal/cli/cli.go', 'structure requires --structural-worker', 'Missing worker names the explicit trusted executable option.'),
        }
        errors = [(name, *updated.get(name, (file, needle, description))) for name, file, needle, description in errors]
    for name, file, needle, description in errors:
        add('error', name, None, file, line_of(file, needle), description, exit_code=1)
    ids = [r['surface_id'] for r in rows]
    if len(ids) != len(set(ids)):
        raise ValueError('duplicate IDs: ' + str([x for x in ids if ids.count(x) > 1]))
    rows.sort(key=lambda r: r['surface_id'])
    dump(AUDIT / ('surface_inventory.jsonl' if args.stage == 'baseline' else 'surface_inventory_post.jsonl'), rows)
    if args.stage == 'baseline':
        dump(AUDIT / 'surface_inventory_baseline.jsonl', rows)
    print(json.dumps(dict(surfaces=len(rows), commands=len(helps), kinds={k: sum(r['kind'] == k for r in rows) for k in sorted(set(r['kind'] for r in rows))})))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
