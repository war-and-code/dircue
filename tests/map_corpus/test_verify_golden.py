#!/usr/bin/env python3
"""Unit tests for verify_golden.py."""
import unittest

from verify_golden import (
    ECOSYSTEM_ALIASES,
    DEPLOYABLE_KIND_ALIASES,
    INTERFACE_KIND_ALIASES,
    match_component,
    match_deployable,
    match_interface,
    match_capability,
    match_edge,
    node_matches_endpoint,
    oracle_set,
    is_oracle_scoped,
    score_components,
    score_deployables,
    score_interfaces,
    score_capabilities,
    score_edges,
    normalize,
    resolve_kind,
)


def _make_node(kind, name, *, root=".", ecosystem="", ikind="", dkind="",
               paths=None, evidence_paths=None, owning_component=""):
    """Helper to build a minimal dircue node."""
    ev = [{"path": p} for p in (evidence_paths or [])]
    props = {}
    if ecosystem:
        props["ecosystem"] = ecosystem
        props["root"] = root
    if ikind:
        props["interface_kind"] = ikind
    if dkind:
        props["kind"] = dkind
    if owning_component:
        props["owning_component"] = owning_component
    return {
        "kind": kind,
        "name": name,
        "paths": paths or [],
        "evidence": ev,
        "properties": props,
    }


def _make_edge(etype, from_id, to_id, *, evidence_paths=None):
    ev = [{"path": p} for p in (evidence_paths or [])]
    return {"type": etype, "from": from_id, "to": to_id, "evidence": ev}


class TestNormalize(unittest.TestCase):
    def test_lowercase(self):
        self.assertEqual(normalize("Mastodon"), "mastodon")

    def test_strip(self):
        self.assertEqual(normalize("  ruff  "), "ruff")


class TestResolveKind(unittest.TestCase):
    def test_known_alias(self):
        self.assertIn("ruby", resolve_kind("bundler", ECOSYSTEM_ALIASES))

    def test_unknown_passthrough(self):
        self.assertEqual(resolve_kind("foobar", ECOSYSTEM_ALIASES), {"foobar"})

    def test_deployable_terraform(self):
        self.assertIn("infrastructure", resolve_kind("terraform_module", DEPLOYABLE_KIND_ALIASES))

    def test_deployable_function(self):
        self.assertIn("infrastructure", resolve_kind("function", DEPLOYABLE_KIND_ALIASES))


class TestMatchComponent(unittest.TestCase):
    def test_exact_match(self):
        lb = {"name": "@mastodon/streaming", "root": "streaming", "ecosystem": "npm"}
        node = _make_node("component", "@mastodon/streaming", root="streaming",
                          ecosystem="npm", evidence_paths=["streaming/package.json"])
        self.assertTrue(match_component(lb, node))

    def test_case_insensitive_name(self):
        # dircue reports "Mastodon" but label says "mastodon"
        lb = {"name": "mastodon", "root": ".", "ecosystem": "bundler"}
        node = _make_node("component", "Mastodon", root=".", ecosystem="ruby",
                          evidence_paths=["Gemfile"])
        self.assertTrue(match_component(lb, node))

    def test_ecosystem_alias_bundler(self):
        lb = {"name": "mastodon", "root": ".", "ecosystem": "bundler"}
        node = _make_node("component", "mastodon", root=".", ecosystem="ruby")
        self.assertTrue(match_component(lb, node))

    def test_ecosystem_alias_pyproject(self):
        lb = {"name": "apache_superset", "root": ".", "ecosystem": "pyproject"}
        node = _make_node("component", "apache_superset", root=".", ecosystem="python-uv")
        self.assertTrue(match_component(lb, node))

    def test_ecosystem_alias_gomod(self):
        lb = {"name": "github.com/grafana/loki/v3", "root": ".", "ecosystem": "gomod"}
        node = _make_node("component", "github.com/grafana/loki/v3", root=".", ecosystem="go")
        self.assertTrue(match_component(lb, node))

    def test_wrong_ecosystem(self):
        lb = {"name": "ruff", "root": ".", "ecosystem": "cargo"}
        node = _make_node("component", "ruff", root=".", ecosystem="npm")
        self.assertFalse(match_component(lb, node))

    def test_wrong_root(self):
        lb = {"name": "ruff", "root": "crates/ruff", "ecosystem": "cargo"}
        node = _make_node("component", "ruff", root=".", ecosystem="cargo")
        self.assertFalse(match_component(lb, node))

    def test_root_normalization(self):
        lb = {"name": "ruff", "root": ".", "ecosystem": "cargo"}
        node = _make_node("component", "ruff", root=".", ecosystem="cargo")
        self.assertTrue(match_component(lb, node))

    def test_non_component_node(self):
        lb = {"name": "ruff", "root": ".", "ecosystem": "cargo"}
        node = _make_node("deployable", "ruff", dkind="container_build")
        self.assertFalse(match_component(lb, node))


