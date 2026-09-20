#!/usr/bin/env python3
"""Check static .NET graph reports against XML declarations and graph mathematics."""
import argparse
import collections
import hashlib
import io
import json
import os
import pathlib
import posixpath
import subprocess
import xml.etree.ElementTree as ET

PROJECT_SUFFIXES = {'.csproj', '.fsproj', '.vbproj'}
MAX_ORACLE_BYTES = 128 << 20


def digest(data):
    return hashlib.sha256(data).hexdigest()


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, timeout=180, **kwargs)


def read_inventory(root, mode):
    """Read ordinary selected files; never follow symlinks or execute checkout code."""
    inventory, manifests = set(), {}
    identity = {'mode': mode}
    if mode == 'git':
        identity['commit'] = command(['git', '-C', str(root), 'rev-parse', 'HEAD']).stdout.decode().strip()
        identity['tree'] = command(['git', '-C', str(root), 'rev-parse', 'HEAD^{tree}']).stdout.decode().strip()
        listing = command(['git', '-C', str(root), 'ls-tree', '-rlz', 'HEAD']).stdout
        selected = []
        for entry in listing.split(b'\0'):
            if not entry:
                continue
            metadata, filename = entry.split(b'\t', 1)
            mode_bits, kind, oid, size = metadata.split()
            if kind != b'blob' or mode_bits not in {b'100644', b'100755'}:
                continue
            name = filename.decode()
            inventory.add(name)
            if pathlib.PurePosixPath(name).suffix.lower() in PROJECT_SUFFIXES:
                selected.append((name, oid, int(size)))
        assert sum(size for _, _, size in selected) <= MAX_ORACLE_BYTES
        body = command(['git', '-C', str(root), 'cat-file', '--batch'], input=b''.join(oid + b'\n' for _, oid, _ in selected)).stdout
        stream = io.BytesIO(body)
        for name, oid, size in selected:
            actual_oid, kind, actual_size = stream.readline().strip().split()
            assert (actual_oid, kind, int(actual_size)) == (oid, b'blob', size)
            manifests[name] = stream.read(size)
            assert stream.read(1) == b'\n'
        assert not stream.read()
    else:
        for directory, dirs, files in os.walk(root, followlinks=False):
            dirs[:] = [d for d in dirs if d != '.git' and not pathlib.Path(directory, d).is_symlink()]
            for filename in files:
                file = pathlib.Path(directory, filename)
                if file.is_symlink() or not file.is_file():
                    continue
                name = file.relative_to(root).as_posix()
                inventory.add(name)
                if file.suffix.lower() in PROJECT_SUFFIXES:
                    manifests[name] = file.read_bytes()
        assert sum(map(len, manifests.values())) <= MAX_ORACLE_BYTES
    identity['regular_files'] = len(inventory)
    identity['project_xml_inventory_sha256'] = digest(b''.join(name.encode() + b'\0' + digest(data).encode() + b'\n' for name, data in sorted(manifests.items())))
    return inventory, manifests, identity


def local(tag):
    return tag.rsplit('}', 1)[-1]


def xml_observations(manifests, max_bytes):
    """An independent XML parser; no MSBuild property or target evaluation."""
    roots, references, omitted = set(), [], collections.Counter()
    for name, content in sorted(manifests.items()):
        if len(content) > max_bytes:
            omitted['over-manifest-limit'] += 1
            continue
        if b'<!DOCTYPE' in content or b'<!ENTITY' in content:
            omitted['xml-directive'] += 1
            continue
        try:
            # The production reader supports UTF-8 XML, unlike ElementTree's wider codecs.
            content.decode('utf-8-sig')
            document = ET.fromstring(content)
        except (UnicodeDecodeError, ET.ParseError):
            omitted['encoding-or-malformed-xml'] += 1
            continue
        if local(document.tag).lower() != 'project':
            omitted['other-root'] += 1
            continue
        roots.add(name)
        todo = [(document, False, ())]
        while todo:
            element, conditional, ancestors = todo.pop()
            conditional = conditional or bool(element.attrib.get('Condition', '').strip())
            label = local(element.tag)
            if label == 'ProjectReference':
                for part in element.attrib.get('Include', '').split(';'):
                    value = part.strip()
                    if value:
                        references.append({'source': name, 'value': value, 'conditional': conditional,
                                           'opaque_container': any(a in {'Choose', 'When', 'Otherwise', 'Target', 'UsingTask', 'ProjectExtensions'} for a in ancestors)})
            for child in reversed(list(element)):
                todo.append((child, conditional, ancestors + (label,)))
    return roots, references, omitted


