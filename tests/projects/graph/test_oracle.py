"""Mutation checks for the independent graph receipt oracle."""
import unittest

import verify


def graph():
    edges = [{'source': a, 'target': b, 'value': b, 'certainty': 'unconditional', 'resolution': 'in_scope', 'included': True} for a, b in [('A.csproj', 'B.csproj'), ('B.csproj', 'A.csproj')]]
    return {'nodes': [{'id': name, 'fan_in': 1, 'fan_out': 1, 'component': 'A.csproj', 'cyclic': True} for name in ['A.csproj', 'B.csproj']], 'edges': edges, 'coverage': {'unique_edges': 2}, 'components': [{'id': 'A.csproj', 'nodes': ['A.csproj', 'B.csproj'], 'edges': 2}], 'cycles': [{'id': 'A.csproj', 'nodes': ['A.csproj', 'B.csproj']}]}


class OracleTests(unittest.TestCase):
    def test_known_cycle(self):
        self.assertEqual(verify.check_graph_math(graph())['cyclic_nodes'], 2)

    def test_rejects_graph_math_mutations(self):
        for field in ['degree', 'component', 'cycle', 'edge-count']:
            with self.subTest(field=field):
                changed = graph()
                if field == 'degree':
                    changed['nodes'][0]['fan_in'] = 2
                elif field == 'component':
                    changed['components'][0]['edges'] = 1
                elif field == 'cycle':
                    changed['cycles'] = []
                else:
                    changed['coverage']['unique_edges'] = 3
                with self.assertRaises(AssertionError):
                    verify.check_graph_math(changed)

    def test_xml_rejects_missing_or_invented_declarations(self):
        manifests = {'A.csproj': b'<Project><ItemGroup><ProjectReference Include="B.csproj"/></ItemGroup></Project>', 'B.csproj': b'<Project><ItemGroup><ProjectReference Include="A.csproj"/></ItemGroup></Project>'}
        def check(value):
            return verify.check_xml(value, {'max_manifest_bytes': 1048576}, set(manifests), manifests)
        self.assertEqual(check(graph())['audited_unique_unconditional_in_scope_pairs'], 2)
        missing = graph()
        missing['edges'].pop()
        with self.assertRaises(AssertionError):
            check(missing)
        invented = graph()
        invented['edges'].append(dict(invented['edges'][0], value='Invented.csproj'))
        with self.assertRaises(AssertionError):
            check(invented)
        conditional = graph()
        conditional['edges'][0]['certainty'] = 'conditional'
        with self.assertRaises(AssertionError):
            check(conditional)


if __name__ == '__main__':
    unittest.main()