class TestMatchDeployable(unittest.TestCase):
    def test_exact_name_and_kind(self):
        lb = {"kind": "service", "name": "web", "path": "docker-compose.yml"}
        node = _make_node("deployable", "web", dkind="service",
                          evidence_paths=["docker-compose.yml"])
        self.assertTrue(match_deployable(lb, node))

    def test_terraform_module_alias(self):
        lb = {"kind": "terraform_module", "name": "(root)", "path": "."}
        node = _make_node("deployable", "(root)", dkind="infrastructure",
                          paths=["."], evidence_paths=["versions.tf"])
        self.assertTrue(match_deployable(lb, node))

    def test_helm_alias(self):
        lb = {"kind": "helm", "name": "loki", "path": "production/helm/loki"}
        node = _make_node("deployable", "loki", dkind="infrastructure",
                          paths=["production/helm/loki/Chart.yaml"],
                          evidence_paths=["production/helm/loki/Chart.yaml"])
        self.assertTrue(match_deployable(lb, node))

    def test_function_alias(self):
        lb = {"kind": "function", "name": "GetOrderFunction", "path": "template.yaml"}
        node = _make_node("deployable", "GetOrderFunction", dkind="infrastructure",
                          paths=["template.yaml"],
                          evidence_paths=["template.yaml"])
        self.assertTrue(match_deployable(lb, node))

    def test_container_build_by_path(self):
        # dircue may name the container_build "(root)" but label says "ruff"
        lb = {"kind": "container_build", "name": "ruff", "path": "Dockerfile"}
        node = _make_node("deployable", "(root)", dkind="container_build",
                          paths=["Dockerfile"],
                          evidence_paths=["Dockerfile"])
        self.assertTrue(match_deployable(lb, node))

    def test_wrong_kind(self):
        lb = {"kind": "service", "name": "web", "path": "docker-compose.yml"}
        node = _make_node("deployable", "web", dkind="workload",
                          evidence_paths=["docker-compose.yml"])
        self.assertFalse(match_deployable(lb, node))

    def test_wrong_name_no_path_overlap(self):
        lb = {"kind": "service", "name": "web", "path": "docker-compose.yml"}
        node = _make_node("deployable", "db", dkind="service",
                          paths=["other-compose.yml"],
                          evidence_paths=["other-compose.yml"])
        self.assertFalse(match_deployable(lb, node))


