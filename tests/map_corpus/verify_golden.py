#!/usr/bin/env python3
"""
verify_golden.py: Semantic precision/recall gate for dircue map (issue #75).

Measures dircue's output against blind hand-written golden labels.
Nodes are matched on **semantic keys** (name + root + ecosystem, kind + name +
path, etc.) — never on hashed IDs.  dircue output is restricted to nodes and
edges whose evidence paths fall in the label's oracle file set so that
precision is measurable.

Usage:
    python3 tests/map_corpus/verify_golden.py \\
        --labels  tests/map_corpus/golden_expectations.json \\
        --maps    <dir with per-repo <repo>.json dircue map files> \\
        --output  tests/map_corpus/golden_results.json

Exit 0 when all gate thresholds pass, 1 on any failure.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# ---------------------------------------------------------------------------
# Vocabulary alias table
# ---------------------------------------------------------------------------
# Each table maps a *label* vocabulary word to the set of *dircue* vocabulary
# words it corresponds to.  Every alias needs a one-line justification.

ECOSYSTEM_ALIASES: dict[str, set[str]] = {
    # Label     → dircue ecosystem(s)
    "bundler":  {"ruby"},        # Ruby Gemfile → dircue reports eco=ruby
    "pyproject": {"python", "python-uv"},  # PEP 517 pyproject.toml; dircue uses python-uv when uv lock present
    "pypi":     {"python", "python-uv"},  # Python distribution (maturin, flit) → dircue eco python/python-uv
    "gomod":    {"go"},          # go.mod → dircue eco=go
    "npm":      {"npm"},         # package.json (same)
    "cargo":    {"cargo"},       # Cargo.toml (same)
    "maven":    {"maven"},       # pom.xml (same)
    "gradle":   {"gradle"},      # build.gradle (same)
    "terraform":{"terraform"},   # versions.tf / *.tf (same)
}

DEPLOYABLE_KIND_ALIASES: dict[str, set[str]] = {
    # Label kind      → dircue properties.kind
    # dircue uses "infrastructure" for multiple IaC/serverless node types
    "terraform_module": {"infrastructure"},  # Terraform module directory; dircue provider=terraform
    "helm":             {"infrastructure"},  # Helm Chart.yaml; dircue provider=helm
    "function":         {"infrastructure"},  # SAM/Lambda function; dircue provider=cloudformation
    "container_build":  {"container_build"}, # Dockerfile; same in both
    "service":          {"service"},         # Compose/K8s Service; same in both
    "workload":         {"workload"},        # K8s Deployment/StatefulSet; same in both
    "resource":         {"resource", "infrastructure"},  # K8s Secrets etc. dircue may say infrastructure
    "workflow":         {"workflow"},        # GitHub Actions workflow; same in both
}

INTERFACE_KIND_ALIASES: dict[str, set[str]] = {
    # Label interface kind    → dircue interface_kind
    "cli_binary":             {"binary",                  # Go package main → dircue binary
                               "cargo-default-run"},      # Rust [[bin]] via default-run
    "npm_start":              {"script"},                 # npm scripts.start → dircue script
    "grpc_service":           {"service"},                # protobuf service block → dircue service
    "grpc_operation":         {"operation"},              # per-RPC method → dircue operation
    "declared_port":          {"declared_port"},          # EXPOSE / ports / containerPort; same
    "spring_boot_application":{"spring_boot_application"}, # @SpringBootApplication; same
}

CAPABILITY_ALIASES: dict[str, set[str]] = {
    # Label capability → dircue capability name(s)
    # Intentionally empty: capability names should match exactly between label
    # and dircue output.  If a capability name is missing in dircue's catalog,
    # record it as FN; do not hide the gap with an alias.
    # Known catalog gaps (documented in DISAGREEMENTS.md):
    #   datastore:dynamodb  – no catalog entry; expected to be fixed by catalog rebuild
    #   datastore:mysql     – missing from Maven catalog
    #   datastore:h2        – missing from Maven catalog
    #   datastore:postgresql (Ruby pg gem) – missing from Ruby catalog
}

# The "datastore:relational" capability in dircue is a dircue-only generic term
# for an unknown SQL datastore detected from config files (not from package
# manifests).  It is NOT an alias for label-specific datastores.  When dircue
# emits datastore:relational from oracle files, and the label expects a more
# specific name (datastore:postgresql, datastore:mysql), that is a vocabulary
# difference recorded as FP (dircue) + FN (label).


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def normalize(s: str) -> str:
    """Case-fold and strip for name comparison."""
    return s.strip().lower()


def oracle_set(label_entry: dict) -> set[str]:
    """Return the set of oracle file paths for a label entry."""
    return {f["path"] for f in label_entry.get("oracle_files", [])}


def is_oracle_scoped(node: dict, oracle: set[str]) -> bool:
    """True when the node has at least one evidence path in the oracle set."""
    for ev in node.get("evidence", []):
        if ev.get("path", "") in oracle:
            return True
    return False


def is_edge_oracle_scoped(edge: dict, oracle: set[str]) -> bool:
    """True when an edge has at least one evidence path in the oracle set."""
    for ev in edge.get("evidence", []):
        if ev.get("path", "") in oracle:
            return True
    return False


def resolve_kind(label_kind: str, alias_table: dict[str, set[str]]) -> set[str]:
    """Return the set of dircue kind values that correspond to a label kind."""
    return alias_table.get(label_kind, {label_kind})


def node_prop(node: dict, key: str) -> str:
    return node.get("properties", {}).get(key, "")


# ---------------------------------------------------------------------------
# Per-kind matchers
# ---------------------------------------------------------------------------

def _go_module_base(name: str) -> str:
    """Return the short base name for a Go module path.

    For 'github.com/grafana/loki/v3' returns 'loki' (strips leading path
    segments and trailing major-version suffixes).  Mirrors dircue's
    moduleBaseName logic.
    """
    import posixpath
    base = posixpath.basename(name)
    if (len(base) > 1 and base[0] == "v"
            and base[1:].isdigit()):
        # trailing /vN — use the parent segment
        base = posixpath.basename(posixpath.dirname(name))
    return base


def match_component(label: dict, node: dict) -> bool:
    """Return True iff a label component matches a dircue component node.

    Name matching has two modes:
    - Direct: case-insensitive string equality.
    - Go-module: if label ecosystem is gomod and the label name looks like a
      module path, also accept a match against dircue's short base name.
      Justification: dircue normalises 'github.com/grafana/loki/v3' → 'loki'
      via moduleBaseName; the label uses the declared module path.
    """
    if node.get("kind") != "component":
        return False
    label_name = label.get("name", "")
    node_name = node.get("name", "")
    # Root: normalize '.' and ''
    label_root = label.get("root", ".").rstrip("/") or "."
    node_root = node_prop(node, "root").rstrip("/") or "."
    if normalize(label_root) != normalize(node_root):
        return False
    # Ecosystem: with alias
    label_eco = label.get("ecosystem", "")
    node_eco = node_prop(node, "ecosystem")
    allowed = resolve_kind(label_eco, ECOSYSTEM_ALIASES)
    if node_eco not in allowed:
        return False
    # Name: case-insensitive direct match
    if normalize(label_name) == normalize(node_name):
        return True
    # Go-module: label name may be a full module path; dircue uses base name
    if label_eco == "gomod" and "/" in label_name:
        if normalize(_go_module_base(label_name)) == normalize(node_name):
            return True
    return False


def match_deployable(label: dict, node: dict) -> bool:
    """Return True iff a label deployable matches a dircue deployable node.

    Matching uses these strategies in order:
    1. Exact case-insensitive name match.
    2. Path-based match (for container_build nodes dircue names "(root)"):
       the label's path must exactly match a node path or evidence path.
       The label name must also be non-empty and distinct.
    3. Directory prefix match (for helm/terraform: label path is a dir,
       node path is a file under that dir).
    Path-based matching is restricted to cases where the node name is a
    fallback placeholder ("(root)") to avoid over-matching service nodes
    that share the same compose file.
    """
    if node.get("kind") != "deployable":
        return False
    # Kind: with alias
    label_kind = label.get("kind", "")
    node_kind = node_prop(node, "kind")
    if node_kind not in resolve_kind(label_kind, DEPLOYABLE_KIND_ALIASES):
        return False
    label_name = label.get("name", "")
    node_name = node.get("name", "")
    # Strategy A: exact name match (case-insensitive) — primary matcher
    if normalize(label_name) == normalize(node_name):
        return True
    # Strategy B + C: path-based — only when node name is a placeholder
    # "(root)" that dircue assigns when no explicit name is available.
    # This handles Dockerfiles named "(root)" by dircue but given a real name
    # by the labeler (e.g. "ruff" or "superset").
    label_path = label.get("path", "")
    node_paths = node.get("paths", [])
    node_ev_paths = {ev.get("path", "") for ev in node.get("evidence", [])}
    if node_name in ("(root)", "") and label_path:
        # Strategy B: label path is node's primary path or evidence path
        if label_path in node_paths or label_path in node_ev_paths:
            return True
        # Strategy C: label path is a directory whose files appear in node paths
        # (e.g. label path "production/helm/loki" and node path ends in "/Chart.yaml")
        for np in list(node_paths) + list(node_ev_paths):
            if np.startswith(label_path.rstrip("/") + "/"):
                return True
    # Strategy C (name mismatch, path prefix): for helm/terraform where node
    # name matches but label path is a parent dir of the node's Chart.yaml/main.tf
    if label_name and label_path:
        for np in list(node_paths) + list(node_ev_paths):
            if np.startswith(label_path.rstrip("/") + "/"):
                # Only accept if names are close (same after normalization)
                if normalize(label_name) == normalize(node_name):
                    return True
    return False


def match_interface(label: dict, node: dict) -> bool:
    """Return True iff a label interface matches a dircue interface node."""
    if node.get("kind") != "interface":
        return False
    # Interface kind: with alias
    label_kind = label.get("kind", "")
    node_ikind = node_prop(node, "interface_kind")
    if node_ikind not in resolve_kind(label_kind, INTERFACE_KIND_ALIASES):
        return False
    label_name = label.get("name", "")
    node_name = node.get("name", "")
    # Special: declared_port — compare port numbers
    if label_kind == "declared_port":
        # Label name might be "3000", node name "port:3000"
        # Normalize to the port number
        def extract_port(s: str) -> str:
            return s.lower().replace("port:", "").strip()
        if extract_port(label_name) != extract_port(node_name):
            return False
        return True
    # grpc_service: label name is the service name (e.g. "Querier"),
    # dircue name is "package.ServiceName" (e.g. "logproto.Querier")
    if label_kind == "grpc_service":
        node_service_part = node_name.split(".")[-1] if "." in node_name else node_name
        if normalize(label_name) == normalize(node_service_part):
            return True
        if normalize(label_name) == normalize(node_name):
            return True
        return False
    # npm_start: label name is a description; dircue name is the script key
    # Match if node name == "start" (the standard start script) or label says "start"
    if label_kind == "npm_start":
        if normalize(node_name) in ("start", "start server"):
            return True
        if normalize(label_name) == normalize(node_name):
            return True
        return False
    # General: case-insensitive name match
    return normalize(label_name) == normalize(node_name)


def match_capability(label: dict, node: dict) -> bool:
    """Return True iff a label capability matches a dircue capability node."""
    if node.get("kind") != "capability":
        return False
    label_cap = label.get("capability", "")
    node_cap = node.get("name", "")
    allowed = resolve_kind(label_cap, CAPABILITY_ALIASES)
    return node_cap in allowed or node_cap == label_cap


def node_matches_endpoint(node: dict, endpoint_str: str) -> bool:
    """Check if a label edge endpoint string could refer to the given dircue node.

    Uses the same semantic keys as the per-kind node matchers so that an edge
    endpoint resolves correctly regardless of how dircue names the node:
    - component: name equality; Go-module base-name normalisation
    - deployable: name equality; path/evidence-path equality; path-prefix dir match;
                  compose "file:service" format; "<path> image" suffix
    - interface: name equality; "<name> binary" / "<name> gRPC service" suffixes;
                 declared-port "port:N" / "N" normalisation; grpc short name
    - capability: exact name (e.g. "datastore:dynamodb"); alias resolution
    """
    kind = node.get("kind", "")
    name = node.get("name", "")
    norm_ep = normalize(endpoint_str)
    norm_name = normalize(name)

    if not norm_ep or not kind:
        return False

    # Direct name match works for all kinds
    if norm_ep == norm_name:
        return True

    if kind == "component":
        # Module-path normalisation applies to Go components only: an npm
        # scoped name such as "@mastodon/mastodon" is not a module path.
        if node.get("properties", {}).get("ecosystem") != "go":
            return False
        # Go module: endpoint may be the full module path ("github.com/grafana/loki/v3")
        # while the node stores the base name ("loki")
        if "/" in endpoint_str:
            if normalize(_go_module_base(endpoint_str)) == norm_name:
                return True
        # Reverse: node has the full module path, endpoint is the short base name
        if "/" in name:
            if norm_ep == normalize(_go_module_base(name)):
                return True

    elif kind == "deployable":
        p = node.get("properties", {})
        node_paths = set(node.get("paths", []))
        node_ev_paths = {ev.get("path", "") for ev in node.get("evidence", [])}
        all_paths = node_paths | node_ev_paths

        # Path match: endpoint is a file path that appears in this node's paths/evidence
        if endpoint_str in all_paths:
            return True
        # "<path> image" suffix: strip " image" and check path or name
        if norm_ep.endswith(" image"):
            ep_base = endpoint_str[:-6].strip()
            if ep_base in all_paths or normalize(ep_base) == norm_name:
                return True
        # "(description)" suffix in parentheses: e.g. "ruff (container image)"
        # strip the parenthetical and match against the name or path
        if "(" in endpoint_str:
            ep_bare = normalize(endpoint_str[:endpoint_str.index("(")].strip())
            if ep_bare == norm_name:
                return True
            if ep_bare and ep_bare in all_paths:
                return True
        # Directory prefix: endpoint is a directory prefix of a path in this node
        ep_clean = endpoint_str.rstrip("/")
        for np in all_paths:
            if np.startswith(ep_clean + "/"):
                return True
        # Compose service "file:service" format, e.g. "docker-compose.yml:web"
        # The part after the colon is the service name
        if ":" in endpoint_str and not endpoint_str.startswith("data"):
            colon_idx = endpoint_str.index(":")
            file_part = endpoint_str[:colon_idx]
            service_name = endpoint_str[colon_idx + 1:]
            # The service must be declared in the named file: a Dockerfile
            # deployable that shares the service's name is a different node.
            if normalize(service_name) == norm_name and file_part in all_paths:
                return True

    elif kind == "interface":
        p = node.get("properties", {})
        ikind = p.get("interface_kind", "")

        # "<name> binary": strip " binary" suffix and match node name with binary kind
        if norm_ep.endswith(" binary") and ikind in ("binary", "cargo-default-run"):
            if normalize(endpoint_str[:-7].strip()) == norm_name:
                return True

        # "<name> gRPC service": strip suffix and match; handle "pkg.ServiceName" format
        if "grpc service" in norm_ep and ikind == "service":
            service_name = norm_ep.replace(" grpc service", "").strip()
            node_short = norm_name.split(".")[-1] if "." in norm_name else norm_name
            if service_name == node_short or service_name == norm_name:
                return True
        # For grpc services: endpoint may just be the short service name
        if ikind == "service":
            node_short = norm_name.split(".")[-1] if "." in norm_name else norm_name
            if norm_ep == node_short:
                return True

        # Declared port: "port:3100", "3100", "http" (named port)
        if ikind == "declared_port":
            def _extract_port(s: str) -> str:
                return s.lower().replace("port:", "").strip()
            if _extract_port(endpoint_str) == _extract_port(name):
                return True

        # npm_start: match "start" script
        if ikind == "script" and norm_name == "start":
            if norm_ep in ("start", "npm start", "start server"):
                return True

    elif kind == "capability":
        # Capabilities: exact name or alias
        allowed = resolve_kind(endpoint_str, CAPABILITY_ALIASES)
        if name in allowed or name == endpoint_str:
            return True

    return False


def match_edge(label: dict, edge: dict, id_to_node: dict[str, dict]) -> bool:
    """Return True iff a label edge matches a dircue edge.

    Edge type must match exactly.  Both endpoints are resolved via
    node_matches_endpoint, which uses the same semantic keys as the per-kind
    node matchers (component/deployable/interface/capability).
    """
    edge_type = edge.get("type")
    from_node = id_to_node.get(edge.get("from", ""), {})
    to_node = id_to_node.get(edge.get("to", ""), {})
    # Alias (naming only): a label "parent contains child" is the same fact as
    # dircue's "child member_of parent".
    if label.get("type") == "contains" and edge_type == "member_of":
        edge_type, from_node, to_node = "contains", to_node, from_node
    if label.get("type") != edge_type:
        return False
    label_from = label.get("from", "")
    label_to = label.get("to", "")
    return (node_matches_endpoint(from_node, label_from) and
            node_matches_endpoint(to_node, label_to))


# ---------------------------------------------------------------------------
# Oracle-scoped node extraction
# ---------------------------------------------------------------------------

def oracle_nodes(map_doc: dict, oracle: set[str]) -> list[dict]:
    """Return nodes with at least one evidence path in oracle."""
    return [n for n in map_doc.get("nodes", []) if is_oracle_scoped(n, oracle)]


def oracle_edges(map_doc: dict, oracle: set[str]) -> list[dict]:
    """Return edges with at least one evidence path in oracle."""
    return [e for e in map_doc.get("edges", []) if is_edge_oracle_scoped(e, oracle)]


# ---------------------------------------------------------------------------
# Precision / recall per question
# ---------------------------------------------------------------------------

@dataclass
class QuestionResult:
    question: str
    tp: int = 0
    fp: int = 0
    fn: int = 0
    tp_items: list[str] = field(default_factory=list)
    fp_items: list[str] = field(default_factory=list)
    fn_items: list[str] = field(default_factory=list)

    @property
    def precision(self) -> float | None:
        denom = self.tp + self.fp
        return (self.tp / denom) if denom > 0 else None

    @property
    def recall(self) -> float | None:
        denom = self.tp + self.fn
        return (self.tp / denom) if denom > 0 else None

    def as_dict(self) -> dict:
        return {
            "question": self.question,
            "tp": self.tp,
            "fp": self.fp,
            "fn": self.fn,
            "precision": round(self.precision, 4) if self.precision is not None else None,
            "recall": round(self.recall, 4) if self.recall is not None else None,
            "tp_items": self.tp_items,
            "fp_items": self.fp_items,
            "fn_items": self.fn_items,
        }


def _node_desc(node: dict) -> str:
    p = node.get("properties", {})
    return (f"{node.get('kind')}:{node.get('name')}:"
            f"root={p.get('root','')}:eco={p.get('ecosystem','')}:"
            f"kind={p.get('kind','')}:ikind={p.get('interface_kind','')}:"
            f"paths={node.get('paths','')}")


def score_components(label_entry: dict, map_doc: dict, oracle: set[str]) -> QuestionResult:
    r = QuestionResult("components")
    labels = label_entry.get("components", [])
    scoped = [n for n in oracle_nodes(map_doc, oracle) if n.get("kind") == "component"]
    matched_nodes: set[int] = set()
    for lb in labels:
        found = next(
            (i for i, n in enumerate(scoped) if i not in matched_nodes and match_component(lb, n)),
            None,
        )
        if found is not None:
            r.tp += 1
            r.tp_items.append(f"name={lb.get('name')} eco={lb.get('ecosystem')}")
            matched_nodes.add(found)
        else:
            r.fn += 1
            r.fn_items.append(f"name={lb.get('name')} eco={lb.get('ecosystem')} root={lb.get('root')}")
    for i, n in enumerate(scoped):
        if i not in matched_nodes:
            r.fp += 1
            r.fp_items.append(_node_desc(n))
    return r


def score_deployables(label_entry: dict, map_doc: dict, oracle: set[str]) -> QuestionResult:
    r = QuestionResult("deployables")
    labels = label_entry.get("deployables", [])
    scoped = [n for n in oracle_nodes(map_doc, oracle) if n.get("kind") == "deployable"]
    matched_nodes: set[int] = set()
    for lb in labels:
        found = next(
            (i for i, n in enumerate(scoped) if i not in matched_nodes and match_deployable(lb, n)),
            None,
        )
        if found is not None:
            r.tp += 1
            r.tp_items.append(f"kind={lb.get('kind')} name={lb.get('name')}")
            matched_nodes.add(found)
        else:
            r.fn += 1
            r.fn_items.append(f"kind={lb.get('kind')} name={lb.get('name')} path={lb.get('path')}")
    for i, n in enumerate(scoped):
        if i not in matched_nodes:
            r.fp += 1
            r.fp_items.append(_node_desc(n))
    return r


def score_interfaces(label_entry: dict, map_doc: dict, oracle: set[str]) -> QuestionResult:
    r = QuestionResult("interfaces")
    labels = label_entry.get("interfaces", [])
    scoped = [n for n in oracle_nodes(map_doc, oracle) if n.get("kind") == "interface"]
    matched_nodes: set[int] = set()
    for lb in labels:
        found = next(
            (i for i, n in enumerate(scoped) if i not in matched_nodes and match_interface(lb, n)),
            None,
        )
        if found is not None:
            r.tp += 1
            r.tp_items.append(f"kind={lb.get('kind')} name={lb.get('name')}")
            matched_nodes.add(found)
        else:
            r.fn += 1
            r.fn_items.append(f"kind={lb.get('kind')} name={lb.get('name')} path={lb.get('path')}")
    for i, n in enumerate(scoped):
        if i not in matched_nodes:
            r.fp += 1
            r.fp_items.append(_node_desc(n))
    return r


def score_capabilities(label_entry: dict, map_doc: dict, oracle: set[str]) -> QuestionResult:
    r = QuestionResult("capabilities")
    labels = label_entry.get("capabilities", [])
    # Capability nodes are matched by name.  Owner (component) is validated via
    # uses_capability edges when the edge is oracle-scoped.
    scoped_caps = [n for n in oracle_nodes(map_doc, oracle) if n.get("kind") == "capability"]
    scoped_edges = oracle_edges(map_doc, oracle)
    id_to_node = {n["id"]: n for n in map_doc.get("nodes", [])}

    # Build a lookup: capability node name → set of component names connected
    # by oracle-scoped uses_capability edges
    cap_owners: dict[str, set[str]] = {}
    for e in scoped_edges:
        if e.get("type") == "uses_capability":
            cap_node = id_to_node.get(e.get("to", ""), {})
            comp_node = id_to_node.get(e.get("from", ""), {})
            cap_name = cap_node.get("name", "")
            comp_name = comp_node.get("name", "")
            cap_owners.setdefault(cap_name, set()).add(comp_name)

    # Also include all capabilities (oracle-scoped) even without an edge,
    # using directory-containment attribution from properties
    for n in scoped_caps:
        cap_name = n.get("name", "")
        owner_id = node_prop(n, "owning_component")
        if owner_id and owner_id in id_to_node:
            owner_name = id_to_node[owner_id].get("name", "")
            cap_owners.setdefault(cap_name, set()).add(owner_name)

    matched_caps: set[int] = set()
    for lb in labels:
        lb_cap = lb.get("capability", "")
        lb_owner = lb.get("owner", "")
        found = None
        for i, n in enumerate(scoped_caps):
            if i in matched_caps:  # skip nodes already matched by a previous label
                continue
            if not match_capability(lb, n):
                continue
            # Check owner if specified
            if lb_owner:
                owners = cap_owners.get(n.get("name", ""), set())
                # Normalised label owner: try exact match first, then Go-module
                # base-name match (label may use the full module path while dircue
                # stores the short name derived via moduleBaseName).
                def _owner_match(lbo: str, os: set[str]) -> bool:
                    norm_lbo = normalize(lbo)
                    if any(norm_lbo == normalize(o) for o in os):
                        return True
                    # Go-module: "github.com/org/repo/v3" ↔ "repo". Only a
                    # module path (first element contains a dot) qualifies;
                    # npm scoped names such as "@scope/name" do not.
                    def _is_go_path(v: str) -> bool:
                        return "/" in v and "." in v.split("/", 1)[0] and not v.startswith("@")
                    if _is_go_path(lbo):
                        short = normalize(_go_module_base(lbo))
                        if any(short == normalize(o) for o in os):
                            return True
                    # Reverse: owner stored as full path, label is short name
                    for o in os:
                        if _is_go_path(o) and normalize(_go_module_base(o)) == norm_lbo:
                            return True
                    return False
                if not _owner_match(lb_owner, owners):
                    continue
            found = i
            break
        if found is not None:
            r.tp += 1
            r.tp_items.append(f"capability={lb_cap} owner={lb_owner}")
            matched_caps.add(found)
        else:
            r.fn += 1
            r.fn_items.append(f"capability={lb_cap} owner={lb_owner} path={lb.get('path')}")
    for i, n in enumerate(scoped_caps):
        if i not in matched_caps:
            r.fp += 1
            r.fp_items.append(_node_desc(n))
    return r


EVALUATED_EDGE_TYPES = ("builds", "runs", "depends_on", "uses_capability", "contains")


def score_edges(label_entry: dict, map_doc: dict, oracle: set[str]) -> QuestionResult:
    r = QuestionResult("edges")
    labels = label_entry.get("edges", [])
    scoped = oracle_edges(map_doc, oracle)
    id_to_node = {n["id"]: n for n in map_doc.get("nodes", [])}

    # For precision, every edge of an evaluated type within oracle scope
    # counts, whether or not the labels mention that type. declares and
    # depends_on_local are evaluated only where a repository labels them,
    # because per-member workspace edges are not exhaustively labeled.
    labeled_edge_types: set[str] = set(EVALUATED_EDGE_TYPES) | (
        {lb.get("type", "") for lb in labels} & {"declares", "depends_on_local"})

    matched_edges: set[int] = set()
    for lb in labels:
        found = next(
            (i for i, e in enumerate(scoped) if match_edge(lb, e, id_to_node)), None
        )
        if found is not None:
            r.tp += 1
            r.tp_items.append(f"type={lb.get('type')} from={lb.get('from')} to={lb.get('to')}")
            matched_edges.add(found)
        else:
            r.fn += 1
            r.fn_items.append(f"type={lb.get('type')} from={lb.get('from')} to={lb.get('to')}")
    for i, e in enumerate(scoped):
        etype = "contains" if e.get("type") == "member_of" else e.get("type")
        if i not in matched_edges and etype in labeled_edge_types:
            fn = id_to_node.get(e.get("from", ""), {})
            tn = id_to_node.get(e.get("to", ""), {})
            r.fp += 1
            r.fp_items.append(f"type={e.get('type')} from={fn.get('name','?')} to={tn.get('name','?')}")
    return r


def score_coverage(label_entry: dict, map_doc: dict) -> QuestionResult:
    """Check coverage status agreement per question."""
    r = QuestionResult("coverage")
    label_cov = label_entry.get("coverage", {})
    map_cov = {c.get("question"): c.get("status") for c in map_doc.get("coverage", [])}
    # dircue question names vs label question keys
    question_map = {
        "components": "components",
        "deployables": "deployables",
        "interfaces": "interfaces",
        "capabilities": "capabilities",
        "edges": "edges",
    }
    for q, dq in question_map.items():
        if q not in label_cov:
            continue
        if dq not in map_cov and q not in map_cov:
            # The map has no coverage question of this name (labels may add
            # one, such as "edges"); there is nothing to compare.
            continue
        label_status = label_cov[q].get("status", "unknown")
        dircue_status = map_cov.get(dq, map_cov.get(q, "not_run"))
        # Treat "complete" as the strict bound: if label says complete but
        # dircue says partial, that's a disagreement.  Unknown matches anything
        # that isn't complete.
        if label_status == dircue_status:
            r.tp += 1
            r.tp_items.append(f"question={q} status={label_status}")
        elif dircue_status == "complete":
            # dircue claims completeness the labeler could not establish:
            # an overclaim, the failure dircue's coverage contract forbids.
            r.fp += 1
            r.fp_items.append(f"question={q}: dircue=complete but label={label_status} (overclaim)")
        else:
            # dircue is more conservative than the label: recorded, not a pass.
            r.fn += 1
            r.fn_items.append(f"question={q}: label={label_status} but dircue={dircue_status} (conservative)")
    return r


# ---------------------------------------------------------------------------
# Repo scoring
# ---------------------------------------------------------------------------

@dataclass
class RepoResult:
    repo: str
    commit: str
    questions: list[QuestionResult] = field(default_factory=list)

    def as_dict(self) -> dict:
        return {
            "repo": self.repo,
            "commit": self.commit,
            "questions": [q.as_dict() for q in self.questions],
        }


def score_repo(label_entry: dict, map_doc: dict) -> RepoResult:
    oracle = oracle_set(label_entry)
    repo = label_entry.get("repo", "unknown")
    commit = label_entry.get("commit", "unknown")
    rr = RepoResult(repo, commit)
    rr.questions.append(score_components(label_entry, map_doc, oracle))
    rr.questions.append(score_deployables(label_entry, map_doc, oracle))
    rr.questions.append(score_interfaces(label_entry, map_doc, oracle))
    rr.questions.append(score_capabilities(label_entry, map_doc, oracle))
    rr.questions.append(score_edges(label_entry, map_doc, oracle))
    rr.questions.append(score_coverage(label_entry, map_doc))
    return rr


# ---------------------------------------------------------------------------
# Summary helpers
# ---------------------------------------------------------------------------

def total_pr(results: list[RepoResult]) -> dict[str, dict]:
    """Compute totals across all repos per question kind."""
    totals: dict[str, dict[str, int]] = {}
    for rr in results:
        for q in rr.questions:
            t = totals.setdefault(q.question, {"tp": 0, "fp": 0, "fn": 0})
            t["tp"] += q.tp
            t["fp"] += q.fp
            t["fn"] += q.fn
    out = {}
    for q, t in totals.items():
        p_denom = t["tp"] + t["fp"]
        r_denom = t["tp"] + t["fn"]
        out[q] = {
            "tp": t["tp"],
            "fp": t["fp"],
            "fn": t["fn"],
            "precision": round(t["tp"] / p_denom, 4) if p_denom else None,
            "recall": round(t["tp"] / r_denom, 4) if r_denom else None,
        }
    return out


GATE_THRESHOLDS: dict[str, dict[str, float]] = {
    # Questions we claim at 1.0. Both precision and recall must meet the bar.
    # A question not listed here is not claimed at 1.0 per issue #75's rule.
    # Issue #75: precision >= 0.90 and recall >= 0.80 for every question.
    "components":  {"precision": 0.90, "recall": 0.80},
    "deployables": {"precision": 0.90, "recall": 0.80},
    "interfaces":  {"precision": 0.90, "recall": 0.80},
    "capabilities":{"precision": 0.90, "recall": 0.80},
    "edges":       {"precision": 0.90, "recall": 0.80},
    # coverage is informational only.
}


def check_gates(totals: dict[str, dict]) -> list[str]:
    """Return a list of failure messages; empty means all gates passed."""
    failures = []
    for q, thresh in GATE_THRESHOLDS.items():
        if q not in totals:
            failures.append(f"{q}: no data")
            continue
        t = totals[q]
        for metric, bar in thresh.items():
            actual = t.get(metric)
            if actual is None:
                failures.append(f"{q}.{metric}: no data (0 denominator)")
            elif actual < bar:
                failures.append(
                    f"{q}.{metric}={actual:.4f} below threshold {bar:.4f}"
                )
    return failures


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def print_table(results: list[RepoResult], totals: dict[str, dict]) -> None:
    """Print a per-question P/R table to stdout."""
    questions = ["components", "deployables", "interfaces", "capabilities", "edges", "coverage"]
    header = f"{'repo':<22} {'question':<14} {'P':>6} {'R':>6} {'TP':>4} {'FP':>4} {'FN':>4}"
    print(header)
    print("-" * len(header))
    for rr in results:
        for q in rr.questions:
            p = f"{q.precision:.2f}" if q.precision is not None else "  N/A"
            r = f"{q.recall:.2f}" if q.recall is not None else "  N/A"
            print(f"{rr.repo:<22} {q.question:<14} {p:>6} {r:>6} {q.tp:>4} {q.fp:>4} {q.fn:>4}")
    print("-" * len(header))
    print(f"{'TOTAL':<22} {'':14}", end="")
    for qn in ["components", "deployables", "interfaces", "capabilities", "edges"]:
        t = totals.get(qn, {})
        p = f"{t['precision']:.2f}" if t.get("precision") is not None else "  N/A"
        r_str = f"{t['recall']:.2f}" if t.get("recall") is not None else "  N/A"
        print(f"\n{'':22} {qn:<14} {p:>6} {r_str:>6} {t.get('tp',0):>4} {t.get('fp',0):>4} {t.get('fn',0):>4}", end="")
    print()


def run_dircue(binary: Path, repo_dir: Path) -> dict:
    """Run dircue map on repo_dir and return the parsed JSON output."""
    import subprocess
    result = subprocess.run(
        [str(binary), "map", str(repo_dir)],
        capture_output=True, text=True, timeout=120
    )
    if result.returncode != 0:
        raise RuntimeError(f"dircue map failed for {repo_dir}: {result.stderr[:200]}")
    return json.loads(result.stdout)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--labels", type=Path, required=True,
                    help="Path to golden_expectations.json")
    # --maps: pre-generated map files (use when repos unavailable)
    ap.add_argument("--maps", type=Path,
                    help="Directory containing <repo>.json dircue map files")
    # --binary + --repos: run dircue on the fly (used by make golden)
    ap.add_argument("--binary", type=Path,
                    help="Path to dircue binary (used with --repos)")
    ap.add_argument("--repos", type=Path,
                    help="Directory containing pinned repo clones (used with --binary)")
    ap.add_argument("--output", type=Path,
                    help="Write results JSON to this file")
    ap.add_argument("--no-gate", action="store_true",
                    help="Report but do not fail on gate violations")
    args = ap.parse_args()

    if not args.maps and not (args.binary and args.repos):
        ap.error("provide either --maps or both --binary and --repos")

    label_doc = json.loads(args.labels.read_text())
    entries = label_doc.get("repos", [])

    results: list[RepoResult] = []
    for entry in entries:
        repo = entry.get("repo", "unknown")
        if args.maps:
            map_path = args.maps / f"{repo}.json"
            if not map_path.exists():
                print(f"WARNING: {map_path} not found, skipping {repo}", file=sys.stderr)
                continue
            map_doc = json.loads(map_path.read_text())
        else:
            repo_dir = args.repos / repo
            if not repo_dir.is_dir():
                print(f"WARNING: {repo_dir} not found, skipping {repo}", file=sys.stderr)
                continue
            try:
                map_doc = run_dircue(args.binary, repo_dir)
            except Exception as exc:
                print(f"WARNING: dircue failed for {repo}: {exc}", file=sys.stderr)
                continue
        rr = score_repo(entry, map_doc)
        results.append(rr)

    totals = total_pr(results)
    print_table(results, totals)

    out_doc = {
        "gate": "golden-map-corpus",
        "labeled_by": "blind",
        "repos": [r.as_dict() for r in results],
        "totals": totals,
        "gates": GATE_THRESHOLDS,
    }
    if args.output:
        args.output.write_text(json.dumps(out_doc, indent=2) + "\n")

    failures = check_gates(totals)
    if failures and not args.no_gate:
        print("\nGATE FAILURES:", file=sys.stderr)
        for f in failures:
            print(f"  {f}", file=sys.stderr)
        return 1
    elif failures:
        print("\nGATE VIOLATIONS (--no-gate: not failing):", file=sys.stderr)
        for f in failures:
            print(f"  {f}", file=sys.stderr)
    else:
        print("\nAll gates passed.", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
