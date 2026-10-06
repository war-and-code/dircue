#!/usr/bin/env python3
"""Hand-labeled acceptance checks for dircue's 1.5.0 structural assessment."""

import argparse
import hashlib
import json
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parents[2]
RECEIPT_DEFAULT = ROOT / ".cache/assessment150/receipt.json"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def put(root, relative, contents):
    """Write one fixture file and return its exact UTF-8 byte count."""
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    data = contents.encode("utf-8") if isinstance(contents, str) else contents
    path.write_bytes(data)
    return len(data)


def run(candidate, *args, cwd=None, timeout=120):
    result = subprocess.run([str(candidate), *map(str, args)], cwd=cwd,
                            capture_output=True, timeout=timeout)
    if result.returncode:
        raise AssertionError(f"command failed ({result.returncode}): "
                             f"{[str(candidate), *map(str, args)]!r}\n"
                             f"stderr={result.stderr[-3000:]!r}")
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"invalid JSON from candidate: {result.stdout[-1000:]!r}") from exc


def msbuild_oracle(dotnet, project, label):
    """Query effective MSBuild properties/items, without invoking a target."""
    cache = ROOT / ".cache/assessment150/msbuild_oracle"
    cache.mkdir(parents=True, exist_ok=True)
    env = dict(__import__("os").environ)
    env.update({"DOTNET_CLI_TELEMETRY_OPTOUT": "1",
                "DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1",
                "DOTNET_CLI_HOME": str(ROOT / ".cache/assessment150/dotnet-home"),
                "DOTNET_NOLOGO": "1"})
    result = subprocess.run([str(dotnet), "msbuild", str(project), "-nologo", "-verbosity:quiet",
                             "-getProperty:NuGetLockFilePath,ManagePackageVersionsCentrally,UseIt",
                             "-getItem:PackageReference,PackageVersion"],
                            env=env, capture_output=True, timeout=60)
    if result.returncode:
        raise AssertionError(f"controlled MSBuild query failed for {project}: {result.stderr[-2000:]!r}")
    raw = result.stdout.decode("utf-8")
    (cache / f"{label}.json").write_text(raw)
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"MSBuild -getProperty/-getItem returned invalid JSON for {project}: {raw!r}") from exc


def msbuild_properties_oracle(dotnet, project, label):
    """Query only properties; include MSBuildProjectDirectory so empty values still emit JSON."""
    cache = ROOT / ".cache/assessment150/msbuild_oracle"
    cache.mkdir(parents=True, exist_ok=True)
    env = dict(__import__("os").environ)
    env.update({"DOTNET_CLI_TELEMETRY_OPTOUT": "1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1",
                "DOTNET_CLI_HOME": str(ROOT / ".cache/assessment150/dotnet-home"), "DOTNET_NOLOGO": "1"})
    result = subprocess.run([str(dotnet), "msbuild", str(project), "-nologo", "-verbosity:quiet",
                             "-getProperty:NuGetLockFilePath,MSBuildProjectDirectory"],
                            env=env, capture_output=True, timeout=60)
    if result.returncode:
        raise AssertionError(f"controlled MSBuild precedence query failed for {project}: {result.stderr[-2000:]!r}")
    raw = result.stdout.decode("utf-8")
    (cache / f"{label}.json").write_text(raw)
    return json.loads(raw)


def oracle_summary(data):
    properties = data.get("Properties", {})
    items = data.get("Items", {})
    return {
        "properties": {key: properties.get(key, "") for key in
                       ("NuGetLockFilePath", "ManagePackageVersionsCentrally", "UseIt")},
        "package_references": sorted(item.get("Identity", "") for item in items.get("PackageReference", [])),
        "package_versions": sorted([item.get("Identity", ""), item.get("Version", "")]
                                    for item in items.get("PackageVersion", [])),
    }


def expect_failure(candidate, *args):
    result = subprocess.run([str(candidate), *map(str, args)], capture_output=True, timeout=30)
    if result.returncode == 0:
        raise AssertionError(f"invalid invocation unexpectedly succeeded: {args!r}")
    if result.stdout.strip().startswith(b"{"):
        raise AssertionError(f"invalid invocation emitted a success report: {args!r}")


def check_saved_reports(candidate, reports, temp):
    """Export CLI schemas once, validate every saved report offline, then native-load compare it."""
    exported = {}
    for name in ("profile", "assessment"):
        path = temp / f"exported-{name}-schema.json"
        command = [str(candidate), "capabilities", "--schema", name, "--json"]
        result = subprocess.run([str(candidate), "capabilities", "--schema", name, "--json"],
                                capture_output=True, timeout=30)
        if result.returncode:
            saved = preserve_acceptance_failure(-1, f"export-{name}", None, exported, command,
                                                 result.stdout, result.stderr)
            raise AssertionError(f"schema export failed for {name}: {result.stderr[-2000:]!r}")
        path.write_bytes(result.stdout)
        json.loads(result.stdout)
        exported[name] = path
    validator = ROOT / "tests/assessment_v150/schema_validate.go"
    for index, report in enumerate(reports):
        report_path = temp / f"saved-report-{index:03d}.json"
        report_path.write_text(json.dumps(report, sort_keys=True) + "\n")
        for schema_name in ("profile", "assessment"):
            command = ["go", "run", str(validator), str(exported[schema_name]), str(report_path), schema_name]
            result = subprocess.run(command, cwd=ROOT,
                                    capture_output=True, timeout=90)
            if result.returncode:
                saved = preserve_acceptance_failure(index, f"schema-{schema_name}", report_path, exported,
                                                     command, result.stdout, result.stderr)
                raise AssertionError(f"offline {schema_name} schema rejected saved fixture {index}: "
                                     f"{result.stderr[-3000:]!r}; reproduction: {saved}")
        command = [str(candidate), "compare", str(report_path), str(report_path), "--json"]
        compare = subprocess.run(command,
                                 capture_output=True, timeout=45)
        if compare.returncode:
            saved = preserve_acceptance_failure(index, "native-compare", report_path, exported, command,
                                                 compare.stdout, compare.stderr)
            raise AssertionError(f"native compare loader rejected saved report fixture {index}: "
                                 f"{compare.stderr[-3000:]!r}; reproduction: {saved}")
        try:
            diff = json.loads(compare.stdout)
        except json.JSONDecodeError as exc:
            saved = preserve_acceptance_failure(index, "native-json", report_path, exported, command,
                                                 compare.stdout, compare.stderr)
            raise AssertionError(f"native compare output invalid for fixture {index}: {compare.stdout[-1000:]!r}; reproduction: {saved}") from exc
        if not isinstance(diff, dict):
            saved = preserve_acceptance_failure(index, "native-shape", report_path, exported, command,
                                                 compare.stdout, compare.stderr)
            raise AssertionError(f"native compare returned unexpected shape for fixture {index}: {diff!r}; reproduction: {saved}")


def preserve_acceptance_failure(index, phase, report_path, schemas, command, stdout, stderr):
    """Retain only a failing synthetic reproducer and tool outputs under ignored cache."""
    root = ROOT / ".cache/assessment150/failures"
    bundle = root / f"fixture-{index:03d}-{phase}-{time.time_ns()}"
    bundle.mkdir(parents=True, exist_ok=False)
    if report_path is not None and Path(report_path).is_file():
        shutil.copyfile(report_path, bundle / "report.json")
    for name, path in schemas.items():
        if path.is_file():
            shutil.copyfile(path, bundle / f"{name}-schema.json")
    (bundle / "command.json").write_text(json.dumps(command, indent=2) + "\n")
    (bundle / "stdout.bin").write_bytes(stdout or b"")
    (bundle / "stderr.bin").write_bytes(stderr or b"")
    return bundle


def check_corruption_rejected(candidate, reports, temp):
    """Each independent accounting/evidence axis must reject locally corrupted saved data."""
    mutations = [
        ("population", "population", lambda r: r["assessment"]["structure"]["populations"][0]["metric"].update(count=-1)),
        ("population_overflow", "population", lambda r: r["assessment"]["structure"]["populations"][0]["metric"].update(count=1 << 80)),
        ("workspace_group", "workspace_group", lambda r: r["assessment"]["structure"]["workspace_groups"][0].update(member_count=999)),
        ("dependency", "dependency", lambda r: r["assessment"]["structure"]["dependencies"].update(qualified_reference_count=999)),
        ("entry_point", "entry_point", lambda r: r["assessment"]["structure"]["entry_points"][0].update(evidence_path="")),
        ("missing_entry_coverage", "entry_point", lambda r: r["assessment"]["structure"].update(coverage=[row for row in r["assessment"]["structure"]["coverage"] if row.get("scope") != "entry_points"])),
        ("lock_evidence", "lock_evidence", lambda r: r["lockfiles"]["contexts"][0]["nuget_evidence"].update(candidate_count=999)),
        ("impossible_lock_partition", "lock_evidence", lambda r: r["assessment"]["lockfiles_overall"]["missing"].update(count=r["assessment"]["lockfiles_overall"]["missing"]["count"] + 1)),
    ]
    for axis, key, mutate in mutations:
        valid = reports[key]
        bad = json.loads(json.dumps(valid))
        mutate(bad)
        path = temp / f"corrupt-{axis}.json"
        path.write_text(json.dumps(bad, sort_keys=True) + "\n")
        result = subprocess.run([str(candidate), "compare", str(path), str(path), "--json"],
                                capture_output=True, timeout=45)
        if result.returncode == 0:
            saved = preserve_acceptance_failure(-1, f"corruption-{axis}", path, {},
                                                 [str(candidate), "compare", str(path), str(path), "--json"],
                                                 result.stdout, result.stderr)
            raise AssertionError(f"native saved-report loader accepted corrupted {axis} accounting/evidence; reproduction: {saved}")


def assessment(report):
    value = report.get("assessment")
    if not isinstance(value, dict):
        raise AssertionError("profile has no assessment object")
    if value.get("version") != "1.1.0":
        raise AssertionError(f"expected assessment v1.1.0 carrying the 1.5.0 structure: {value.get('version')!r}")
    structure = value.get("structure")
    if not isinstance(structure, dict):
        raise AssertionError("assessment omits required structural report")
    return value, structure


