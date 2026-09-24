#!/usr/bin/env python3
"""Create deterministic, fully written profiling fixtures; never execute their code."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import random
import shutil
import subprocess
import time

MIB = 1024 * 1024
PROFILES = {
    "smoke": {"jobs": 8, "item_bytes": 16*1024, "java_bytes": 8*1024,
              "properties_bytes": 1024, "context_bytes": 1024, "assets": 2,
              "asset_bytes": 16*1024, "log_bytes": 2*MIB+1, "projects": 24,
              "reference_depth": 12, "directory_depth": 4, "count_files": 257, "history_commits": 4},
    "acceptance": {"jobs": 2048, "item_bytes": 512*1024, "java_bytes": 384*1024,
                   "properties_bytes": 16*1024, "context_bytes": 16*1024, "assets": 96,
                   "asset_bytes": 2*MIB, "log_bytes": 1100*MIB+1, "projects": 2048,
                   "reference_depth": 256, "directory_depth": 24, "count_files": 100000, "history_commits": 64},
}
SCENARIOS = ["etl-pipeline", "xml-log", "dotnet-graph", "boundaries", "tree-count"]
GIT_ENV = {**os.environ, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
           "GIT_AUTHOR_NAME": "dircue fixture", "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
           "GIT_COMMITTER_NAME": "dircue fixture", "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
           "GIT_AUTHOR_DATE": "2000-01-01T00:00:00+0000", "GIT_COMMITTER_DATE": "2000-01-01T00:00:00+0000"}


def git(root, *args):
    result = subprocess.run(["git", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false",
                             "-c", "gc.auto=0", "-C", str(root), *args], env=GIT_ENV,
                            text=True, capture_output=True, timeout=1800)
    if result.returncode:
        raise RuntimeError(f"git {args}: {result.stderr}")
    return result.stdout.strip()


def commit(root, message):
    if not (root / ".git").exists():
        git(root, "init", "-q", "--initial-branch=main")
    git(root, "add", "--all")
    git(root, "commit", "-q", "-m", message)
    return git(root, "rev-parse", "HEAD")


class Writer:
    def __init__(self, root):
        self.root = root
        self.files = {}

    def write(self, name, content):
        target = self.root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("wb") as stream:
            stream.write(content)
        self.record(name, len(content), hashlib.sha256(content).hexdigest(),
                    hashlib.sha1(f"blob {len(content)}\0".encode()+content).hexdigest())

    def record(self, name, size, sha, blob_oid):
        info = (self.root / name).stat()
        self.files[name] = {"path": name, "bytes": size, "sha256": sha,
                            "allocated_bytes": info.st_blocks * 512, "git_blob_oid": blob_oid}

    def document(self, name, size, kind):
        """Write valid envelope plus varied records, sequentially, to an exact byte size."""
        target = self.root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        rng = random.Random(int.from_bytes(hashlib.sha256(name.encode()).digest(), "big"))
        if kind == "xml":
            header, footer = b'<?xml version="1.0" encoding="UTF-8"?>\n<synthetic-records>\n', b'</synthetic-records>\n'
        elif kind == "java":
            klass = target.stem
            header = f'package synthetic.jobs;\n// Synthetic generated job; not produced by an ETL pipeline tool.\npublic class {klass} {{\n static final String[] DATA = {{\n'.encode()
            footer = b' };\n}\n'
        elif kind == "csharp":
            header = b"namespace Synthetic;\npublic class HistoryData {\n static readonly string[] Data = {\n"
            footer = b" };\n}\n"
        elif kind == "text":
            header, footer = b"# Synthetic context key-value data\n", b"\n"
        elif kind == "asset":
            header, footer = b"\x89PNG\r\n\x1a\nSYNTHETIC-ASSET-NOT-A-VALID-IMAGE\x00", b"\x00END\n"
        else:
            raise ValueError(kind)
        if size < len(header) + len(footer):
            raise ValueError(f"document too small: {name}")
        digest = hashlib.sha256()
        blob_digest = hashlib.sha1(f"blob {size}\0".encode())
        with target.open("wb") as stream:
            def emit(content):
                stream.write(content)
                digest.update(content)
                blob_digest.update(content)
            emit(header)
            remaining = size - len(header) - len(footer)
            sequence = 0
            while remaining:
                # A batch avoids per-record syscalls; entropy is deliberate and reported,
                # not repeated zero padding that unrealistically disappears in Git.
                limit = min(remaining, 128*1024)
                if kind == "asset":
                    block = rng.randbytes(limit)
                else:
                    records = []
                    used = 0
                    while limit - used >= 256:
                        payload = rng.randbytes(min(1536, (limit-used-128)//2)).hex().encode()
                        if kind == "xml":
                            record = b' <record index="%d" component="tMap" value="' % sequence + payload + b'"/>\n'
                        elif kind in ("java", "csharp"):
                            record = b'  "' + payload + b'", // parameter %d\n' % sequence
                        else:
                            record = b'context.parameter_%d=' % sequence + payload + b'\n'
                        records.append(record)
                        used += len(record)
                        sequence += 1
                    # Spaces occur only between complete records; XML/Java stay well formed.
                    block = b"".join(records) + b" " * (limit-used)
                emit(block)
                remaining -= len(block)
            emit(footer)
        self.record(name, size, digest.hexdigest(), blob_digest.hexdigest())


def materialize_flat(source, target, files):
    target.mkdir(parents=True)
    for name in files:
        destination = target / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        # Attribute revisions must never mutate another view's inode.
        if name == ".gitattributes":
            shutil.copyfile(source / name, destination)
        else:
            os.link(source / name, destination)


def inventory(root):
    logical = allocated = count = 0
    for current, _, names in os.walk(root):
        for name in names:
            info = (Path(current) / name).stat()
            logical += info.st_size
            allocated += info.st_blocks * 512
            count += 1
    return {"file_count": count, "logical_bytes": logical, "allocated_bytes": allocated}


def generate(args):
    config = PROFILES[args.profile]
    selected = args.scenario or SCENARIOS
    root = args.output.resolve()
    if root.exists() and any(root.iterdir()):
        raise ValueError("output must be absent or empty; existing fixtures are never overwritten")
    root.mkdir(parents=True, exist_ok=True)
    estimates = {
        "etl-pipeline": config["jobs"] * sum(config[k] for k in ["item_bytes", "java_bytes", "properties_bytes", "context_bytes"]) + config["assets"]*config["asset_bytes"],
        "xml-log": config["log_bytes"], "dotnet-graph": config["projects"]*8192,
        "boundaries": 5*MIB, "tree-count": config["count_files"]*8192,
    }
    required = 2*sum(estimates[name] for name in selected) + 128*MIB
    free = shutil.disk_usage(root).free
    if free < required:
        raise ValueError(f"need conservative {required} free bytes for sources, Git and inode overhead; have {free}")
    manifest = {"schema_version": "1.0.0", "profile": args.profile, "parameters": config,
                "started_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "generator_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                "git_version": subprocess.check_output(["git", "--version"], text=True).strip(),
                "python_version": __import__("sys").version,
                "disk_free_before": free, "fixtures": [],
                "method": "Sequential real writes, no sparse files; deterministic path-seeded PRNG records. Flat views hardlink identical payloads; attributes copied. Allocated bytes per view double-count shared inodes; unique allocation reported separately.",
                "fidelity": "Synthetic sizes, formats and layout only. Not an actual ETL-pipeline export, compilable application or log-transfer sample."}
    for scenario in selected:
        repo = root / scenario / "git"
        repo.mkdir(parents=True)
        writer = Writer(repo)
        variants = []
        expectations = []
        if scenario == "etl-pipeline":
            writer.write(".gitattributes", b"src/generated/** linguist-generated=true\n*.javajet linguist-language=Java\n")
            writer.write("pom.xml", b'<project><modelVersion>4.0.0</modelVersion><groupId>synthetic</groupId><artifactId>jobs</artifactId><version>1</version></project>\n')
            writer.write("src/main/java/Application.java", b'package synthetic;\npublic class Application { public static void main(String[] args) { System.out.println("synthetic"); } }\n')
            writer.write("templates/tSynthetic_java.javajet", b'<%@ jet package="synthetic" class="Template" %>\npublic class <%= "Synthetic" %> {}\n')
            writer.write("bin/run.sh", b'#!/bin/sh\njava -jar synthetic-job.jar "$@"\n')
            for index in range(config["jobs"]):
                job = f"Job{index:05d}"
                folder = f"group{index%32:02d}"
                writer.document(f"process/{folder}/{job}_0.1.item", config["item_bytes"], "xml")
                writer.document(f"process/{folder}/{job}_0.1.properties", config["properties_bytes"], "xml")
                writer.document(f"src/generated/java/synthetic/jobs/{job}.java", config["java_bytes"], "java")
                writer.document(f"contexts/{folder}/{job}/Default.properties", config["context_bytes"], "text")
            for index in range(config["assets"]):
                writer.document(f"assets/asset{index:04d}.png", config["asset_bytes"], "asset")
            expectations = [{"path": "src/main/java/Application.java", "language": "Java"},
                            {"path": "src/generated/java/synthetic/jobs/Job00000.java", "language": None}]
        elif scenario == "xml-log":
            writer.write("src/Program.cs", b'using System;\nclass Program { static void Main() { Console.WriteLine("synthetic"); } }\n')
            writer.document("logs/events.xml", config["log_bytes"], "xml")
            writer.write(".gitattributes", b"# XML logs use Linguist default data-language exclusion.\n")
            baseline = commit(repo, "default XML exclusion")
            files = dict(writer.files)
            materialize_flat(repo, root / scenario / "flat-default", files)
            variants.append({"name": "default", "revision": baseline, "flat": f"{scenario}/flat-default",
                             "files": list(files.values()), "assertions": [{"path": "logs/events.xml", "language": None}]})
            writer.write(".gitattributes", b"logs/events.xml linguist-detectable=true\n")
            expectations = [{"path": "logs/events.xml", "language": "XML", "bytes": config["log_bytes"]}]
        elif scenario == "dotnet-graph":
            count, depth = config["projects"], config["reference_depth"]
            paths = [f"src/wide/p{i:05d}/Project{i:05d}.csproj" for i in range(count-depth)]
            paths += ["src/deep/" + "/".join(f"level{j:02d}" for j in range(1+(i%config["directory_depth"]))) + f"/p{i:05d}/Project{count-depth+i:05d}.csproj" for i in range(depth)]
            edges = []
            for index, name in enumerate(paths):
                # A long chain and broad fan-out, with repeated references and a cycle.
                targets = ([index-1] if index else []) + ([0] if index > 1 else [])
                if index == 0:
                    targets = [count-1]
                references = []
                for target in targets:
                    relative = os.path.relpath(paths[target], str(Path(name).parent)).replace(os.sep, "/")
                    references.append(f'    <ProjectReference Include="{relative}" />')
                    edges.append({"from": name, "to": paths[target]})
                content = '<Project Sdk="Microsoft.NET.Sdk">\n  <PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup>\n  <ItemGroup>\n' + '\n'.join(references) + '\n    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />\n  </ItemGroup>\n</Project>\n'
                writer.write(name, content.encode())
                writer.write(str(Path(name).with_name("Program.cs")), f'namespace Synthetic.P{index};\npublic class Program {{ public static string Name => "Project{index}"; }}\n'.encode())
            writer.document("src/shared/HistoryData.cs", 2*MIB if args.profile == "acceptance" else 64*1024, "csharp")
            writer.write("Directory.Build.props", b'<Project><PropertyGroup><Nullable>enable</Nullable></PropertyGroup></Project>\n')
            writer.write("Directory.Packages.props", b'<Project><ItemGroup><PackageVersion Include="Newtonsoft.Json" Version="13.0.3" /></ItemGroup></Project>\n')
            writer.write("global.json", b'{"sdk":{"version":"8.0.100","rollForward":"latestFeature"}}\n')
            (root / scenario / "graph.json").write_text(json.dumps({"projects": paths, "edges": edges, "contains_cycle": True}, indent=2)+"\n")
            expectations = [{"path": "src/wide/p00000/Program.cs", "language": "C#"}]
        elif scenario == "boundaries":
            for size in [0, 1, 128*1024-1, 128*1024, 128*1024+1, MIB-1, MIB, MIB+1]:
                name = f"src/Size{size}.java"
                if size < 200:
                    writer.write(name, b" "*size)
                else:
                    writer.document(name, size, "java")
            expectations = [{"path": "src/Size1048577.java", "language": "Java", "bytes": MIB+1}]
        elif scenario == "tree-count":
            for index in range(config["count_files"]):
                writer.write(f"src/bucket{index//1000:03d}/f{index:06d}.go", f"package p // {index}\n".encode())
        if scenario == "etl-pipeline":
            excluded_head = commit(repo, "exclude synthetic generated Java")
            excluded_files = dict(writer.files)
            materialize_flat(repo, root / scenario / "flat-generated-excluded", excluded_files)
            variants.append({"name": "generated-excluded", "revision": excluded_head,
                             "flat": f"{scenario}/flat-generated-excluded", "files": list(excluded_files.values()),
                             "assertions": expectations})
            writer.write(".gitattributes", b"src/generated/** linguist-generated=false\n*.javajet linguist-language=Java\n")
            expectations = [{"path": "src/main/java/Application.java", "language": "Java"},
                            {"path": "src/generated/java/synthetic/jobs/Job00000.java", "language": "Java",
                             "bytes": config["java_bytes"]}]
        head = commit(repo, f"synthetic {scenario}")
        history = None
        if scenario == "dotnet-graph":
            initial_tree = git(repo, "rev-parse", f"{head}^{{tree}}")
            history_path = repo / "src/shared/HistoryData.cs"
            original = history_path.read_bytes()
            for revision in range(config["history_commits"]):
                history_path.write_bytes(f"// Synthetic history revision {revision}\n".encode()+original)
                commit(repo, f"synthetic history {revision}")
            history_path.write_bytes(original)
            head = commit(repo, "restore identical acceptance tree")
            if git(repo, "rev-parse", f"{head}^{{tree}}") != initial_tree:
                raise RuntimeError("history generation changed final source tree")
            before_pack = inventory(repo / ".git")
            git(repo, "repack", "-adf", "--depth=50", "--window=50")
            git(repo, "prune-packed")
            pack_indexes = sorted((repo / ".git/objects/pack").glob("*.idx"))
            descriptions = [git(repo, "verify-pack", "-v", str(index)) for index in pack_indexes]
            deltas = [line for text in descriptions for line in text.splitlines() if len(line.split()) == 7]
            history = {"commits": int(git(repo, "rev-list", "--count", "HEAD")),
                       "final_tree": initial_tree, "identical_tree_before_and_after": True,
                       "before_pack_storage": before_pack, "delta_objects": len(deltas),
                       "max_delta_chain_depth": max((int(line.split()[5]) for line in deltas), default=0),
                       "pack_files": [{"name": path.name, "bytes": path.stat().st_size,
                                        "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
                                       for path in sorted((repo / ".git/objects/pack").iterdir())]}
        flat_name = {"xml-log": "flat-detectable", "etl-pipeline": "flat-generated-included"}.get(scenario, "flat")
        materialize_flat(repo, root / scenario / flat_name, writer.files)
        variants.append({"name": {"xml-log": "detectable", "etl-pipeline": "generated-included"}.get(scenario, "default"),
                         "revision": head, "flat": f"{scenario}/{flat_name}",
                         "files": list(writer.files.values()), "assertions": expectations})
        for variant in variants:
            variant["file_count"] = len(variant["files"])
            variant["logical_bytes"] = sum(file["bytes"] for file in variant["files"])
            variant["allocated_source_bytes"] = sum(file["allocated_bytes"] for file in variant["files"])
        fixture = {"name": scenario, "git": f"{scenario}/git", "variants": variants,
                   "git_storage": inventory(repo / ".git"), "git_head": head}
        if history:
            fixture["packed_history"] = history
        if scenario == "tree-count":
            fixture["tree_size_limits"] = [config["count_files"]-1, config["count_files"], config["count_files"]+1]
        manifest["fixtures"].append(fixture)
        (root / "manifest.json").write_text(json.dumps(manifest, indent=2)+"\n")
        print(json.dumps({"generated": scenario, "logical_bytes": variants[-1]["logical_bytes"], "files": len(writer.files)}), flush=True)
    seen = set()
    allocated = 0
    for current, _, names in os.walk(root):
        for name in names:
            info = (Path(current)/name).stat()
            key = (info.st_dev, info.st_ino)
            if key not in seen:
                seen.add(key)
                allocated += info.st_blocks * 512
    manifest.update({"unique_allocated_bytes": allocated, "disk_free_after": shutil.disk_usage(root).free,
                     "finished_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())})
    (root / "manifest.json").write_text(json.dumps(manifest, indent=2)+"\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--profile", choices=PROFILES, default="smoke")
    parser.add_argument("--scenario", choices=SCENARIOS, action="append")
    args = parser.parse_args()
    generate(args)

if __name__ == "__main__":
    main()