class TestMatchInterface(unittest.TestCase):
    def test_declared_port_colon_prefix(self):
        lb = {"kind": "declared_port", "name": "3000", "path": "Dockerfile"}
        node = _make_node("interface", "port:3000", ikind="declared_port")
        self.assertTrue(match_interface(lb, node))

    def test_declared_port_exact(self):
        lb = {"kind": "declared_port", "name": "4000", "path": "streaming/Dockerfile"}
        node = _make_node("interface", "port:4000", ikind="declared_port")
        self.assertTrue(match_interface(lb, node))

    def test_declared_port_wrong_number(self):
        lb = {"kind": "declared_port", "name": "3000", "path": "Dockerfile"}
        node = _make_node("interface", "port:4000", ikind="declared_port")
        self.assertFalse(match_interface(lb, node))

    def test_cli_binary_go(self):
        lb = {"kind": "cli_binary", "name": "loki", "path": "cmd/loki/main.go"}
        node = _make_node("interface", "loki", ikind="binary",
                          evidence_paths=["cmd/loki/main.go"])
        self.assertTrue(match_interface(lb, node))

    def test_cli_binary_rust(self):
        lb = {"kind": "cli_binary", "name": "ruff", "path": "crates/ruff"}
        node = _make_node("interface", "ruff", ikind="cargo-default-run",
                          evidence_paths=["crates/ruff/Cargo.toml"])
        self.assertTrue(match_interface(lb, node))

    def test_grpc_service_short_name(self):
        # Label uses "Querier", dircue uses "logproto.Querier"
        lb = {"kind": "grpc_service", "name": "Querier", "path": "pkg/logproto/logproto.proto"}
        node = _make_node("interface", "logproto.Querier", ikind="service")
        self.assertTrue(match_interface(lb, node))

    def test_grpc_service_exact_name(self):
        lb = {"kind": "grpc_service", "name": "logproto.Querier", "path": "x.proto"}
        node = _make_node("interface", "logproto.Querier", ikind="service")
        self.assertTrue(match_interface(lb, node))

    def test_npm_start_matches_start_script(self):
        lb = {"kind": "npm_start", "name": "streaming server", "path": "streaming/package.json"}
        node = _make_node("interface", "start", ikind="script")
        self.assertTrue(match_interface(lb, node))

    def test_spring_boot_application(self):
        lb = {"kind": "spring_boot_application", "name": "spring-petclinic", "path": "pom.xml"}
        node = _make_node("interface", "PetClinicApplication", ikind="spring_boot_application")
        # Name doesn't match directly, only kind matches
        # spring_boot_application uses generic name match — expect False
        self.assertFalse(match_interface(lb, node))


class TestMatchCapability(unittest.TestCase):
    def test_exact_match(self):
        lb = {"capability": "cache:redis", "owner": "mastodon", "path": "Gemfile"}
        node = _make_node("capability", "cache:redis")
        self.assertTrue(match_capability(lb, node))

    def test_no_match(self):
        lb = {"capability": "cache:redis", "owner": "mastodon", "path": "Gemfile"}
        node = _make_node("capability", "cache:memcached")
        self.assertFalse(match_capability(lb, node))

    def test_dynamodb_no_alias(self):
        # datastore:dynamodb has no alias; a dircue node with that name would match
        lb = {"capability": "datastore:dynamodb", "owner": "x"}
        node = _make_node("capability", "datastore:dynamodb")
        self.assertTrue(match_capability(lb, node))


class TestMatchEdge(unittest.TestCase):
    def _map(self):
        nodes = {
            "c1": {"id": "c1", "kind": "component", "name": "mastodon", "properties": {}},
            "c2": {"id": "c2", "kind": "capability", "name": "cache:redis", "properties": {}},
            "d1": {"id": "d1", "kind": "deployable", "name": "web", "paths": ["docker-compose.yml"], "properties": {}},
            "d2": {"id": "d2", "kind": "deployable", "name": "db", "paths": ["docker-compose.yml"], "properties": {}},
        }
        return nodes

    def test_uses_capability(self):
        nodes = self._map()
        lb = {"type": "uses_capability", "from": "mastodon", "to": "cache:redis"}
        edge = _make_edge("uses_capability", "c1", "c2")
        self.assertTrue(match_edge(lb, edge, nodes))

    def test_depends_on_compose(self):
        nodes = self._map()
        lb = {"type": "depends_on", "from": "docker-compose.yml:web", "to": "docker-compose.yml:db"}
        edge = _make_edge("depends_on", "d1", "d2")
        self.assertTrue(match_edge(lb, edge, nodes))

    def test_wrong_type(self):
        nodes = self._map()
        lb = {"type": "builds", "from": "mastodon", "to": "cache:redis"}
        edge = _make_edge("uses_capability", "c1", "c2")
        self.assertFalse(match_edge(lb, edge, nodes))


