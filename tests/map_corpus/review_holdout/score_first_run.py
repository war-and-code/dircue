#!/usr/bin/env python3
"""Bounded score for the committed, frozen review-holdout labels."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
RECEIPTS = HERE / "receipts"
LABEL_COMMIT = "0fb62770cc7faa80ab04c195dfc0e3d6f473bbaf"


def verify_frozen_label(path, repo_root):
    relpath = path.relative_to(repo_root)
    try:
        frozen = subprocess.run(
            ["git", "show", f"{LABEL_COMMIT}:{relpath.as_posix()}"],
            cwd=repo_root, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        ).stdout
    except (OSError, subprocess.CalledProcessError) as exc:
        raise SystemExit(f"cannot read frozen label {relpath} from {LABEL_COMMIT}") from exc
    current = path.read_bytes()
    require_frozen_label(current, frozen, relpath)
    return current


def require_frozen_label(current, frozen, relpath):
    if current != frozen:
        raise SystemExit(f"working label differs from frozen Git blob: {relpath}")


def evpaths(node):
    return {e.get("path") for e in node.get("evidence", [])}


def allpaths(node):
    return set(node.get("paths", [])) | evpaths(node)


def match_component(doc, label):
    cs = [n for n in doc["nodes"] if n["kind"] == "component" and n["properties"].get("root") == label["root"]]
    # Accept only the emitted component name or the Go module identity that
    # the map retains as a property. A single component at the expected root
    # is not enough to establish that it is the labeled component.
    exact = [n for n in cs if n["name"] == label["name"] or n["properties"].get("go_module") == label["name"]]
    return exact[0] if len(exact) == 1 else None


def match_deployable(doc, label):
    xs = [n for n in doc["nodes"] if n["kind"] == "deployable" and n["properties"].get("kind") == label["kind"] and label["path"] in allpaths(n)]
    exact = [n for n in xs if n["name"] == label["name"]]
    # Dockerfile roots serialize as "(root)"; other unexpected names are
    # unresolved rather than silently treated as semantic matches.
    if len(exact) == 1:
        return exact[0]
    root_alias = [n for n in xs if label["kind"] == "container_build"
                  and label["path"] == "Dockerfile"
                  and n["properties"].get("provider") == "dockerfile"
                  and n["name"] == "(root)"]
    return root_alias[0] if len(root_alias) == 1 else None


def match_interface(doc, label):
    kind = {"npm_start": "script"}.get(label["kind"], label["kind"])
    xs = [n for n in doc["nodes"] if n["kind"] == "interface" and n["properties"].get("interface_kind") == kind and label["path"] in allpaths(n)]
    if kind == "declared_port":
        xs = [n for n in xs if n["properties"].get("port") == str(label["name"])]
    if kind == "flask_application":
        xs = [n for n in xs if n["name"] == label["name"]]
    if kind == "script":
        xs = [n for n in xs if n["name"] == "start"]
    return xs[0] if len(xs) == 1 else None


def match_capability(doc, label, label_data):
    xs = [n for n in doc["nodes"] if n["kind"] == "capability" and n["name"] == label["capability"] and label["path"] in allpaths(n)]
    owner_label = next((c for c in label_data.get("components", [])
                        if c["name"] == label.get("owner")), None)
    if owner_label is None:
        return None
    owner = match_component(doc, owner_label)
    if owner is None:
        return None
    xs = [n for n in xs if n.get("properties", {}).get("owning_component") == owner["id"]]
    return xs[0] if len(xs) == 1 else None


def match_edge(doc, label, label_data):
    ids = {n["id"]: n for n in doc["nodes"]}
    comps = label_data.get("components", [])
    caps = label_data.get("capabilities", [])
    interfaces = label_data.get("interfaces", [])
    for edge in doc["edges"]:
        if edge["type"] != label["type"]:
            continue
        src, dst = ids[edge["from"]], ids[edge["to"]]
        if label["type"] == "builds":
            src_path, src_name = label["from"].split(":", 1)
            build_label = next((d for d in label_data.get("deployables", []) if d["kind"] == "container_build" and d["path"] == src_path), None)
            build_node = match_deployable(doc, build_label) if build_label else None
            target = next((c for c in comps if c["name"] == label["to"]), None)
            component = match_component(doc, target) if target else None
            if build_node and src_path in evpaths(edge) and src["id"] == build_node["id"] and component and dst["id"] == component["id"]:
                return edge
        elif label["type"] == "runs":
            src_path, src_name = label["from"].split(":", 1)
            target = next((c for c in comps if c["name"] == label["to"]), None)
            component = match_component(doc, target) if target else None
            if src["name"] == src_name and src_path in allpaths(src) and component and dst["id"] == component["id"]:
                return edge
        elif label["type"] == "depends_on":
            p1, n1 = label["from"].split(":", 1)
            p2, n2 = label["to"].split(":", 1)
            s_label = next((d for d in label_data.get("deployables", []) if d["path"] == p1 and d["name"] == n1), None)
            d_label = next((d for d in label_data.get("deployables", []) if d["path"] == p2 and d["name"] == n2), None)
            s_node = match_deployable(doc, s_label) if s_label else None
            d_node = match_deployable(doc, d_label) if d_label else None
            if s_node and d_node and src["id"] == s_node["id"] and dst["id"] == d_node["id"]:
                return edge
        elif label["type"] == "uses_capability":
            comp_label = next((c for c in comps if c["name"] == label["from"]), None)
            cap_label = next((c for c in caps if c["capability"] == label["to"]), None)
            comp = match_component(doc, comp_label) if comp_label else None
            cap = match_capability(doc, cap_label, label_data) if cap_label else None
            if comp and cap and src["id"] == comp["id"] and dst["id"] == cap["id"]:
                return edge
        elif label["type"] == "declares":
            comp_label = next((c for c in comps if c["name"] == label["from"]), None)
            int_label = next((i for i in interfaces if i["name"] == label["to"]), None)
            comp = match_component(doc, comp_label) if comp_label else None
            interface = match_interface(doc, int_label) if int_label else None
            if comp and interface and src["id"] == comp["id"] and dst["id"] == interface["id"]:
                return edge
    return None


def raw_edge_identity_match(doc, edge, label):
    ids = {n["id"]: n for n in doc["nodes"]}
    src, dst = ids[edge["from"]], ids[edge["to"]]
    if label["type"] in ("builds", "runs"):
        source_name = label["from"].split(":", 1)[1]
        target_name = label["to"]
        return src.get("name") == source_name and dst.get("name") == target_name
    if label["type"] == "depends_on":
        return src.get("name") == label["from"].split(":", 1)[1] and dst.get("name") == label["to"].split(":", 1)[1]
    if label["type"] in ("uses_capability", "declares"):
        return src.get("name") == label["from"] and dst.get("name") == label["to"]
    return True


def scoped_negative_checks(label, doc):
    repo = label["repo"]
    nodes, edges = doc["nodes"], doc["edges"]
    by_id = {n["id"]: n for n in nodes}
    def rows(facts, statuses, notes):
        return [{"fact": f["fact"], "status": statuses[i], "note": notes[i]} for i, f in enumerate(facts)]
    facts = label.get("bounded_negative_facts", [])
    if repo == "chi":
        root = next((n for n in nodes if n["kind"] == "component" and n["properties"].get("root") == "."), None)
        local_edges = [e for e in edges if e["type"] == "depends_on_local" and root and e["from"] == root["id"]]
        statuses = ["unscored_scope_mismatch", "unscored_scope_mismatch", "not_contradicted_by_map" if not local_edges else "contradicted_by_map"]
        notes = ["Frozen scope is go.mod, while the observed net:http-client edge cites Go source imports. This cannot contradict the manifest-only negative. Separately, the root capability assignment may conflate inbound net/http server use with outbound client use.", "The map attributes binary interfaces from nested _examples paths to the root component, but the frozen oracle chi.go is package chi and is not the source of those interfaces. This is outside the frozen negative's source scope and merits ownership/role review.", "No depends_on_local edge leaves the root component."]
    elif repo == "fastapi":
        http = [n for n in nodes if n["kind"] == "capability" and n["name"] == "net:http-client" and "pyproject.toml" in allpaths(n)]
        base = [n for n in http if n.get("properties", {}).get("condition") == "dependencies"]
        local = [e for e in edges if e["type"] == "depends_on_local"]
        deployable_from_oracles = [n for n in nodes if n["kind"] == "deployable" and bool(evpaths(n) & {"pyproject.toml", "fastapi/applications.py"}) and n["properties"].get("provider") != "github-actions"]
        statuses = ["not_contradicted_by_map" if not base else "contradicted_by_map", "not_contradicted_by_map" if not local else "contradicted_by_map", "not_contradicted_by_map" if not deployable_from_oracles else "contradicted_by_map"]
        notes = ["The map's httpx observation is tagged extra:all, not base dependencies; test-only SQLAlchemy is also outside the base requirements claim.", "No depends_on_local edges occur in the map.", "The map's deployment nodes come from workflow files, outside the two cited oracle paths."]
    elif repo == "changedetection":
        compose_builds = [e for e in edges if e["type"] == "builds" and any(by_id.get(e["from"], {}).get("properties", {}).get("provider") == "compose" for _ in [0])]
        compose_depends = [e for e in edges if e["type"] == "depends_on" and "docker-compose.yml" in evpaths(e)]
        services = [n for n in nodes if n["kind"] == "deployable" and n["properties"].get("provider") == "compose"]
        statuses = ["not_contradicted_by_map" if not compose_builds else "contradicted_by_map", "not_contradicted_by_map" if not compose_depends else "contradicted_by_map", "not_contradicted_by_map" if len(services) == 1 and services[0]["name"] == "changedetection" else "contradicted_by_map"]
        notes = ["The only builds edge originates at Dockerfile; no Compose build edge appears.", "No Compose depends_on edge appears.", "One active Compose service node, changedetection, appears; commented examples produced no service nodes."]
    elif repo == "umami":
        compose_builds = [e for e in edges if e["type"] == "builds" and any(by_id.get(e["from"], {}).get("properties", {}).get("provider") == "compose" for _ in [0])]
        db = next((n for n in nodes if n["kind"] == "deployable" and n["name"] == "db" and n["properties"].get("provider") == "compose"), None)
        reverse_dep = [e for e in edges if e["type"] == "depends_on" and db and e["from"] == db["id"]]
        db_port_edges = [e for e in edges if e["type"] == "exposes" and db and e["from"] == db["id"]]
        statuses = ["not_contradicted_by_map" if not compose_builds else "contradicted_by_map", "not_contradicted_by_map" if not db_port_edges and db else "contradicted_by_map", "not_contradicted_by_map" if not reverse_dep else "contradicted_by_map"]
        notes = ["No Compose builds edge appears; the observed builds edge originates at Dockerfile.", "The db deployable has no published-port/exposes edge; port 3000 is attached to the app component.", "The only Compose depends_on edge is umami → db."]
    else:
        return []
    return rows(facts, statuses, notes)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--write", action="store_true")
    args = ap.parse_args()
    report, hit_count, pos_count = [], 0, 0
    build = json.loads((RECEIPTS / "build.json").read_text())
    try:
        repo_root = Path(subprocess.run(
            ["git", "rev-parse", "--show-toplevel"], cwd=HERE,
            check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        ).stdout.strip())
    except (OSError, subprocess.CalledProcessError) as exc:
        raise SystemExit("cannot resolve Git root for frozen label verification") from exc
    for label_file in sorted(HERE.glob("*.json")):
        label_bytes = verify_frozen_label(label_file, repo_root)
        label = json.loads(label_bytes)
        receipt = json.loads((RECEIPTS / f"{label['repo']}.receipt.json").read_text())
        raw_path = RECEIPTS / receipt["stdout_file"]
        stderr_path = RECEIPTS / receipt["stderr_file"]
        raw_bytes, stderr_bytes = raw_path.read_bytes(), stderr_path.read_bytes()
        if hashlib.sha256(raw_bytes).hexdigest() != receipt["stdout_sha256"] or len(raw_bytes) != receipt["stdout_bytes"]:
            raise SystemExit(f"{label['repo']}: raw map hash/size does not match first-run receipt")
        if hashlib.sha256(stderr_bytes).hexdigest() != receipt["stderr_sha256"] or len(stderr_bytes) != receipt["stderr_bytes"]:
            raise SystemExit(f"{label['repo']}: stderr hash/size does not match first-run receipt")
        if receipt["exit_status"] != 0 or not receipt.get("stdout_json_valid"):
            raise SystemExit(f"{label['repo']}: first-run receipt is not successful valid JSON")
        if receipt.get("binary_sha256") != build.get("binary_sha256"):
            raise SystemExit(f"{label['repo']}: receipt binary hash differs from build metadata")
        if receipt.get("source_commit") != receipt.get("expected_commit"):
            raise SystemExit(f"{label['repo']}: receipt source commit differs from its expected pin")
        if label["repo"] != receipt["repo"] or label["commit"] != receipt["expected_commit"]:
            raise SystemExit(f"{label['repo']}: frozen source pin differs from first-run receipt")
        if build["label_commit"] != LABEL_COMMIT:
            raise SystemExit("receipt build metadata does not name the committed label freeze")
        doc = json.loads(raw_bytes)
        if doc.get("source", {}).get("commit") != label["commit"] or doc.get("source", {}).get("tree") != receipt["source_tree"]:
            raise SystemExit(f"{label['repo']}: raw map source identity differs from frozen pin/receipt")
        checks = []
        for cat in ("components", "deployables", "interfaces", "capabilities", "edges"):
            for fact in label.get(cat, []):
                if cat == "components": got = match_component(doc, fact)
                elif cat == "deployables": got = match_deployable(doc, fact)
                elif cat == "interfaces": got = match_interface(doc, fact)
                elif cat == "capabilities": got = match_capability(doc, fact, label)
                else: got = match_edge(doc, fact, label)
                changes = []
                if got and cat == "components":
                    expected_ecosystem = {"pypi": "python"}.get(fact["ecosystem"], fact["ecosystem"])
                    if got["properties"].get("ecosystem") != expected_ecosystem:
                        got = None
                    elif got["properties"].get("ecosystem") != fact["ecosystem"]:
                        changes.append(f"ecosystem label {fact['ecosystem']} emitted as {got['properties'].get('ecosystem')}")
                    if got and got["name"] != fact["name"] and got["properties"].get("go_module") != fact["name"]:
                        changes.append(f"name {fact['name']} emitted as {got['name']}")
                elif got and cat == "deployables" and got["name"] != fact["name"]:
                    changes.append(f"name {fact['name']} emitted as {got['name']}")
                elif got and cat == "interfaces":
                    expected_kind = {"npm_start": "script"}.get(fact["kind"], fact["kind"])
                    if fact["kind"] != expected_kind:
                        changes.append(f"interface kind {fact['kind']} emitted as {expected_kind}")
                    expected_name = f"port:{fact['name']}" if fact["kind"] == "declared_port" else ("start" if fact["kind"] == "npm_start" else fact["name"])
                    if fact["kind"] == "npm_start" and got["name"] != fact["name"]:
                        changes.append(f"name {fact['name']} emitted as {got['name']}")
                    elif got["name"] != expected_name:
                        changes.append(f"name {fact['name']} emitted as {got['name']}")
                    if fact["kind"] == "declared_port" and got["name"] != fact["name"]:
                        changes.append(f"port label {fact['name']} emitted as node name {got['name']} (port property agrees)")
                status = "absent" if not got else ("matched_with_label_map_drift" if changes else "matched")
                raw_exact = bool(got)
                if got and cat == "components":
                    raw_exact = (got["name"] == fact["name"] and got["properties"].get("ecosystem") == fact["ecosystem"])
                elif got and cat == "deployables":
                    raw_exact = got["name"] == fact["name"]
                elif got and cat == "interfaces":
                    raw_exact = (got["name"] == fact["name"] and got["properties"].get("interface_kind") == fact["kind"])
                elif got and cat == "edges":
                    raw_exact = raw_edge_identity_match(doc, got, fact)
                checks.append({"category": cat, "status": status, "raw_exact": raw_exact, "expected": fact, "observed_name": got.get("name") if got else None, "drift": changes})
                pos_count += 1
                hit_count += bool(got)
        cov = {x["question"]: {"status": x["status"], "reasons": x.get("reasons", [])} for x in doc.get("coverage", [])}
        expected_nodes = {"component": {x["name"] for x in label.get("components", [])}, "deployable": {x["name"] for x in label.get("deployables", [])}, "interface": {x["name"] for x in label.get("interfaces", [])}, "capability": {x["capability"] for x in label.get("capabilities", [])}}
        extras = {k: sorted({n["name"] for n in doc["nodes"] if n["kind"] == k and n["name"] not in expected_nodes[k]})[:12] for k in expected_nodes}
        report.append({"repo": label["repo"], "map_source_commit": doc.get("source", {}).get("commit"), "label_commit": label["commit"], "source_pin_match": doc.get("source", {}).get("commit") == label["commit"], "map_status": doc.get("status"), "positive_checks": checks, "positive_count": len(checks), "positive_matched": sum(x["status"] != "absent" for x in checks), "positive_raw_exact": sum(x["raw_exact"] for x in checks), "negative_checks": scoped_negative_checks(label, doc), "map_coverage": cov, "output_node_counts": {k: sum(n["kind"] == k for n in doc["nodes"]) for k in ("component", "deployable", "interface", "capability")}, "unscored_output_names_sample": extras})
    semantic_hits = sum(r["positive_matched"] for r in report)
    raw_hits = sum(r["positive_raw_exact"] for r in report)
    negs = [x for r in report for x in r["negative_checks"]]
    score = {"method": "Semantic matching uses frozen source paths, node category, and relationship endpoints. A second raw-exact measure requires emitted names/ecosystem/interface-kind and resolved endpoint names to equal frozen label values. Python pypi→python, npm_start→script, Dockerfile root name and port names are reported as label/map vocabulary drift. Missing positives are raw first-run discrepancies, not automatically adjudicated defects. Negative facts are explicitly not contradicted, contradicted, or unscored when output evidence lies outside frozen oracle scope.", "positive_assertions": {"total": pos_count, "raw_exact_matches": raw_hits, "raw_exact_recall": raw_hits/pos_count if pos_count else None, "semantic_matches_after_vocabulary_normalization": semantic_hits, "semantic_recall": semantic_hits/pos_count if pos_count else None, "absent_after_semantic_matching": pos_count-semantic_hits}, "negative_assertions": {"total": len(negs), "not_contradicted_by_map": sum(x["status"] == "not_contradicted_by_map" for x in negs), "contradicted_by_map": sum(x["status"] == "contradicted_by_map" for x in negs), "unscored_scope_mismatch": sum(x["status"] == "unscored_scope_mismatch" for x in negs), "per_fact": "See repositories[].negative_checks. Non-contradiction is not proof of absence; output evidence must fit the frozen oracle scope."}, "review_flags": ["Both app repositories lack the frozen Compose runs edge. docs/MAP.md:111 requires a matching declared image identity; the labels inferred identity from repository/package-name similarity. Treat both raw mismatches as source-label adjudication questions, not proven map defects.", "Chi's map output attaches net:http-client to the root component from imports including net/http in chi.go, and attaches nested _examples binaries to the root component. These are separate semantic/ownership concerns; the frozen negative checks are scoped to go.mod or chi.go and are marked unscored where the output evidence falls outside that scope."], "repositories": report}
    out = json.dumps(score, indent=2, ensure_ascii=False) + "\n"
    if args.write:
        (RECEIPTS / "score.json").write_text(out)
    print(out, end="")


if __name__ == "__main__":
    main()