def metric_count(metric, label):
    if not isinstance(metric, dict) or not isinstance(metric.get("count"), int):
        raise AssertionError(f"{label}: expected count metric, got {metric!r}")
    return metric["count"]


def population(structure, name, ecosystem=None):
    rows = [row for row in structure.get("populations", [])
            if row.get("population") == name and
            (ecosystem is None or row.get("ecosystem") == ecosystem)]
    return sum(metric_count(row.get("metric"), f"{name}/{ecosystem}") for row in rows)


def stable_report(report):
    """Remove only selected-source identity; retain all measured evidence."""
    normalized = json.loads(json.dumps(report))
    def remove_identity(node, top=False):
        if isinstance(node, dict):
            if top:
                # The profile root is the selected checkout path, not a
                # project root retained inside any module's evidence.
                node.pop("root", None)
            has_source_identity = "source" in node
            for key in list(node):
                if key == "source" or has_source_identity and key in {"tree", "commit", "consistency"}:
                    del node[key]
                else:
                    remove_identity(node[key])
        elif isinstance(node, list):
            for child in node:
                remove_identity(child)

    remove_identity(normalized, top=True)
    return normalized


def reject_policy_language(value):
    forbidden_keys = re.compile(r"(^|_)(verdict|independence|independent|sufficiency|quality_grade|risk_score)($|_)", re.I)
    forbidden_values = re.compile(r"\b(independent|independence|sufficient|insufficient|healthy|unhealthy|compliant|noncompliant)\b", re.I)

    def visit(node, path="$", parent_key=""):
        if isinstance(node, dict):
            for key, child in node.items():
                if forbidden_keys.search(key):
                    raise AssertionError(f"policy/independence field present at {path}.{key}")
                visit(child, f"{path}.{key}", key)
        elif isinstance(node, list):
            for index, child in enumerate(node):
                visit(child, f"{path}[{index}]", parent_key)
        elif isinstance(node, str) and forbidden_values.search(node):
            raise AssertionError(f"policy/independence claim present at {path}: {node!r}")

    visit(value)


def write_large_single(root):
    contents = {"go.mod": "module example.invalid/large-one\n\ngo 1.23\n"}
    for index in range(48):
        if index == 0:
            rel = "cmd/app/main.go"
            body = "package main\nfunc main() { run() }\n"
        else:
            rel = f"internal/unit{index:02d}/unit.go"
            body = f"package unit{index:02d}\nfunc Value() int {{ return {index} }}\n"
        contents[rel] = body
    byte_count = sum(put(root, name, body) for name, body in contents.items())
    return len(contents), byte_count


def write_many_dotnet(root):
    projects = ["app/App.csproj"]
    projects += [f"src/Library{i:02d}/Library{i:02d}.csproj" for i in range(8)]
    projects += [f"tests/Library{i:02d}.Tests/Library{i:02d}.Tests.csproj" for i in range(2)]
    sln = ["Microsoft Visual Studio Solution File, Format Version 12.00", "# Visual Studio Version 17"]
    project_guid = "{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}"
    for number, project in enumerate(projects, 1):
        name = Path(project).stem
        guid = "{" + f"{number:08X}" + "-0000-0000-0000-000000000000}"
        sln += [f'Project("{project_guid}") = "{name}", "{project}", "{guid}"', "EndProject"]
    files = {"Product.sln": "\n".join(sln) + "\n"}
    for project in projects:
        if project == projects[0]:
            targets = [f"../src/Library{i:02d}/Library{i:02d}.csproj" for i in range(8)]
        elif project.startswith("tests/"):
            targets = ["../../src/Library00/Library00.csproj"]
        else:
            targets = []
        ref = "" if not targets else "<ItemGroup>" + "".join(
            f"<ProjectReference Include=\"{target}\" />" for target in targets) + "</ItemGroup>"
        files[project] = f"<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework>{'<OutputType>Exe</OutputType>' if project == projects[0] else ''}</PropertyGroup>{ref}</Project>\n"
    files["app/Program.cs"] = "Console.WriteLine(1);\n"
    files["README.md"] = "synthetic app with libraries and tests\n"
    byte_count = sum(put(root, name, body) for name, body in files.items())
    return len(files), byte_count, projects


def write_ecosystem_workspaces(root):
    files = {
        "maven/pom.xml": (
            "<project><modelVersion>4.0.0</modelVersion><groupId>test</groupId>"
            "<artifactId>parent</artifactId><version>1</version><packaging>pom</packaging>"
            "<modules><module>service</module></modules></project>\n"),
        "maven/service/pom.xml": "<project><modelVersion>4.0.0</modelVersion><artifactId>service</artifactId></project>\n",
        "gradle/settings.gradle": "rootProject.name = 'root'\ninclude(':service')\n",
        "gradle/build.gradle": "plugins { id 'base' }\n",
        "gradle/service/build.gradle": "plugins { id 'java' }\n",
        "npm/package.json": json.dumps({"name": "fixture-root", "private": True, "workspaces": ["packages/app"]}, sort_keys=True) + "\n",
        "npm/packages/app/package.json": json.dumps({"name": "fixture-app", "version": "1.0.0"}, sort_keys=True) + "\n",
        "uv/pyproject.toml": "[tool.uv.workspace]\nmembers = [\"packages/app\"]\n",
        "uv/packages/app/pyproject.toml": "[project]\nname = \"uv-app\"\nversion = \"0.1.0\"\n",
        "cargo/Cargo.toml": "[workspace]\nmembers = [\"crates/app\"]\n",
        "cargo/crates/app/Cargo.toml": "[package]\nname = \"cargo-app\"\nversion = \"0.1.0\"\n",
        "go/go.work": "go 1.23\n\nuse ./cmd/app\n",
        "go/cmd/app/go.mod": "module example.invalid/work/app\n\ngo 1.23\n",
    }
    byte_count = sum(put(root, name, body) for name, body in files.items())
    return len(files), byte_count


def write_graph(root, with_entrypoints=True):
    files = {
        "A/A.csproj": (
            "<Project><ItemGroup>"
            "<ProjectReference Include=\"../B/B.csproj\" />"
            "<ProjectReference Include=\"../B/B.csproj\" Condition=\"'$(TargetFramework)' == 'net8.0'\" />"
            "<ProjectReference Include=\"../Missing/Missing.csproj\" />"
            "<ProjectReference Include=\"../C/C.csproj\" Condition=\"'$(TargetFramework)' == 'net8.0'\" />"
            "</ItemGroup></Project>\n"),
        "B/B.csproj": "<Project><ItemGroup><ProjectReference Include=\"../A/A.csproj\" /></ItemGroup></Project>\n",
        "C/C.csproj": "<Project />\n",
        "npm/package.json": json.dumps({"name": "launch-fixture", "scripts": (
            {"start": "node server.js", "serve": "node alternate.js"} if with_entrypoints
            else {"serve": "node alternate.js"})}, sort_keys=True) + "\n",
        "npm/server.js": "console.log('server');\n",
        "npm/alternate.js": "console.log('alternate');\n",
    }
    return sum(put(root, name, body) for name, body in files.items()), len(files)


def write_inflated(root, count):
    base = {
        "package.json": json.dumps({"name": "inflation", "private": True, "workspaces": ["packages/*"]}, sort_keys=True) + "\n",
        "packages/app/package.json": '{"name":"app","version":"1.0.0"}\n',
        "src/.gitattributes": "generated/** linguist-generated=true\n",
    }
    byte_count = sum(put(root, name, body) for name, body in base.items())
    vendor_count = count * 3 // 4
    for index in range(count):
        # Vendor data and generated source inflation are intentionally
        # unrelated to manifests or project roots.
        if index < vendor_count:
            name = f"vendor/archive/chunk-{index:04d}.txt"
            body = f"vendor fixture {index:04d} " + ("x" * 96) + "\n"
        else:
            generated = index - vendor_count
            name = f"src/generated/auto-{generated:04d}.cs"
            body = f"// generated fixture {generated:04d}\ninternal class Auto{generated:04d} {{ }}\n"
        byte_count += put(root, name, body)
    return len(base) + count, byte_count, vendor_count, count - vendor_count


def write_bounded(root):
    # 70 explicit npm workspace members exceed the report's 64-member sample cap.
    members = [f"packages/p{index:03d}" for index in range(70)]
    byte_count = put(root, "package.json", json.dumps({"name": "bounded-root", "private": True, "workspaces": ["packages/*"]}, sort_keys=True) + "\n")
    for rel in members:
        byte_count += put(root, f"{rel}/package.json", json.dumps({"name": rel.rsplit("/", 1)[-1], "version": "1.0.0"}, sort_keys=True) + "\n")
    # Independent of the npm group above: filename evidence has a finite sample
    # while the inventory/candidate population remains exact.
    for index in range(270):
        byte_count += put(root, f"maven/p{index:03d}/pom.xml", "<project><modelVersion>4.0.0</modelVersion></project>\n")
    return len(members) + 1 + 270, byte_count, len(members) + 1 + 270


def assert_workspace_group(structure, ecosystem, member_count, kind="workspace"):
    matches = [group for group in structure.get("workspace_groups", [])
               if group.get("ecosystem") == ecosystem and group.get("kind") == kind]
    if not matches:
        raise AssertionError(f"missing {ecosystem} {kind} group; groups={structure.get('workspace_groups')!r}")
    group = max(matches, key=lambda item: item.get("member_count", 0))
    if group.get("member_count") != member_count:
        raise AssertionError(f"{ecosystem} group should have {member_count} members, got {group!r}")
    return group


