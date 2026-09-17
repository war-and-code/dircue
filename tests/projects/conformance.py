#!/usr/bin/env python3
"""Record project-map invariants on local checkouts without copying their contents."""
import argparse
import hashlib
import json
import pathlib
import subprocess
import sys


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def git_head(root):
    result = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"],
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False)
    return result.stdout.decode().strip() if result.returncode == 0 else None


def execute(binary, root):
    result = subprocess.run([str(binary), "analyze", "projects", "--json",
                             "--source", "directory", str(root)], capture_output=True, check=True)
    report = json.loads(result.stdout)
    report["root"] = "<corpus>"
    return report, result.stderr


def evaluate(binary, name, root):
    report, stderr = execute(binary, root)
    if (report, stderr) != execute(binary, root):
        raise AssertionError(f"{name}: repeated project reports differ")
    assert report["schema_version"] == "1.2.0"
    body = report["projects"]
    assert body["source"] == "directory" and "tree" not in body
    projects = body["projects"]
    assert [p["id"] for p in projects] == sorted(p["id"] for p in projects)
    assert len({p["id"] for p in projects}) == len(projects)
    totals = {key: sum(role[key] for role in body["composition"]) for key in ("files", "bytes")}
    for key in totals:
        attributed = sum(p[key] for p in projects) + body["unassigned"][key] + body["ambiguous"][key]
        assert attributed == totals[key], (name, key, attributed, totals[key])
    references = [r for p in projects for r in p["references"]]
    references += [r for c in body["configurations"] for r in c["references"]]
    for ref in references:
        assert ref["target_status"] in {"present", "missing", "unresolved"}
        if ref["target_status"] == "unresolved":
            assert "target" not in ref
        else:
            target = pathlib.PurePosixPath(ref["target"])
            assert not target.is_absolute() and ".." not in target.parts
    observed_manifests = sorted({p["id"] for p in projects} |
                                {c["path"] for c in body["configurations"]} |
                                {d["path"] for d in body["diagnostics"] if d["path"] != "."})
    inventory = hashlib.sha256()
    for manifest in observed_manifests:
        inventory.update(manifest.encode() + b"\0")
        inventory.update(sha256((root / manifest).read_bytes()).encode() + b"\n")
    states = {}
    for ref in references:
        states[ref["state"]] = states.get(ref["state"], 0) + 1
    diagnostics = {}
    for item in body["diagnostics"]:
        diagnostics[item["code"]] = diagnostics.get(item["code"], 0) + 1
    kinds = {}
    for project in projects:
        kinds[project["kind"]] = kinds.get(project["kind"], 0) + 1
    return {"corpus": name, "checkout_commit": git_head(root), "mode": "directory",
            "status": body["status"], "stderr_lines": len(stderr.splitlines()),
            "stderr_sha256": sha256(stderr), "projects": len(projects), "project_kinds": kinds,
            "configurations": len(body["configurations"]), "references": len(references),
            "reference_states": states, "diagnostic_codes": diagnostics,
            "composition": body["composition"], "unassigned": body["unassigned"],
            "ambiguous": body["ambiguous"], "omitted_files": body["omitted_files"],
            "observed_manifest_count": len(observed_manifests),
            "observed_manifest_inventory_sha256": inventory.hexdigest(),
            "normalized_report_sha256": sha256(json.dumps(report, sort_keys=True, separators=(",", ":")).encode()),
            "checks": ["repeated-output-identical", "schema-version", "directory-source",
                       "sorted-unique-projects", "exclusive-file-attribution", "exclusive-byte-attribution",
                       "reference-target-state", "root-contained-targets"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("corpora", nargs="+", help="NAME=CHECKOUT_PATH")
    args = parser.parse_args()
    binary = args.binary.resolve()
    cases = []
    for value in args.corpora:
        name, separator, location = value.partition("=")
        if not separator or not name or not location:
            parser.error("corpora must use NAME=CHECKOUT_PATH")
        cases.append(evaluate(binary, name, pathlib.Path(location).resolve()))
    receipt = {"binary_sha256": sha256(binary.read_bytes()), "cases": cases,
               "scope": "Project inventory consistency and deterministic output; not proof of effective build membership."}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    print(f"Validated {len(cases)} corpora; receipt: {args.output}")


if __name__ == "__main__":
    main()