class TestOracleScoped(unittest.TestCase):
    def test_oracle_set(self):
        entry = {"oracle_files": [{"path": "Gemfile", "sha256": "abc"},
                                   {"path": "Dockerfile", "sha256": "def"}]}
        self.assertEqual(oracle_set(entry), {"Gemfile", "Dockerfile"})

    def test_is_oracle_scoped_true(self):
        node = _make_node("component", "x", evidence_paths=["Gemfile"])
        self.assertTrue(is_oracle_scoped(node, {"Gemfile"}))

    def test_is_oracle_scoped_false(self):
        node = _make_node("component", "x", evidence_paths=["go.sum"])
        self.assertFalse(is_oracle_scoped(node, {"Gemfile"}))


class TestScoreComponents(unittest.TestCase):
    def _entry(self, components, oracle_files):
        return {"components": components,
                "oracle_files": [{"path": p, "sha256": ""} for p in oracle_files],
                "deployables": [], "interfaces": [], "capabilities": [], "edges": [],
                "coverage": {}}

    def _map(self, nodes):
        return {"nodes": nodes, "edges": [], "coverage": []}

    def test_tp_fp_fn(self):
        # 2 labels; 1 match, 1 miss; 1 extra dircue node
        entry = self._entry(
            [{"name": "@mastodon/streaming", "root": "streaming", "ecosystem": "npm"},
             {"name": "mastodon", "root": ".", "ecosystem": "bundler"}],
            ["streaming/package.json", "Gemfile"]
        )
        nodes = [
            _make_node("component", "@mastodon/streaming", root="streaming",
                       ecosystem="npm", evidence_paths=["streaming/package.json"]),
            _make_node("component", "extra", root=".", ecosystem="npm",
                       evidence_paths=["streaming/package.json"]),
        ]
        r = score_components(entry, self._map(nodes), {"streaming/package.json", "Gemfile"})
        self.assertEqual(r.tp, 1)
        self.assertEqual(r.fn, 1)  # mastodon not found (no oracle-scoped match)
        self.assertEqual(r.fp, 1)  # "extra" not in labels


class TestScoreDeployables(unittest.TestCase):
    def _entry(self, deployables, oracle_files):
        return {"deployables": deployables,
                "oracle_files": [{"path": p, "sha256": ""} for p in oracle_files],
                "components": [], "interfaces": [], "capabilities": [], "edges": [],
                "coverage": {}}

    def _map(self, nodes):
        return {"nodes": nodes, "edges": [], "coverage": []}

    def test_compose_service_match(self):
        entry = self._entry(
            [{"kind": "service", "name": "web", "path": "docker-compose.yml"},
             {"kind": "service", "name": "db", "path": "docker-compose.yml"}],
            ["docker-compose.yml"]
        )
        nodes = [
            _make_node("deployable", "web", dkind="service",
                       evidence_paths=["docker-compose.yml"]),
            _make_node("deployable", "db", dkind="service",
                       evidence_paths=["docker-compose.yml"]),
        ]
        r = score_deployables(entry, self._map(nodes), {"docker-compose.yml"})
        self.assertEqual(r.tp, 2)
        self.assertEqual(r.fn, 0)
        self.assertEqual(r.fp, 0)