def check_cli_negatives(candidate, root):
    expect_failure(candidate, "analyze", "assessment", "--source", "nonsense", "--json", root)
    expect_failure(candidate, "analyze", "assessment", "--workers", "-1", "--source", "directory", "--json", root)
    expect_failure(candidate, "analyze", "assessment", "--max-file-bytes", "-1", "--source", "directory", "--json", root)
    limited = run(candidate, "analyze", "assessment", "--max-file-bytes", "1", "--source", "directory", "--json", root)
    report, _ = assessment(limited)
    if metric_count(report["unparsed_manifest_candidates"], "byte-capped unparsed manifests") < 1:
        raise AssertionError("one-byte input limit did not disclose manifest parser omissions")
    if report["projects"].get("completeness") != "lower_bound":
        raise AssertionError("one-byte input limit did not qualify parsed-project coverage")
    selected = [path for path in root.rglob("*") if path.is_file()]
    if metric_count(report["inventory"]["files"], "byte-capped exact files") != len(selected):
        raise AssertionError("parser byte cap changed exact selected file count")
    if metric_count(report["inventory"]["bytes"], "byte-capped exact bytes") != sum(path.stat().st_size for path in selected):
        raise AssertionError("parser byte cap changed exact selected byte count")
    baseline, _ = assessment(run(candidate, "analyze", "assessment", "--source", "directory", "--json", root))
    if metric_count(report["manifest_candidate_population"], "byte-capped manifest candidate population") != metric_count(baseline["manifest_candidate_population"], "baseline manifest candidates"):
        raise AssertionError("parser byte cap changed exact manifest-candidate population")
    return limited, run(candidate, "analyze", "assessment", "--source", "directory", "--json", root)


def check_empty_entrypoint_coverage(report):
    structure = report.get("assessment", {}).get("structure", {})
    count = structure.get("entry_point_count", len(structure.get("entry_points", [])))
    if count == 0:
        complete = [row for row in structure.get("coverage", [])
                    if row.get("scope") == "entry_points" and row.get("status") == "complete"]
        if complete:
            raise AssertionError(f"empty entry-point sample cannot imply complete source/deployment discovery: {complete!r}")