def normalized_target(source, value):
    if any(marker in value for marker in ('$(', '@(', '%(')) or any(c in value for c in '*?[]%\x00\r\n'):
        return None, 'unresolved'
    value = value.replace('\\', '/')
    if value.startswith('/') or ':' in value:
        return None, 'external'
    target = posixpath.normpath(posixpath.join(posixpath.dirname(source), value))
    if target == '..' or target.startswith('../'):
        return None, 'external'
    return target, None


def check_xml(graph, projects, inventory, manifests):
    roots, observations, omitted = xml_observations(manifests, projects['max_manifest_bytes'])
    ids = {row['id'] for row in graph['nodes']}
    assert roots == ids, {'missing_nodes': sorted(roots - ids), 'unexpected_nodes': sorted(ids - roots), 'omitted': dict(omitted)}
    by_value = collections.defaultdict(list)
    for edge in graph['edges']:
        by_value[(edge['source'], edge['value'])].append(edge)
    assert set(by_value) <= {(row['source'], row['value']) for row in observations}, 'graph invented a declaration absent from project XML'
    audited, skipped = collections.Counter(), collections.Counter()
    direct_literal_pairs = set()
    for observation in observations:
        source, value = observation['source'], observation['value']
        candidates = by_value[(source, value)]
        assert candidates, ('missing XML declaration', observation)
        if observation['opaque_container']:
            skipped['target-or-choose-container'] += 1
            continue
        target, resolution = normalized_target(source, value)
        if resolution is None:
            resolution = 'missing' if target not in inventory else 'in_scope' if target in ids else 'unrecognized_target'
        certainty = 'unresolved' if target is None else 'conditional' if observation['conditional'] else 'unconditional'
        expected_included = resolution == 'in_scope' and certainty == 'unconditional'
        matching = [edge for edge in candidates if edge['resolution'] == resolution and edge['certainty'] == certainty and edge['included'] == expected_included and (target is None or edge.get('target') == target)]
        assert matching, ('XML/reference disagreement', observation, resolution, certainty, candidates)
        audited[certainty + '/' + resolution] += 1
        if expected_included:
            direct_literal_pairs.add((source, target))
    reported_pairs = {(edge['source'], edge['target']) for edge in graph['edges'] if edge['included']}
    assert direct_literal_pairs <= reported_pairs
    unaudited_pairs = reported_pairs - direct_literal_pairs
    return {'included_pairs_outside_literal_oracle': len(unaudited_pairs), 'parsed_projects': len(roots), 'xml_reference_observations': len(observations), 'audited_observations': dict(sorted(audited.items())), 'unaudited_container_observations': dict(skipped), 'omitted_xml': dict(omitted), 'audited_unique_unconditional_in_scope_pairs': len(direct_literal_pairs)}


def check_graph_math(graph):
    """Use bitset transitive closure, rather than the production SCC traversal."""
    names = [node['id'] for node in graph['nodes']]
    assert names == sorted(set(names))
    indices = {name: index for index, name in enumerate(names)}
    pairs = {(edge['source'], edge['target']) for edge in graph['edges'] if edge['included']}
    assert len(pairs) == graph['coverage']['unique_edges']
    assert all(edge['certainty'] == 'unconditional' and edge['resolution'] == 'in_scope' for edge in graph['edges'] if edge['included'])
    outgoing, incoming = collections.Counter(a for a, _ in pairs), collections.Counter(b for _, b in pairs)
    undirected = {name: set() for name in names}
    reach = [1 << index for index in range(len(names))]
    for a, b in pairs:
        undirected[a].add(b)
        undirected[b].add(a)
        reach[indices[a]] |= 1 << indices[b]
    for pivot in range(len(names)):
        bit, reachable = 1 << pivot, reach[pivot]
        for index in range(len(names)):
            if reach[index] & bit:
                reach[index] |= reachable
    components = []
    unseen = set(names)
    component_of = {}
    while unseen:
        first = min(unseen)
        todo, members = [first], set()
        while todo:
            node = todo.pop()
            if node in members:
                continue
            members.add(node)
            todo.extend(undirected[node] - members)
        unseen -= members
        members = sorted(members)
        for node in members:
            component_of[node] = members[0]
        components.append({'id': members[0], 'nodes': members, 'edges': sum(1 for a, b in pairs if a in members and b in members)})
    assert components == graph['components']
    cycles, assigned = [], set()
    for name in names:
        if name in assigned:
            continue
        index = indices[name]
        members = [other for other in names if reach[index] & (1 << indices[other]) and reach[indices[other]] & (1 << index)]
        assigned.update(members)
        if len(members) > 1 or (name, name) in pairs:
            cycles.append({'id': members[0], 'nodes': members})
    assert cycles == graph['cycles']
    cyclic = {name for cycle in cycles for name in cycle['nodes']}
    for node in graph['nodes']:
        assert (node['fan_in'], node['fan_out'], node['component'], node['cyclic']) == (incoming[node['id']], outgoing[node['id']], component_of[node['id']], node['id'] in cyclic), node
    return {'nodes': len(names), 'unique_edges': len(pairs), 'weak_components': len(components), 'cyclic_strong_components': len(cycles), 'cyclic_nodes': len(cyclic)}