class TestScoreInterfaces(unittest.TestCase):
    def _entry(self, interfaces, oracle_files):
        return {"interfaces": interfaces,
                "oracle_files": [{"path": p, "sha256": ""} for p in oracle_files],
                "components": [], "deployables": [], "capabilities": [], "edges": [],
                "coverage": {}}

    def _map(self, nodes):
        return {"nodes": nodes, "edges": [], "coverage": []}

    def test_port_match(self):
        entry = self._entry(
            [{"kind": "declared_port", "name": "3000", "path": "Dockerfile"}],
            ["Dockerfile"]
        )
        nodes = [
            _make_node("interface", "port:3000", ikind="declared_port",
                       evidence_paths=["Dockerfile"]),
        ]
        r = score_interfaces(entry, self._map(nodes), {"Dockerfile"})
        self.assertEqual(r.tp, 1)
        self.assertEqual(r.fn, 0)
        self.assertEqual(r.fp, 0)


class TestScoreCapabilities(unittest.TestCase):
    def _entry(self, capabilities, oracle_files):
        return {"capabilities": capabilities,
                "oracle_files": [{"path": p, "sha256": ""} for p in oracle_files],
                "components": [], "deployables": [], "interfaces": [], "edges": [],
                "coverage": {}}

    def _map(self, nodes, edges=None):
        return {"nodes": nodes, "edges": edges or [], "coverage": []}

    def test_capability_with_owner_via_edge(self):
        cap_node = {"id": "cap:1", "kind": "capability", "name": "cache:redis",
                    "properties": {}, "evidence": [{"path": "Gemfile"}]}
        comp_node = {"id": "comp:1", "kind": "component", "name": "mastodon",
                     "properties": {}, "evidence": [{"path": "Gemfile"}]}
        edge = _make_edge("uses_capability", "comp:1", "cap:1", evidence_paths=["Gemfile"])
        entry = self._entry(
            [{"capability": "cache:redis", "owner": "mastodon", "path": "Gemfile"}],
            ["Gemfile"]
        )
        map_doc = {"nodes": [cap_node, comp_node], "edges": [edge], "coverage": []}
        r = score_capabilities(entry, map_doc, {"Gemfile"})
        self.assertEqual(r.tp, 1)
        self.assertEqual(r.fn, 0)
        self.assertEqual(r.fp, 0)

    def test_capability_wrong_owner(self):
        cap_node = {"id": "cap:1", "kind": "capability", "name": "cache:redis",
                    "properties": {}, "evidence": [{"path": "Gemfile"}]}
        comp_node = {"id": "comp:2", "kind": "component", "name": "other",
                     "properties": {}, "evidence": [{"path": "Gemfile"}]}
        edge = _make_edge("uses_capability", "comp:2", "cap:1", evidence_paths=["Gemfile"])
        entry = self._entry(
            [{"capability": "cache:redis", "owner": "mastodon", "path": "Gemfile"}],
            ["Gemfile"]
        )
        map_doc = {"nodes": [cap_node, comp_node], "edges": [edge], "coverage": []}
        r = score_capabilities(entry, map_doc, {"Gemfile"})
        self.assertEqual(r.fn, 1)  # wrong owner


if __name__ == "__main__":
    unittest.main()