def git_commit(root):
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.email", "assessment-fixture@example.invalid"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.name", "Synthetic Assessment Fixture"], cwd=root, check=True)
    subprocess.run(["git", "add", "-A"], cwd=root, check=True)
    subprocess.run(["git", "commit", "-qm", "synthetic assessment fixture"], cwd=root, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=RECEIPT_DEFAULT)
    parser.add_argument("--dotnet", type=Path, help="optional isolated dotnet executable for controlled fixtures only")
    args = parser.parse_args()
    candidate = args.candidate.resolve()
    if not candidate.is_file():
        parser.error(f"candidate binary not found: {candidate}")

    receipt = {
        "candidate_sha256": sha(candidate),
        "harness_sha256": sha(Path(__file__)),
        "platform": platform.platform(),
        "oracle": {"dotnet": str(args.dotnet.resolve()) if args.dotnet else None,
                   "status": "not_requested" if args.dotnet else "unavailable_without_isolated_sdk"},
        "fixtures": [],
        "method": "Temporary hand-authored repositories with explicit expected file and byte populations. "
                  "Static parser facts are asserted from literal fixture declarations. No package manager, "
                  "restore, build, network access, public corpus, or private data is used. The optional "
                  "MSBuild comparison is restricted to controlled fixtures.",
    }

    with tempfile.TemporaryDirectory(prefix="dircue-assessment150-") as temporary:
        temp = Path(temporary)

        large = temp / "large-single"
        large.mkdir()
        files, byte_count = write_large_single(large)
        large_report = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "1", "--json", large)
        native, structure = assessment(large_report)
        if metric_count(native["inventory"]["files"], "large inventory files") != files:
            raise AssertionError("large single-project file count differs from hand-counted files")
        if metric_count(native["inventory"]["bytes"], "large inventory bytes") != byte_count:
            raise AssertionError("large single-project bytes differ from hand-counted UTF-8 lengths")
        if population(structure, "parsed_projects", "go") != 1:
            raise AssertionError("48 source files in one go.mod must remain one parsed Go project")
        reject_policy_language(large_report)
        receipt["fixtures"].append({"id": "large_single_go_project", "files": files, "bytes": byte_count,
                                    "parsed_go_projects": 1, "assessment_version": native["version"]})

        dotnet = temp / "many-dotnet"
        dotnet.mkdir()
        files, byte_count, projects = write_many_dotnet(dotnet)
        dotnet_report = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "8", "--json", dotnet)
        native, structure = assessment(dotnet_report)
        if metric_count(native["inventory"]["files"], "dotnet inventory files") != files or metric_count(native["inventory"]["bytes"], "dotnet inventory bytes") != byte_count:
            raise AssertionError(".NET fixture inventory differs from hand count")
        # The assessment's project population names this parser family `nuget`;
        # the solution group itself correctly records the `.sln` ecosystem.
        if population(structure, "parsed_projects", "nuget") != len(projects):
            raise AssertionError(f"one app, eight libraries, and two test projects should parse as {len(projects)} projects")
        solutions = structure.get("solution_groups", [])
        solution = next((group for group in solutions if group.get("member_count") == len(projects)), None)
        if solution is None:
            raise AssertionError(f"solution should retain all {len(projects)} explicit members: {solutions!r}")
        receipt["fixtures"].append({"id": "one_app_many_dotnet_projects", "files": files, "bytes": byte_count,
                                    "project_count": len(projects), "solution_member_count": solution["member_count"]})

        same_root = temp / "same-root-dotnet-roles"
        same_root.mkdir()
        same_root_files = {
            "apps/App.csproj": "<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n",
            "apps/App.Tests.csproj": "<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n",
        }
        same_root_bytes = sum(put(same_root, name, body) for name, body in same_root_files.items())
        same_root_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", same_root)
        _, same_root_structure = assessment(same_root_report)
        if population(same_root_structure, "parsed_projects", "nuget") != 2:
            raise AssertionError("same-root app and test project must remain two parsed .NET projects")
        role_counts = {(row.get("population"), row.get("ecosystem"), row.get("role")): row["metric"]["count"]
                       for row in same_root_structure.get("populations", [])
                       if row.get("ecosystem") == "nuget"}
        if role_counts.get(("parsed_projects", "nuget", "primary")) != 1 or role_counts.get(("parsed_projects", "nuget", "test")) != 1:
            raise AssertionError(f"same-root projects should remain partitioned by primary/test role: {role_counts!r}")
        distinct_roots = sum(count for (pop, eco, _role), count in role_counts.items()
                             if pop == "distinct_roots" and eco == "nuget")
        if distinct_roots != 1:
            raise AssertionError(f"two role-partitioned projects at one physical root should count one unique root, got {distinct_roots}")
        if metric_count(same_root_report["assessment"]["inventory"]["files"], "same-root files") != 2 or metric_count(same_root_report["assessment"]["inventory"]["bytes"], "same-root bytes") != same_root_bytes:
            raise AssertionError("same-root role partition fixture inventory differs")
        receipt["fixtures"].append({"id": "same_root_dotnet_role_partition", "files": 2, "bytes": same_root_bytes,
                                    "parsed_projects": 2, "role_project_counts": {"primary": 1, "test": 1},
                                    "distinct_physical_roots": 1})

        many_solutions = temp / "many-solutions"
        many_solutions.mkdir()
        put(many_solutions, "src/App.csproj", "<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n")
        solution_count = 270
        solution_header = ["Microsoft Visual Studio Solution File, Format Version 12.00", "# Visual Studio Version 17"]
        project_type = "{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}"
        solution_bytes = (many_solutions / "src/App.csproj").stat().st_size
        for index in range(solution_count):
            guid = "{" + f"{index + 1:08X}" + "-0000-0000-0000-000000000000}"
            contents = "\n".join(solution_header + [
                f'Project("{project_type}") = "App{index:03d}", "../src/App.csproj", "{guid}"',
                "EndProject", "Global", "EndGlobal", ""])
            solution_bytes += put(many_solutions, f"solutions/S{index:03d}.sln", contents)
        many_solution_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", many_solutions)
        _, many_solution_structure = assessment(many_solution_report)
        retained_solutions = many_solution_structure.get("solution_groups", [])
        if many_solution_structure.get("solution_group_count") != solution_count:
            raise AssertionError(f"solution group exact total should be {solution_count}: {many_solution_structure.get('solution_group_count')!r}")
        if len(retained_solutions) > 256 or many_solution_structure.get("omitted_solution_groups") != solution_count - len(retained_solutions):
            raise AssertionError(f"solution group samples should be bounded without losing exact omitted totals: retained={len(retained_solutions)} structure={many_solution_structure!r}")
        if any(group.get("member_count") != 1 for group in retained_solutions):
            raise AssertionError("retained synthetic solution groups should each have one declared project member")
        if metric_count(many_solution_report["assessment"]["inventory"]["files"], "many solution exact files") != solution_count + 1 or metric_count(many_solution_report["assessment"]["inventory"]["bytes"], "many solution exact bytes") != solution_bytes:
            raise AssertionError("many-solution fixture exact inventory differs")
        receipt["fixtures"].append({"id": "solution_group_population_over_sample_cap", "files": solution_count + 1,
                                    "bytes": solution_bytes, "exact_solution_groups": solution_count,
                                    "retained_solution_groups": len(retained_solutions),
                                    "omitted_solution_groups": many_solution_structure["omitted_solution_groups"]})

        workspaces = temp / "workspaces"
        workspaces.mkdir()
        ws_files, ws_bytes = write_ecosystem_workspaces(workspaces)
        report = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "1", "--json", workspaces)
        native, structure = assessment(report)
        if metric_count(native["inventory"]["files"], "workspace inventory files") != ws_files or metric_count(native["inventory"]["bytes"], "workspace inventory bytes") != ws_bytes:
            raise AssertionError("mixed workspace fixture inventory differs from hand count")
        for ecosystem in ("maven", "npm", "python-uv", "cargo", "go"):
            assert_workspace_group(structure, ecosystem, 1)
        gradle_group = assert_workspace_group(structure, "gradle", 0)
        if gradle_group.get("unresolved_member_count") != 1 or not any(
                member.get("state") == "conditional" for member in gradle_group.get("unresolved_members", [])):
            raise AssertionError(f"Gradle include should remain qualified static evidence: {gradle_group!r}")
        receipt["fixtures"].append({"id": "six_explicit_ecosystem_workspaces", "files": ws_files, "bytes": ws_bytes,
                                    "ecosystems": ["maven", "gradle-qualified", "npm", "python-uv", "cargo", "go"]})

        wider = temp / "wider-ecosystems"
        wider.mkdir()
        wider_files = {
            "dart/app/pubspec.yaml": "name: app\ndependencies:\n  local_pkg:\n    path: ../local_pkg\n",
            "dart/local_pkg/pubspec.yaml": "name: local_pkg\n",
            "dart/app/bin/main.dart": "void main() {}\n",
            "flat/readme.txt": "plain fixture text\n",
            "flat/data.xml": "<catalog><entry>fixture</entry></catalog>\n",
            "flat/numbers.csv": "key,value\na,1\n",
            "unsupported/python/pyproject.toml": "[project]\nname='unsupported-lock-fixture'\nversion='1.0.0'\ndependencies=['sample==1']\n",
            "unsupported/python/requirements.lock": "sample==1\n",
            "console/pyproject.toml": "[project]\nname='console-fixture'\nversion='1.0.0'\n[project.scripts]\nwider-cli='wider.cli:main'\n",
        }
        wider_bytes = sum(put(wider, name, body) for name, body in wider_files.items())
        wider_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", wider)
        wider_assessment, wider_structure = assessment(wider_report)
        if metric_count(wider_assessment["inventory"]["files"], "wider ecosystem inventory") != len(wider_files) or metric_count(wider_assessment["inventory"]["bytes"], "wider ecosystem bytes") != wider_bytes:
            raise AssertionError("wider ecosystem fixture inventory differs from hand-counted file/byte totals")
        if population(wider_structure, "parsed_projects", "dart-pub") != 2:
            raise AssertionError("two Dart pubspec roots should be recorded as two parsed projects")
        python_entries = [entry for entry in wider_structure.get("entry_points", [])
                          if entry.get("ecosystem") == "python" and entry.get("name") == "wider-cli"]
        if len(python_entries) != 1 or python_entries[0].get("target") != "wider.cli:main":
            raise AssertionError(f"Python project.scripts console entry should appear once in the neutral entry catalog: {python_entries!r}")
        dart_refs = [edge for edge in wider_structure["dependencies"].get("edges", [])
                     if edge.get("ecosystem") == "dart-pub" and edge.get("kind") == "pub-path-dependency"]
        if len(dart_refs) != 1:
            raise AssertionError("supported Dart pub path dependency declaration was not represented")
        py_context = next((context for context in (wider_report.get("lockfiles") or {}).get("contexts", [])
                           if context.get("ecosystem") == "python"), None)
        if py_context is None:
            py_context = next((context for context in (wider_report.get("lockfiles") or {}).get("contexts", [])
                               if str(context.get("manifest_path", "")).endswith("pyproject.toml")), None)
        if py_context is not None:
            outcome = py_context.get("association_state") or py_context.get("state")
            if outcome not in {"unsupported", "unknown", "not_applicable"}:
                raise AssertionError(f"out-of-scope Python lock evidence must be factual unsupported/unknown, got {py_context!r}")
        if wider_assessment.get("unsupported_ecosystem_projects", {}).get("count", 0) < 1:
            raise AssertionError("Python project should be described under unsupported lock-association scope")
        receipt["fixtures"].append({"id": "wider_dart_flat_data_and_unsupported_lock", "files": len(wider_files),
                                    "bytes": wider_bytes, "dart_projects": 2,
                                    "dart_qualified_references": len(dart_refs),
                                    "python_console_entries": python_entries,
                                    "python_unsupported_scope_count": wider_assessment["unsupported_ecosystem_projects"]["count"]})

        flat = temp / "flat-data-only"
        flat.mkdir()
        flat_files = {"readme.txt": "plain fixture text\n", "catalog.xml": "<root><row>1</row></root>\n",
                      "table.csv": "name,value\nx,1\n"}
        flat_bytes = sum(put(flat, name, body) for name, body in flat_files.items())
        flat_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", flat)
        flat_assessment, flat_structure = assessment(flat_report)
        if metric_count(flat_assessment["inventory"]["files"], "flat-data files") != len(flat_files) or metric_count(flat_assessment["inventory"]["bytes"], "flat-data bytes") != flat_bytes:
            raise AssertionError("flat data-only fixture inventory differs from hand count")
        if sum(metric_count(row.get("metric"), "flat parsed projects") for row in flat_structure.get("populations", [])
               if row.get("population") == "parsed_projects") != 0:
            raise AssertionError("data/text/XML-only tree invented parsed projects")
        receipt["fixtures"].append({"id": "flat_data_only_unknown_by_absence", "files": len(flat_files), "bytes": flat_bytes,
                                    "parsed_projects": 0})

        empty_groups = temp / "explicit-empty-groups"
        empty_groups.mkdir()
        empty_files = {
            "npm/package.json": '{"name":"empty-npm","private":true,"workspaces":[]}\n',
            "gradle/settings.gradle": "rootProject.name = 'empty-gradle'\n",
            "cargo/Cargo.toml": "[workspace]\nmembers = []\n",
            "go/go.work": "go 1.23\n",
        }
        empty_bytes = sum(put(empty_groups, name, body) for name, body in empty_files.items())
        empty_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", empty_groups)
        _, empty_structure = assessment(empty_report)
        for eco in ("npm", "gradle", "cargo", "go"):
            candidates = [g for g in empty_structure.get("workspace_groups", []) if g.get("ecosystem") == eco]
            if not candidates or not any(g.get("member_count") == 0 for g in candidates):
                raise AssertionError(f"explicitly declared empty {eco} group should remain a zero-member boundary: {candidates!r}")
            if not any(g.get("membership_coverage", {}).get("status") in {"complete", "partial"}
                       for g in candidates if g.get("member_count") == 0):
                raise AssertionError(f"empty {eco} group should carry factual membership coverage: {candidates!r}")
        receipt["fixtures"].append({"id": "explicit_zero_member_workspace_groups", "files": len(empty_files),
                                    "bytes": empty_bytes, "ecosystems": ["npm", "gradle", "cargo", "go"]})

        poison = temp / "ecosystem-poison-isolation"
        poison.mkdir()
        poison_files = {
            "package.json": '{"name":"valid-root","private":true,"workspaces":["packages/app"]}\n',
            "packages/app/package.json": '{"name":"valid-app","version":"1.0.0"}\n',
            "broken/pom.xml": "<project><modules><module>oops</project>\n",
        }
        poison_bytes = sum(put(poison, name, body) for name, body in poison_files.items())
        poison_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", poison)
        _, poison_structure = assessment(poison_report)
        healthy_group = assert_workspace_group(poison_structure, "npm", 1)
        if healthy_group.get("membership_coverage", {}).get("status") != "complete":
            raise AssertionError(f"malformed Maven declaration should not taint valid npm membership: {healthy_group!r}")
        if metric_count(poison_report["assessment"]["inventory"]["files"], "poison fixture files") != len(poison_files) or metric_count(poison_report["assessment"]["inventory"]["bytes"], "poison fixture bytes") != poison_bytes:
            raise AssertionError("poison isolation fixture exact inventory differs")
        receipt["fixtures"].append({"id": "unrelated_ecosystem_parser_poison_isolation", "files": len(poison_files),
                                    "bytes": poison_bytes, "healthy_npm_membership": "complete"})

        graph = temp / "graph-entrypoints"
        graph.mkdir()
        graph_bytes, graph_files = write_graph(graph, with_entrypoints=True)
        report_a = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "1", "--json", graph)
        native_a, structure_a = assessment(report_a)
        edges = structure_a["dependencies"].get("edges", [])
        edge_pairs = {(edge.get("from"), edge.get("to")) for edge in edges}
        expected_pairs = {("A/A.csproj", "B/B.csproj"), ("B/B.csproj", "A/A.csproj")}
        if not expected_pairs.issubset(edge_pairs):
            raise AssertionError(f"literal resolved unconditional reference cycle missing: {edge_pairs!r}")
        forbidden_targets = {"Missing/Missing.csproj", "C/C.csproj"}
        if any(any(target.endswith(item) for item in forbidden_targets) for _, target in edge_pairs):
            raise AssertionError("missing or conditional references were promoted to definite edges")
        qualified = structure_a["dependencies"].get("qualified_references", [])
        if not qualified:
            raise AssertionError("conditional/missing reference evidence disappeared from qualified references")
        qualified_states = {item.get("state") for item in qualified}
        if not {"conditional", "missing"}.issubset(qualified_states):
            raise AssertionError(f"conflicting conditional and missing targets need distinct qualified states: {qualified!r}")
        # The graph component contains the two projects joined by the explicit cycle.
        comps = structure_a["dependencies"].get("components", [])
        if not any(set(comp.get("projects", [])) >= {"A/A.csproj", "B/B.csproj"} for comp in comps):
            raise AssertionError(f"resolved cycle should remain in one connected component: {comps!r}")
        receipt["fixtures"].append({"id": "qualified_and_cyclic_references", "files": graph_files,
                                    "bytes": graph_bytes, "definite_cycle_edges": sorted(expected_pairs),
                                    "qualified_reference_records": len(qualified)})

        alternate = temp / "graph-alternate-entrypoint"
        alternate.mkdir()
        write_graph(alternate, with_entrypoints=False)
        report_b = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "8", "--json", alternate)
        _, structure_b = assessment(report_b)
        pairs_b = {(edge.get("from"), edge.get("to")) for edge in structure_b["dependencies"].get("edges", [])}
        if pairs_b != edge_pairs:
            raise AssertionError("changing only npm launch script changed the project-reference graph")
        entries_a = structure_a.get("entry_points", [])
        entries_b = structure_b.get("entry_points", [])
        names_a = sorted(entry.get("name") for entry in entries_a)
        names_b = sorted(entry.get("name") for entry in entries_b)
        if names_a != ["serve", "start"] or names_b != ["serve"]:
            raise AssertionError(f"literal npm start/serve declarations should yield two versus one entrypoint observations: {entries_a!r} vs {entries_b!r}")
        receipt["fixtures"].append({"id": "same_graph_different_entrypoints", "same_definite_edges": True,
                                    "left_entrypoints": entries_a, "right_entrypoints": entries_b})

        npm_bin_start = temp / "npm-bin-and-script-start"
        npm_bin_start.mkdir()
        npm_cli_files = {
            "package.json": '{"name":"cli-fixture","version":"1.0.0","bin":{"cli-fixture":"bin/start.js"},"scripts":{"start":"node server.js"}}\n',
            "bin/start.js": "#!/usr/bin/env node\nconsole.log('bin');\n",
            "server.js": "console.log('server');\n",
        }
        npm_cli_bytes = sum(put(npm_bin_start, name, contents) for name, contents in npm_cli_files.items())
        npm_cli_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", npm_bin_start)
        _, npm_cli_structure = assessment(npm_cli_report)
        npm_cli_entries = [entry for entry in npm_cli_structure.get("entry_points", [])
                           if entry.get("evidence_path") == "package.json"]
        targets = {entry.get("target", "") for entry in npm_cli_entries}
        if len(npm_cli_entries) < 2 or not any(target.endswith("bin/start.js") for target in targets) or not any(entry.get("name") == "start" for entry in npm_cli_entries):
            raise AssertionError(f"npm bin.start and scripts.start are two separate declarations and both must survive: {npm_cli_entries!r}")
        if metric_count(npm_cli_report["assessment"]["inventory"]["files"], "npm bin/script files") != len(npm_cli_files) or metric_count(npm_cli_report["assessment"]["inventory"]["bytes"], "npm bin/script bytes") != npm_cli_bytes:
            raise AssertionError("npm bin/script fixture exact inventory differs")
        receipt["fixtures"].append({"id": "npm_bin_and_script_start_entrypoints", "files": len(npm_cli_files),
                                    "bytes": npm_cli_bytes, "entrypoint_observations": npm_cli_entries})

        relationship_edges = temp / "relationship-edge-cases"
        relationship_edges.mkdir()
        relationship_files = {
            "dotnet/App.csproj": '<Project><ItemGroup><ProjectReference Include="App.csproj" /></ItemGroup></Project>\n',
            "maven/a/pom.xml": '<project><modelVersion>4.0.0</modelVersion><groupId>fixture</groupId><artifactId>a</artifactId><version>1</version><dependencies><dependency><groupId>fixture</groupId><artifactId>b</artifactId><version>1</version></dependency></dependencies></project>\n',
            "maven/b/pom.xml": '<project><modelVersion>4.0.0</modelVersion><groupId>fixture</groupId><artifactId>b</artifactId><version>1</version></project>\n',
        }
        relationship_bytes = sum(put(relationship_edges, name, contents) for name, contents in relationship_files.items())
        relationship_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", relationship_edges)
        _, relationship_structure = assessment(relationship_report)
        rel_dependencies = relationship_structure["dependencies"]
        self_edges = [edge for edge in rel_dependencies.get("edges", [])
                      if edge.get("from") == "dotnet/App.csproj" and edge.get("to") == "dotnet/App.csproj"]
        self_qualified = [ref for ref in rel_dependencies.get("qualified_references", [])
                          if ref.get("ecosystem") == "nuget"]
        if not self_edges and not self_qualified:
            raise AssertionError(f"explicit self ProjectReference was silently lost: {rel_dependencies!r}")
        if any(edge.get("ecosystem") == "maven" for edge in rel_dependencies.get("edges", [])):
            raise AssertionError("coordinate-only Maven sibling dependency without declared reactor was promoted to definite local edge")
        maven_qualified = [ref for ref in rel_dependencies.get("qualified_references", []) if ref.get("ecosystem") == "maven"]
        if not maven_qualified:
            raise AssertionError(f"coordinate-only Maven sibling relation should remain qualified evidence: {rel_dependencies!r}")
        if metric_count(relationship_report["assessment"]["inventory"]["files"], "relationship edge files") != len(relationship_files) or metric_count(relationship_report["assessment"]["inventory"]["bytes"], "relationship edge bytes") != relationship_bytes:
            raise AssertionError("relationship edge-case fixture exact inventory differs")
        receipt["fixtures"].append({"id": "self_and_coordinate_only_relationships", "files": len(relationship_files),
                                    "bytes": relationship_bytes, "self_project_reference_retained": True,
                                    "maven_coordinate_only_qualified": True})

        base = temp / "inflation-base"
        expanded = temp / "inflation-expanded"
        base.mkdir()
        expanded.mkdir()
        base_files, base_bytes, _, _ = write_inflated(base, 0)
        large_count = 160
        expanded_files, expanded_bytes, vendor_count, generated_count = write_inflated(expanded, large_count)
        base_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", base)
        expanded_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", expanded)
        base_assessment, base_structure = assessment(base_report)
        expanded_assessment, expanded_structure = assessment(expanded_report)
        if metric_count(expanded_assessment["inventory"]["files"], "expanded inventory") != expanded_files:
            raise AssertionError("vendor/data inflation file count differs from hand count")
        if metric_count(expanded_assessment["inventory"]["bytes"], "expanded bytes") != expanded_bytes:
            raise AssertionError("vendor/data inflation byte count differs from hand count")
        expected_added_bytes = sum(
            len((f"vendor fixture {index:04d} " + "x" * 96 + "\n").encode()) if index < vendor_count
            else len((f"// generated fixture {index - vendor_count:04d}\n"
                      f"internal class Auto{index - vendor_count:04d} {{ }}\n").encode())
            for index in range(large_count))
        if expanded_files - base_files != large_count or expanded_bytes - base_bytes != expected_added_bytes:
            raise AssertionError("hand-counted inflation delta is inconsistent")
        vendored_files = metric_count(expanded_assessment["inventory"]["vendored_files"], "expanded vendored files")
        if vendored_files < vendor_count:
            raise AssertionError("vendor path subset omitted one or more hand-counted vendor fixture files")
        for name in ("parsed_projects", "distinct_roots", "workspace_groups"):
            if population(base_structure, name) != population(expanded_structure, name):
                raise AssertionError(f"adding unrelated vendor data changed {name}")
        receipt["fixtures"].append({"id": "vendor_data_inflation", "base_files": base_files,
                                    "base_bytes": base_bytes, "expanded_files": expanded_files,
                                    "expanded_bytes": expanded_bytes, "added_vendor_data_files": vendor_count,
                                    "added_generated_source_files": generated_count,
                                    "vendored_metric_count": vendored_files})

        bounded = temp / "bounded"
        bounded.mkdir()
        expected_files, expected_bytes, expected_candidates = write_bounded(bounded)
        bounded_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", bounded)
        bounded_assessment, bounded_structure = assessment(bounded_report)
        group = assert_workspace_group(bounded_structure, "npm", 70)
        if len(group.get("members", [])) > 64 or group.get("omitted_members", 0) != 6:
            raise AssertionError(f"70 member exact total should have <=64 paths and 6 omissions: {group!r}")
        if metric_count(bounded_assessment["inventory"]["files"], "bounded files") != expected_files:
            raise AssertionError("bounded evidence fixture's exact file population changed")
        if metric_count(bounded_assessment["inventory"]["bytes"], "bounded bytes") != expected_bytes:
            raise AssertionError("bounded evidence fixture's exact byte population changed")
        if len(bounded_assessment.get("candidate_evidence", [])) > 256:
            raise AssertionError("candidate evidence exceeds its 256 item sample limit")
        if metric_count(bounded_assessment["manifest_candidate_population"], "manifest candidate population") != expected_candidates:
            raise AssertionError(f"candidate population should count exactly {expected_candidates} manifests")
        omitted_candidates = sum(bounded_assessment.get("omitted_candidate_evidence", {}).values())
        if omitted_candidates != expected_candidates - len(bounded_assessment.get("candidate_evidence", [])):
            raise AssertionError("omitted candidate evidence does not partition exact candidate population")
        receipt["fixtures"].append({"id": "bounded_evidence_and_exact_population", "files": expected_files,
                                    "bytes": expected_bytes, "manifest_candidates": expected_candidates,
                                    "npm_member_count": group["member_count"],
                                    "npm_retained_members": len(group.get("members", [])),
                                    "npm_omitted_members": group.get("omitted_members", 0),
                                    "candidate_evidence_retained": len(bounded_assessment.get("candidate_evidence", []))})

        # Compatibility and option validation use a compact deterministic tree.
        limited_report, bytecap_baseline_report = check_cli_negatives(candidate, large)
        root_default = run(candidate, "--json", "--source", "directory", large)
        languages = run(candidate, "analyze", "languages", "--source", "directory", "--json", large)
        if root_default != languages:
            raise AssertionError("legacy default language output changed relative to explicit analyze languages")
        all_plain = run(candidate, "analyze", "all", "--source", "directory", "--json", large)
        all_assessment = run(candidate, "analyze", "all", "--assessment", "--source", "directory", "--json", large)
        if all_plain.get("languages") != all_assessment.get("languages"):
            raise AssertionError("assessment opt-in changed analyze all language output")
        receipt["checks"] = ["legacy_default_equals_languages", "all_assessment_preserves_languages",
                             "invalid_source", "negative_workers", "negative_byte_limit", "byte_limit_qualifies_coverage"]

        # Git source and directory source select exactly the same committed
        # files; only the selected-source identity is permitted to differ.
        git_root = temp / "git-parity"
        git_root.mkdir()
        write_ecosystem_workspaces(git_root)
        git_commit(git_root)
        directory_report = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "1", "--json", git_root)
        committed_report = run(candidate, "analyze", "assessment", "--source", "git", "--rev", "HEAD", "--workers", "8", "--json", git_root)
        if stable_report(directory_report) != stable_report(committed_report):
            raise AssertionError("directory and committed Git reports differ outside selected-source identity/workers")
        receipt["checks"].append("directory_git_semantic_parity")

        # Determinism across worker counts and relocation (excluding source identity).
        first = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "1", "--json", workspaces)
        second = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "8", "--json", workspaces)
        relocated = temp / "relocated-workspaces"
        shutil.copytree(workspaces, relocated)
        third = run(candidate, "analyze", "assessment", "--source", "directory", "--workers", "8", "--json", relocated)
        if stable_report(first) != stable_report(second) or stable_report(first) != stable_report(third):
            raise AssertionError("report changed across workers or equivalent fixture relocation")
        receipt["checks"].extend(["one_eight_worker_determinism", "relocation_determinism"])

        # Static NuGet candidate presence and owner association are separate.
        # Public profile lockfile contexts (not aggregate assessment tallies)
        # carry the per-project evidence.
        nuget = temp / "nuget-static"
        nuget.mkdir()
        nuget_files = {
            "empty/App.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n",
            "empty/Directory.Build.props": "<Project />\n",
            "empty/packages.lock.json": '{"version":1,"dependencies":{"net8.0":{}}}\n',
            "cpm/App.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup><ItemGroup><PackageReference Include="CentralOnly" /></ItemGroup></Project>\n',
            "cpm/Directory.Packages.props": '<Project><PropertyGroup><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup><ItemGroup><PackageVersion Include="CentralOnly" Version="2.0.0" /></ItemGroup></Project>\n',
            "cpm/packages.lock.json": '{"version":1,"dependencies":{"net8.0":{"CentralOnly":{"type":"Direct","requested":"[2.0.0, )","resolved":"2.1.0","contentHash":"fixture"}}}}\n',
            "custom/Custom.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>custom.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "custom/packages.lock.json": '{"version":1,"dependencies":{"net8.0":{}}}\n',
            "custom/custom.lock.json": '{"version":1,"dependencies":{"net8.0":{}}}\n',
            "uncertain/A.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup><Import Project="../../shared/Unknown.props" /><ItemGroup><PackageReference Include="Conditional" Condition="\'$(UseIt)\' == \'true\'" Version="1.0.0" /></ItemGroup></Project>\n',
            "uncertain/B.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>$(LockPath)</NuGetLockFilePath></PropertyGroup></Project>\n',
            "uncertain/packages.lock.json": '{"version":1,"dependencies":{"net8.0":{}}}\n',
        }
        nuget_byte_count = sum(put(nuget, name, contents) for name, contents in nuget_files.items())
        # The import is intentionally selected only as a literal path under the
        # generated parent directory. It remains outside the fixture's source
        # root and is never fetched or evaluated from a user checkout.
        put(temp, "shared/Unknown.props", '<Project><ItemGroup><PackageReference Include="ImportedOnly" Version="1.0.0" /></ItemGroup></Project>\n')
        oracle_expectations = None
        dotnet_path = args.dotnet.resolve() if args.dotnet else ROOT / ".cache/assessment150/dotnet-sdk/dotnet"
        precedence_observations = None
        if dotnet_path.is_file():
            observed = {
                "empty": msbuild_oracle(dotnet_path, nuget / "empty/App.csproj", "empty"),
                "cpm": msbuild_oracle(dotnet_path, nuget / "cpm/App.csproj", "cpm"),
                "custom": msbuild_oracle(dotnet_path, nuget / "custom/Custom.csproj", "custom"),
                "import_condition": msbuild_oracle(dotnet_path, nuget / "uncertain/A.csproj", "import-condition"),
                "expression_path": msbuild_oracle(dotnet_path, nuget / "uncertain/B.csproj", "expression-path"),
            }
            summaries = {name: oracle_summary(result) for name, result in observed.items()}
            if summaries["cpm"]["properties"].get("ManagePackageVersionsCentrally") != "true":
                raise AssertionError(f"MSBuild oracle did not import central package management: {summaries['cpm']!r}")
            if ["CentralOnly", "2.0.0"] not in summaries["cpm"]["package_versions"]:
                raise AssertionError(f"MSBuild oracle did not retain the central version declaration: {summaries['cpm']!r}")
            if "CentralOnly" not in summaries["cpm"]["package_references"]:
                raise AssertionError(f"MSBuild oracle did not retain the version-only direct reference: {summaries['cpm']!r}")
            if summaries["custom"]["properties"]["NuGetLockFilePath"] != "custom.lock.json":
                raise AssertionError(f"MSBuild oracle changed the explicit literal lock path: {summaries['custom']!r}")
            if "ImportedOnly" not in summaries["import_condition"]["package_references"]:
                raise AssertionError(f"controlled parent import was not evaluated by MSBuild: {summaries['import_condition']!r}")
            if "Conditional" in summaries["import_condition"]["package_references"]:
                raise AssertionError("unset UseIt condition unexpectedly activated Conditional")
            if summaries["expression_path"]["properties"]["NuGetLockFilePath"]:
                raise AssertionError(f"unset LockPath should evaluate to empty in the SDK oracle: {summaries['expression_path']!r}")
            oracle_expectations = summaries

            precedence_root = nuget / "precedence"
            project = precedence_root / "src/Nested/App.csproj"
            put(precedence_root, "Directory.Build.props", "<Project><PropertyGroup><NuGetLockFilePath>from-props.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n")
            put(precedence_root, "Directory.Build.targets", "<Project><PropertyGroup><NuGetLockFilePath>from-targets.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n")
            put(precedence_root, "src/Nested/App.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>from-project.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n")
            precedence = msbuild_properties_oracle(dotnet_path, project, "precedence-project-wins-targets")
            props_value = precedence.get("Properties", {}).get("NuGetLockFilePath")
            if props_value != "from-targets.lock.json":
                raise AssertionError(f"targets property should follow props and project assignments: {precedence!r}")
            put(precedence_root, "first.props", "<Project><PropertyGroup><NuGetLockFilePath>from-first.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n")
            put(precedence_root, "second.props", "<Project><PropertyGroup><NuGetLockFilePath>from-second.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n")
            put(precedence_root, "Directory.Build.targets", "<Project />\n")
            put(precedence_root, "src/Nested/App.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n")
            for first, second, expected, label in [
                    ("first.props", "second.props", "from-second.lock.json", "import-first-second"),
                    ("second.props", "first.props", "from-first.lock.json", "import-second-first")]:
                put(precedence_root, "Directory.Build.props",
                    f'<Project><Import Project="{first}" /><Import Project="{second}" /></Project>\n')
                value = msbuild_properties_oracle(dotnet_path, project, label).get("Properties", {}).get("NuGetLockFilePath")
                if value != expected:
                    raise AssertionError(f"MSBuild import order expected {expected}, got {value!r}")
            for final_value, label in [("", "empty-final-shadow"), ("$(UndefinedLockPath)", "dynamic-final-shadow")]:
                put(precedence_root, "Directory.Build.props", "<Project />\n")
                put(precedence_root, "Directory.Build.targets", "<Project />\n")
                put(precedence_root, "src/Nested/App.csproj",
                    f"<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>earlier.lock.json</NuGetLockFilePath>"
                    f"<NuGetLockFilePath>{final_value}</NuGetLockFilePath></PropertyGroup></Project>\n")
                value = msbuild_properties_oracle(dotnet_path, project, label).get("Properties", {}).get("NuGetLockFilePath", "")
                if value:
                    raise AssertionError(f"empty/dynamic later assignment should shadow the literal, got {value!r}")
            # Re-query the nested relative literal and record only the basename
            # and whether the property itself was normalized.
            put(precedence_root, "Directory.Build.props", "<Project />\n")
            put(precedence_root, "src/Nested/App.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>nested.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n")
            nested = msbuild_properties_oracle(dotnet_path, project, "nested-relative-path")
            nested_value = nested.get("Properties", {}).get("NuGetLockFilePath", "")
            if nested_value != "nested.lock.json":
                raise AssertionError(f"MSBuild property query should preserve relative literal, got {nested_value!r}")
            precedence_observations = {
                "directory_props_project_targets": "targets assignment wins",
                "ordered_imports": ["last imported assignment wins", "reversing order reverses selected assignment"],
                "later_empty_or_dynamic": "shadows earlier literal with empty effective value",
                "nested_relative_literal": "property remains relative; restore resolution not queried",
            }
            anchored = temp / "msbuild-anchored-import"
            anchored_project = anchored / "build/src/App.csproj"
            winner = ('<Project><ItemGroup><PackageReference Include="PathWinner" Version="1.0" />'
                      '<PackageVersion Include="CentralWinner" Version="3.0" /></ItemGroup></Project>\n')
            decoy = ('<Project><ItemGroup><PackageReference Include="PathDecoy" Version="9.0" />'
                     '<PackageVersion Include="CentralDecoy" Version="9.0" /></ItemGroup></Project>\n')
            put(anchored, "build/common.props", winner)
            put(anchored, "build/build/common.props", decoy)
            put(anchored, "build/src/App.csproj", '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n')
            path_import_observations = {}
            for spelling, label in [("$(MSBuildThisFileDirectory)common.props", "anchored"),
                                    ("$(MSBuildThisFileDirectory)./common.props", "dot_slash"),
                                    ("$(MSBuildThisFileDirectory).\\common.props", "backslash")]:
                put(anchored, "build/Directory.Build.props", f'<Project><Import Project="{spelling}" /></Project>\n')
                result = oracle_summary(msbuild_oracle(dotnet_path, anchored_project, f"import-path-{label}"))
                package_refs = result["package_references"]
                package_versions = result["package_versions"]
                if package_refs != ["PathWinner"] or package_versions != [["CentralWinner", "3.0"]]:
                    raise AssertionError(f"anchored import {label} selected unexpected items: {result!r}")
                path_import_observations[label] = {"package_references": package_refs,
                                                   "package_versions": package_versions,
                                                   "decoy_directory_not_selected": "PathDecoy" not in package_refs}

            shared_oracle = temp / "msbuild-default-imports"
            put(shared_oracle, "Directory.Build.props",
                '<Project><PropertyGroup><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup>'
                '<ItemGroup><PackageReference Include="SharedPropsItem" Version="1.0" /></ItemGroup></Project>\n')
            put(shared_oracle, "Directory.Build.targets",
                '<Project><ItemGroup><PackageReference Include="SharedTargetsItem" Version="2.0" /></ItemGroup></Project>\n')
            sdk_import_observations = {}
            for sdk in ("Microsoft.NET.Sdk", "Microsoft.NET.Sdk.Web", "Microsoft.NET.Sdk.Razor", "Microsoft.NET.Sdk.Worker"):
                label = sdk.removeprefix("Microsoft.NET.Sdk").lower() or "base"
                project_path = shared_oracle / label / "App.csproj"
                put(shared_oracle, f"{label}/App.csproj",
                    f'<Project Sdk="{sdk}"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n')
                summary = oracle_summary(msbuild_oracle(dotnet_path, project_path, f"sdk-shared-import-{label}"))
                if summary["properties"]["ManagePackageVersionsCentrally"] != "true" or summary["package_references"] != ["SharedPropsItem", "SharedTargetsItem"]:
                    raise AssertionError(f"SDK form {sdk} did not import both shared inputs: {summary!r}")
                sdk_import_observations[sdk] = {"manage_centrally": summary["properties"]["ManagePackageVersionsCentrally"],
                                                "package_references": summary["package_references"]}
            put(shared_oracle, "bare/App.csproj",
                '<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n')
            bare_summary = oracle_summary(msbuild_oracle(dotnet_path, shared_oracle / "bare/App.csproj", "bare-shared-import-control"))
            if bare_summary["package_references"] or bare_summary["properties"]["ManagePackageVersionsCentrally"]:
                raise AssertionError(f"bare Project must not import Directory.Build.* implicitly: {bare_summary!r}")
            sdk_import_observations["bare_project"] = {"manage_centrally": "",
                                                        "package_references": []}
            for label, control in (("targets-false", "<ImportDirectoryBuildTargets>false</ImportDirectoryBuildTargets>"),
                                   ("targets-dynamic", "<ImportDirectoryBuildTargets>$(UndefinedControl)</ImportDirectoryBuildTargets>")):
                put(shared_oracle, f"{label}/App.csproj",
                    f'<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework>{control}</PropertyGroup></Project>\n')
                summary = oracle_summary(msbuild_oracle(dotnet_path, shared_oracle / f"{label}/App.csproj", f"shared-import-{label}"))
                expected_refs = ["SharedPropsItem"] if label == "targets-false" else ["SharedPropsItem", "SharedTargetsItem"]
                if summary["package_references"] != expected_refs:
                    raise AssertionError(f"MSBuild {label} import control selected wrong items: {summary!r}")
                sdk_import_observations[label] = {"manage_centrally": "true",
                                                  "package_references": expected_refs}
            sdk_version = subprocess.run([str(dotnet_path), "--version"], capture_output=True,
                                         check=True, text=True, timeout=30).stdout.strip()
            receipt["oracle"] = {"dotnet": str(dotnet_path), "status": "passed_controlled_msbuild_queries",
                                 "version": sdk_version,
                                 "method": "dotnet msbuild -getProperty/-getItem only; no restore, build, or public checkout"}
            expected_path = ROOT / "tests/assessment_v150/oracle_expected.json"
            expected = json.loads(expected_path.read_text())
            if expected.get("oracle_version") != sdk_version or expected.get("observations") != oracle_expectations:
                raise AssertionError("controlled MSBuild observations differ from reviewed oracle_expected.json")
            if expected.get("precedence_observations") != precedence_observations:
                raise AssertionError("controlled MSBuild precedence observations differ from reviewed oracle_expected.json")
            if expected.get("anchored_import_observations") != path_import_observations:
                raise AssertionError("controlled MSBuild import path observations differ from reviewed oracle_expected.json")
            if expected.get("sdk_import_observations") != sdk_import_observations:
                raise AssertionError("controlled SDK shared-import observations differ from reviewed oracle_expected.json")
        else:
            receipt["oracle"] = {"dotnet": None, "status": "unavailable_without_isolated_sdk"}
        nuget_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", nuget)
        nuget_assessment, _ = assessment(nuget_report)
        contexts = [context for context in (nuget_report.get("lockfiles") or {}).get("contexts", [])
                    if context.get("ecosystem") == "nuget"]
        by_manifest = {context.get("manifest_path"): context for context in contexts}
        for manifest in ["empty/App.csproj", "cpm/App.csproj", "custom/Custom.csproj", "uncertain/A.csproj", "uncertain/B.csproj"]:
            if manifest not in by_manifest:
                raise AssertionError(f"NuGet fixture project missing from lockfile contexts: {manifest!r}; {contexts!r}")
        for manifest in ["empty/App.csproj", "cpm/App.csproj", "custom/Custom.csproj"]:
            evidence = by_manifest[manifest].get("nuget_evidence")
            if not isinstance(evidence, dict):
                raise AssertionError(f"NuGet candidate evidence missing for {manifest}: {by_manifest[manifest]!r}")
            if evidence.get("presence_state") not in {"present", "observed", "candidate_present"}:
                raise AssertionError(f"candidate lockfile presence was not separated and retained for {manifest}: {evidence!r}")
            if evidence.get("candidate_count", 0) < 1 or not evidence.get("candidate_paths"):
                raise AssertionError(f"candidate lockfile count/path missing for {manifest}: {evidence!r}")
        # A literal custom path is known as a declaration, but standard-file
        # presence must not be mistaken for ownership by the project.
        custom_evidence = by_manifest["custom/Custom.csproj"]["nuget_evidence"]
        if custom_evidence.get("ownership_state") in {"owned", "observed", "associated"}:
            raise AssertionError(f"custom literal lock path was guessed from standard candidate presence: {custom_evidence!r}")
        uncertain_evidence = []
        for manifest in ["uncertain/A.csproj", "uncertain/B.csproj"]:
            evidence = by_manifest[manifest].get("nuget_evidence")
            if not isinstance(evidence, dict) or evidence.get("presence_state") != "observed":
                raise AssertionError(f"lockfile candidate presence should remain observed for {manifest}: {evidence!r}")
            if evidence.get("ownership_state") != "indeterminate":
                raise AssertionError(f"out-of-tree import, conditional input, expression path, or colliding owners must qualify {manifest}: {evidence!r}")
            if not evidence.get("ownership_reasons"):
                raise AssertionError(f"qualified NuGet ownership must explain its static boundary for {manifest}: {evidence!r}")
            uncertain_evidence.append({"manifest": manifest, "presence_state": evidence["presence_state"],
                                       "ownership_state": evidence["ownership_state"],
                                       "ownership_reasons": evidence["ownership_reasons"]})
        nuget_actual_files = [path for path in nuget.rglob("*") if path.is_file()]
        nuget_actual_bytes = sum(path.stat().st_size for path in nuget_actual_files)
        if metric_count(nuget_assessment["inventory"]["files"], "NuGet fixture files") != len(nuget_actual_files) or metric_count(nuget_assessment["inventory"]["bytes"], "NuGet fixture bytes") != nuget_actual_bytes:
            raise AssertionError("NuGet fixture exact file/byte totals differ")
        receipt["fixtures"].append({"id": "nuget_presence_vs_ownership", "files": len(nuget_actual_files),
                                    "bytes": nuget_actual_bytes, "contexts": len(contexts),
                                    "candidate_presence_checked": ["empty/App.csproj", "cpm/App.csproj", "custom/Custom.csproj"],
                                    "uncertain_ownership_checked": uncertain_evidence,
                                    "custom_ownership_state": custom_evidence.get("ownership_state"),
                                    "sdk_oracle": "passed" if oracle_expectations else "unavailable"})

        unique_locks = temp / "nuget-custom-ownership"
        unique_locks.mkdir()
        unique_files = {}
        for index in range(20):
            directory = f"unique/P{index:02d}"
            unique_files[f"{directory}/P{index:02d}.csproj"] = (
                '<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework>'
                '<NuGetLockFilePath>custom.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n')
            unique_files[f"{directory}/custom.lock.json"] = '{"version":1,"dependencies":{"net10.0":{}}}\n'
        for project_name in ("A", "B"):
            unique_files[f"collision/{project_name}.csproj"] = (
                '<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework>'
                '<NuGetLockFilePath>shared.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n')
        unique_files["collision/shared.lock.json"] = '{"version":1,"dependencies":{"net10.0":{}}}\n'
        unique_bytes = sum(put(unique_locks, name, contents) for name, contents in unique_files.items())
        unique_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", unique_locks)
        unique_contexts = [context for context in (unique_report.get("lockfiles") or {}).get("contexts", [])
                           if context.get("ecosystem") == "nuget"]
        unique_by_manifest = {context.get("manifest_path"): context for context in unique_contexts}
        for index in range(20):
            manifest = f"unique/P{index:02d}/P{index:02d}.csproj"
            context = unique_by_manifest.get(manifest)
            evidence = context.get("nuget_evidence", {}) if context else {}
            if evidence.get("ownership_state") not in {"observed", "owned", "associated"}:
                raise AssertionError(f"unique explicit custom lock should have determinate ownership: {manifest}: {evidence!r}")
        for manifest in ("collision/A.csproj", "collision/B.csproj"):
            evidence = unique_by_manifest.get(manifest, {}).get("nuget_evidence", {})
            if evidence.get("ownership_state") != "indeterminate":
                raise AssertionError(f"two projects sharing one custom lock path must remain indeterminate: {manifest}: {evidence!r}")
        _, unique_structure = assessment(unique_report)
        if metric_count(unique_report["assessment"]["inventory"]["files"], "unique custom-lock fixture files") != len(unique_files):
            raise AssertionError("unique custom-lock fixture exact file count differs")
        receipt["fixtures"].append({"id": "nuget_unique_and_colliding_custom_paths", "files": len(unique_files),
                                    "bytes": unique_bytes, "unique_determinate_projects": 20,
                                    "shared_path_indeterminate_projects": 2,
                                    "nuget_context_count": len(unique_contexts)})

        lock_collision = temp / "nuget_custom_default_collision"
        lock_collision.mkdir()
        collision_files = {
            "custom/Custom.csproj": '<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>../default/packages.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "default/Default.csproj": '<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n',
            "default/packages.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
        }
        collision_bytes = sum(put(lock_collision, name, contents) for name, contents in collision_files.items())
        collision_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", lock_collision)
        collision_contexts = [context for context in (collision_report.get("lockfiles") or {}).get("contexts", [])
                              if context.get("ecosystem") == "nuget"]
        collision_by_path = {context.get("manifest_path"): context for context in collision_contexts}
        for manifest in ("custom/Custom.csproj", "default/Default.csproj"):
            evidence = collision_by_path.get(manifest, {}).get("nuget_evidence", {})
            if evidence.get("ownership_state") != "indeterminate":
                raise AssertionError(f"custom/default lock-path collision should leave both projects indeterminate: {manifest}: {evidence!r}")
        receipt["fixtures"].append({"id": "nuget_custom_default_lock_collision", "files": len(collision_files),
                                    "bytes": collision_bytes, "indeterminate_owners": 2})

        foreign_props = temp / "nuget_foreign_namespace_props"
        foreign_props.mkdir()
        foreign_files = {
            "Directory.Build.props": '<Project xmlns="urn:foreign"><PropertyGroup><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup><ItemGroup><PackageVersion Include="Fake" Version="8.0" /></ItemGroup></Project>\n',
            "App.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup><ItemGroup><PackageReference Include="Fake" /></ItemGroup></Project>\n',
            "packages.lock.json": '{"version":1,"dependencies":{"net10.0":{"Fake":{"type":"Direct","requested":"[1.0, )","resolved":"1.0","contentHash":"fixture"}}}}\n',
        }
        foreign_bytes = sum(put(foreign_props, name, contents) for name, contents in foreign_files.items())
        foreign_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", foreign_props)
        foreign_context = next((context for context in (foreign_report.get("lockfiles") or {}).get("contexts", [])
                                if context.get("manifest_path") == "App.csproj"), None)
        foreign_evidence = foreign_context.get("nuget_evidence", {}) if foreign_context else {}
        if foreign_evidence.get("ownership_state") != "indeterminate" or not foreign_evidence.get("ownership_reasons"):
            raise AssertionError(f"foreign-namespace shared MSBuild input should qualify NuGet ownership: {foreign_evidence!r}")
        receipt["fixtures"].append({"id": "foreign_namespace_shared_msbuild_input", "files": len(foreign_files),
                                    "bytes": foreign_bytes, "ownership_state": foreign_evidence.get("ownership_state")})

        bare_shared = temp / "bare_project_ignores_implicit_shared_imports"
        bare_shared.mkdir()
        bare_files = {
            "Directory.Build.props": '<Project><PropertyGroup><NuGetLockFilePath>missing-shared.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "Directory.Build.targets": '<Project><PropertyGroup><NuGetLockFilePath>also-missing.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "App.csproj": '<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup><ItemGroup><PackageReference Include="BarePackage" Version="1.0" /></ItemGroup></Project>\n',
            "packages.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
        }
        bare_bytes = sum(put(bare_shared, name, contents) for name, contents in bare_files.items())
        bare_report = run(candidate, "analyze", "assessment", "--source", "directory", "--json", bare_shared)
        bare_context = next((context for context in (bare_report.get("lockfiles") or {}).get("contexts", [])
                             if context.get("manifest_path") == "App.csproj"), None)
        bare_evidence = bare_context.get("nuget_evidence", {}) if bare_context else {}
        if "packages.lock.json" not in bare_evidence.get("candidate_paths", []) or bare_evidence.get("ownership_state") not in {"observed", "owned", "associated"}:
            raise AssertionError(f"bare Project should use default lock candidate and ignore implicit shared props/targets: {bare_evidence!r}")
        receipt["fixtures"].append({"id": "bare_project_ignores_implicit_shared_imports", "files": len(bare_files),
                                    "bytes": bare_bytes, "default_lock_candidate": "packages.lock.json",
                                    "ownership_state": bare_evidence.get("ownership_state")})

        msbuild_controls = temp / "msbuild_target_import_controls"
        msbuild_controls.mkdir()
        control_files = {
            "default/Directory.Build.props": '<Project><PropertyGroup><NuGetLockFilePath>props.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "default/Directory.Build.targets": '<Project><PropertyGroup><NuGetLockFilePath>targets.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "default/Default.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup><ItemGroup><PackageReference Include="A" Version="1.0" /></ItemGroup></Project>\n',
            "default/props.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
            "default/targets.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
            "disabled/Directory.Build.props": '<Project><PropertyGroup><NuGetLockFilePath>props.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "disabled/Directory.Build.targets": '<Project><PropertyGroup><NuGetLockFilePath>targets.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "disabled/Disabled.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework><ImportDirectoryBuildTargets>false</ImportDirectoryBuildTargets></PropertyGroup><ItemGroup><PackageReference Include="B" Version="1.0" /></ItemGroup></Project>\n',
            "disabled/props.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
            "disabled/targets.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
            "dynamic/Directory.Build.props": '<Project><PropertyGroup><NuGetLockFilePath>props.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "dynamic/Directory.Build.targets": '<Project><PropertyGroup><NuGetLockFilePath>targets.lock.json</NuGetLockFilePath></PropertyGroup></Project>\n',
            "dynamic/Dynamic.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework><ImportDirectoryBuildTargets>$(UndefinedControl)</ImportDirectoryBuildTargets></PropertyGroup><ItemGroup><PackageReference Include="C" Version="1.0" /></ItemGroup></Project>\n',
            "dynamic/props.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
            "dynamic/targets.lock.json": '{"version":1,"dependencies":{"net10.0":{}}}\n',
        }
        control_bytes = sum(put(msbuild_controls, name, contents) for name, contents in control_files.items())
        control_reports = {}
        control_contexts = {}
        for name in ("default", "disabled", "dynamic"):
            subroot = msbuild_controls / name
            control_reports[name] = run(candidate, "analyze", "assessment", "--source", "directory", "--json", subroot)
            context = next((context for context in (control_reports[name].get("lockfiles") or {}).get("contexts", [])
                            if context.get("ecosystem") == "nuget"), None)
            control_contexts[name] = context.get("nuget_evidence", {}) if context else {}
        default_control = control_contexts["default"]
        if default_control.get("ownership_state") not in {"observed", "owned", "associated"} or "targets.lock.json" not in default_control.get("candidate_paths", []):
            raise AssertionError(f"default SDK imports should select the ordered targets lock path: {default_control!r}")
        for name in ("disabled", "dynamic"):
            evidence = control_contexts[name]
            if evidence.get("ownership_state") != "indeterminate" or not evidence.get("ownership_reasons"):
                raise AssertionError(f"{name} ImportDirectoryBuildTargets control should qualify static ownership: {evidence!r}")
        receipt["fixtures"].append({"id": "nuget_import_directory_build_targets_controls", "files": len(control_files),
                                    "bytes": control_bytes, "default_selected": "targets.lock.json",
                                    "disabled_state": control_contexts["disabled"].get("ownership_state"),
                                    "dynamic_state": control_contexts["dynamic"].get("ownership_state")})

        # Saved reports must pass both user-facing offline schemas and the
        # native compare loader. Corrupt each newly-accounted axis separately.
        saved_reports = [large_report, dotnet_report, report, report_a, report_b,
                         base_report, expanded_report, bounded_report, nuget_report,
                         unique_report, directory_report, committed_report,
                         wider_report, flat_report, empty_report, poison_report, same_root_report,
                         limited_report, bytecap_baseline_report, many_solution_report,
                         collision_report, foreign_report, npm_cli_report, relationship_report,
                         bare_report, *control_reports.values()]
        check_saved_reports(candidate, saved_reports, temp)
        for saved in saved_reports:
            check_empty_entrypoint_coverage(saved)
        check_corruption_rejected(candidate, {
            "population": large_report,
            "workspace_group": report,
            "dependency": report_a,
            "entry_point": report_a,
            "lock_evidence": nuget_report,
        }, temp)
        receipt["checks"].extend(["exported_profile_schema_accepts_saved_reports",
                                 "exported_assessment_schema_accepts_saved_reports",
                                 "native_saved_compare_loader_accepts_all_reports",
                                 "corrupted_accounting_and_evidence_rows_rejected",
                                 "no_fictional_complete_empty_entrypoint_coverage"])

        # The current native loader must continue to accept a saved v1.4-era
        # assessment object that predates the structural section.
        legacy = json.loads(json.dumps(large_report))
        legacy["schema_version"] = "1.9.0"
        legacy["assessment"]["version"] = "1.0.0"
        legacy["assessment"].pop("structure", None)
        legacy_path = temp / "saved-v14-assessment.json"
        legacy_path.write_text(json.dumps(legacy, sort_keys=True) + "\n")
        legacy_compare = subprocess.run([str(candidate), "compare", str(legacy_path), str(legacy_path), "--json"],
                                        capture_output=True, timeout=45)
        if legacy_compare.returncode:
            raise AssertionError(f"native loader rejected pre-structure saved assessment: {legacy_compare.stderr[-2000:]!r}")
        receipt["checks"].append("native_loader_accepts_pre_structure_assessment")

        # Every report must remain an observation-only object.
        for value in saved_reports:
            reject_policy_language(value)

    receipt["passed"] = True
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    print(f"Passed: {len(receipt['fixtures'])} synthetic fixture families; "
          f"{len(receipt['checks'])} runtime/compatibility checks; receipt: {args.output}")


if __name__ == "__main__":
    main()