def run_case(binary, name, mode, root, cache, expected=None):
    inventory, manifests, identity = read_inventory(root, mode)
    cmd = [str(binary), 'analyze', 'graph', '--json', '--source', mode, str(root)]
    first = command(cmd)
    second = command(cmd)
    assert (first.stdout, first.stderr) == (second.stdout, second.stderr), name + ': nondeterministic repeated output'
    report = json.loads(first.stdout)
    cache.mkdir(parents=True, exist_ok=True)
    (cache / (name + '.json')).write_bytes(first.stdout)
    graph = report['graph']
    assert graph['provider_version'] == '1.0.0' and graph['kind'] == 'dotnet-project-reference'
    if mode == 'git':
        assert graph['tree'] == identity['tree']
    xml = check_xml(graph, report['projects'], inventory, manifests)
    math = check_graph_math(graph)
    result = {'name': name, 'source': identity, 'graph_status': graph['status'], 'project_status': report['projects']['status'], 'graph_coverage': graph['coverage'], 'xml_oracle': xml, 'graph_math': math, 'stdout_sha256': digest(first.stdout), 'stderr_sha256': digest(first.stderr), 'stderr_bytes': len(first.stderr), 'checks': ['byte-identical-repeated-output', 'selected-source-tree', 'independent-XML-declarations', 'unique-degree-counts', 'weak-components', 'transitive-closure-cycle-oracle', 'conditional-and-unresolved-edge-exclusion']}
    if expected is not None:
        oracle = json.loads(expected.read_text())
        assert set(oracle['projects']) == {node['id'] for node in graph['nodes']}
        assert {(row['from'], row['to']) for row in oracle['edges']} == {(edge['source'], edge['target']) for edge in graph['edges'] if edge['included']}
        assert len(graph['nodes']) == 2048 and max(len(pathlib.PurePosixPath(node['id']).parts) for node in graph['nodes']) >= 24
        result['original_stress_graph_sha256'] = digest(expected.read_bytes())
        result['checks'].append('original-2048-stress-generator-edge-set')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path, required=True)
    parser.add_argument('--build-inputs', type=pathlib.Path, required=True)
    parser.add_argument('--cache', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    parser.add_argument('--synthetic-expected', type=pathlib.Path)
    parser.add_argument('--pins', type=pathlib.Path, default=pathlib.Path('tests/performance/corpus.json'))
    parser.add_argument('cases', nargs='+', help='NAME=git|directory=PATH; name synthetic2048 enables the extra original-fixture oracle')
    args = parser.parse_args()
    binary = args.binary.resolve()
    inputs = json.loads(args.build_inputs.read_text())
    for name, value in inputs['files'].items():
        assert digest(pathlib.Path(name).read_bytes()) == value, 'build source changed: ' + name
    pins = {row['name']: row for row in json.loads(args.pins.read_text())['projects']}
    cases = []
    for value in args.cases:
        name, mode, root = value.split('=', 2)
        assert mode in {'git', 'directory'}
        result = run_case(binary, name, mode, pathlib.Path(root).resolve(), args.cache, args.synthetic_expected if name == 'synthetic2048' else None)
        if name in pins:
            assert result['source']['commit'] == pins[name]['commit']
            result['upstream'] = {'url': pins[name]['url'], 'commit': pins[name]['commit'], 'tag': pins[name]['tag']}
        cases.append(result)
        print(name, result['graph_math'], flush=True)
    receipt = {'scope': 'Static declaration graphs, not evaluated MSBuild membership. XML oracle audits literal references and explicit inherited conditions; target/Choose containers remain outside its certainty oracle. Graph mathematics is checked over every reported included edge.', 'binary_sha256': digest(binary.read_bytes()), 'source_commit': inputs['source_commit'], 'source_dirty': inputs['dirty'], 'build_inputs_sha256': digest(args.build_inputs.read_bytes()), 'harness_sha256': digest(pathlib.Path(__file__).read_bytes()), 'cases': cases, 'passed': True}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