class TestEndpointNormalisationBoundaries(unittest.TestCase):
    def test_root_dockerfile_edge_endpoint_uses_path_matched_label(self):
        docker = {
            "id": "docker", "kind": "deployable", "name": "(root)",
            "paths": ["Dockerfile"], "evidence": [{"path": "Dockerfile"}],
            "properties": {"kind": "container_build"},
        }
        component = {
            "id": "component", "kind": "component", "name": "api",
            "properties": {"root": ".", "ecosystem": "npm"},
            "evidence": [{"path": "package.json"}],
        }
        edge = _make_edge("builds", "docker", "component", evidence_paths=["Dockerfile"])
        label_entry = {
            "oracle_files": [{"path": "Dockerfile"}],
            "deployables": [{"kind": "container_build", "name": "api", "path": "Dockerfile"}],
            "edges": [{"type": "builds", "from": "api", "to": "api"}],
        }
        result = score_edges(label_entry, {"nodes": [docker, component], "edges": [edge]}, {"Dockerfile"})
        self.assertEqual((result.tp, result.fp, result.fn), (1, 0, 0))

    def test_root_dockerfile_alias_does_not_match_same_named_compose_service(self):
        docker = {
            "id": "docker", "kind": "deployable", "name": "(root)",
            "paths": ["Dockerfile"], "evidence": [{"path": "Dockerfile"}],
            "properties": {"kind": "container_build"},
        }
        compose = {
            "id": "compose", "kind": "deployable", "name": "api",
            "paths": ["compose.yml"], "evidence": [{"path": "compose.yml"}],
            "properties": {"kind": "service"},
        }
        component = {"id": "component", "kind": "component", "name": "api", "properties": {}, "evidence": []}
        wrong = _make_edge("builds", "compose", "component", evidence_paths=["Dockerfile"])
        label_entry = {
            "oracle_files": [{"path": "Dockerfile"}],
            "deployables": [{"kind": "container_build", "name": "api", "path": "Dockerfile"}],
            "edges": [{"type": "builds", "from": "api", "to": "api"}],
        }
        result = score_edges(label_entry, {"nodes": [docker, compose, component], "edges": [wrong]}, {"Dockerfile"})
        self.assertEqual((result.tp, result.fp, result.fn), (0, 1, 1))

    def test_root_dockerfile_endpoint_alias_requires_unique_path_match(self):
        docker = {
            "id": "docker", "kind": "deployable", "name": "(root)",
            "paths": ["Dockerfile"], "evidence": [{"path": "Dockerfile"}],
            "properties": {"kind": "container_build"},
        }
        other = {
            "id": "other", "kind": "deployable", "name": "(root)",
            "paths": ["other/Dockerfile"], "evidence": [{"path": "other/Dockerfile"}],
            "properties": {"kind": "container_build"},
        }
        component = {"id": "c", "kind": "component", "name": "api", "properties": {}, "evidence": []}
        edge = _make_edge("builds", "other", "c", evidence_paths=["Dockerfile"])
        label_entry = {
            "oracle_files": [{"path": "Dockerfile"}],
            "deployables": [
                {"kind": "container_build", "name": "api", "path": "Dockerfile"},
                {"kind": "container_build", "name": "api", "path": "other/Dockerfile"},
            ],
            "edges": [{"type": "builds", "from": "api", "to": "api"}],
        }
        result = score_edges(label_entry, {"nodes": [docker, other, component], "edges": [edge]}, {"Dockerfile"})
        self.assertEqual((result.tp, result.fp, result.fn), (0, 1, 1))

    def test_npm_scoped_name_is_not_a_go_module_path(self):
        node = {"kind": "component", "name": "@mastodon/mastodon", "properties": {"ecosystem": "npm"}}
        self.assertFalse(node_matches_endpoint(node, "Mastodon"))

    def test_compose_service_requires_its_file(self):
        dockerfile = {"kind": "deployable", "name": "superset-websocket", "paths": ["superset-websocket/Dockerfile"], "properties": {}}
        service = {"kind": "deployable", "name": "superset-websocket", "paths": ["docker-compose.yml"], "properties": {}}
        self.assertFalse(node_matches_endpoint(dockerfile, "docker-compose.yml:superset-websocket"))
        self.assertTrue(node_matches_endpoint(service, "docker-compose.yml:superset-websocket"))

    def test_contains_label_matches_reversed_member_of(self):
        nodes = {"a": {"id": "a", "kind": "component", "name": "root", "properties": {}},
                 "b": {"id": "b", "kind": "component", "name": "member", "properties": {}}}
        edge = {"type": "member_of", "from": "b", "to": "a"}
        self.assertTrue(match_edge({"type": "contains", "from": "root", "to": "member"}, edge, nodes))
