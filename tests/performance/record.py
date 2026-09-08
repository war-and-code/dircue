#!/usr/bin/env python3
"""Validate completed raw evidence and generate its reviewable Markdown summary."""
import argparse
import gzip
import hashlib
import io
import json
import math
from pathlib import Path
import shutil
import statistics
import tarfile


# Historical result keys remain stable across the project rename.
TOOLS = ("linguist", "auragaze")
HISTORICAL_HARNESS_SHA256 = "c58d3a7c743fe33d6923aeafa0fcd9eacf260951b2801b1948b8efa62dca0bb2"


def sha256(content):
    return hashlib.sha256(content).hexdigest()


def normalized(value):
    if not isinstance(value, dict):
        raise ValueError("top-level output must be an object")
    return {
        language: {**details, "files": sorted(details.get("files", []))}
        for language, details in value.items()
    }


def write_artifact_archive(output, artifacts):
    """Write only validated raw output files with normalized, deterministic metadata."""
    destination = output / "correctness-artifacts.tar.gz"
    temporary = destination.with_suffix(destination.suffix + ".tmp")
    with temporary.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, compresslevel=9, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for name, content in sorted(artifacts.items()):
                    info = tarfile.TarInfo(f"comparison-details/{name}")
                    info.size = len(content)
                    info.mode = 0o644
                    info.mtime = 0
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    archive.addfile(info, io.BytesIO(content))
    temporary.replace(destination)
    return destination, sha256(destination.read_bytes())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path("tests/performance/results"))
    args = parser.parse_args()

    report = json.loads(args.report.read_text())
    receipt = json.loads(args.receipt.read_text())
    directory = Path(__file__).resolve().parent
    manifest = json.loads((directory / "corpus.json").read_text())
    expected_projects = {project["name"]: project for project in manifest["projects"]}
    if len(expected_projects) != len(manifest["projects"]):
        parser.error("the pinned corpus manifest contains duplicate project names")
    if not report.get("finished_at_utc") or not report.get("passed") or not report.get("performance_passed"):
        parser.error("only a completed correctness- and performance-passing run can be recorded")
    if receipt.get("candidate_sha256") != report.get("candidate_sha256"):
        parser.error("build receipt identifies a different candidate")
    if report.get("candidate_sha256_after") != report.get("candidate_sha256"):
        parser.error("candidate identity was not stable through the run")
    if report.get("harness_sha256") not in {HISTORICAL_HARNESS_SHA256, sha256((directory / "compare.py").read_bytes())}:
        parser.error("report does not identify the current or pinned pre-rename comparison harness")

    methodology = report.get("methodology", {})
    runs = methodology.get("runs", 0)
    if runs < 20 or methodology.get("warmup", 0) < 3:
        parser.error("acceptance evidence requires at least 20 runs per tool and three warmups")
    projects = report.get("projects", [])
    actual_names = [project.get("name") for project in projects]
    if len(actual_names) != len(expected_projects) or len(actual_names) != len(set(actual_names)) or set(actual_names) != set(expected_projects):
        parser.error("acceptance evidence requires every pinned corpus project exactly once")

    artifact_dir = args.report.parent / (args.report.stem + "-details")
    retained_artifacts = {}
    for project in projects:
        expected = expected_projects[project["name"]]
        for field in ("url", "tag", "ref", "language"):
            if project.get(field) != expected[field]:
                parser.error(f"corpus provenance differs for {project['name']}/{field}")
        if project.get("commit") != expected.get("commit"):
            parser.error(f"resolved commit differs from pinned peeled commit for {project['name']}")
        differences = project.get("differences", {})
        if not project.get("match") or differences.get("file_differences") or differences.get("language_totals"):
            parser.error(f"unresolved correctness difference in {project['name']}")

        outputs = []
        for tool in TOOLS:
            artifact_name = f"{project['name']}-{tool}.json"
            try:
                content = (artifact_dir / artifact_name).read_bytes()
            except OSError as error:
                parser.error(f"cannot read correctness output for {project['name']}/{tool}: {error}")
            expected_hash = project.get("output_sha256", {}).get(tool)
            if sha256(content) != expected_hash:
                parser.error(f"correctness output hash mismatch for {project['name']}/{tool}")
            try:
                outputs.append(normalized(json.loads(content)))
            except (ValueError, TypeError) as error:
                parser.error(f"invalid correctness output for {project['name']}/{tool}: {error}")
            retained_artifacts[artifact_name] = content

            samples = project.get("samples", {}).get(tool, [])
            values = [sample.get("seconds") for sample in samples]
            if len(values) != runs or any(not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value <= 0 for value in values):
                parser.error(f"invalid samples for {project['name']}/{tool}")
            if any(type(sample.get("max_rss_kib")) is not int or sample["max_rss_kib"] <= 0 for sample in samples):
                parser.error(f"invalid RSS samples for {project['name']}/{tool}")
            for sample in samples:
                for field in ("user_seconds", "system_seconds"):
                    value = sample.get(field)
                    if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value < 0:
                        parser.error(f"invalid CPU sample for {project['name']}/{tool}")
            timing = project.get("timings", {}).get(tool, {})
            if timing.get("runs") != runs:
                parser.error(f"summary run count differs for {project['name']}/{tool}")
            if not math.isclose(statistics.median(values), timing.get("median_seconds", math.nan), rel_tol=1e-12):
                parser.error(f"median does not match raw samples for {project['name']}/{tool}")
            if max(sample["max_rss_kib"] for sample in samples) != timing.get("peak_rss_kib"):
                parser.error(f"peak RSS does not match raw samples for {project['name']}/{tool}")
            ordered = sorted(values)
            recomputed = {
                "min_seconds": ordered[0], "max_seconds": ordered[-1],
                "mean_seconds": statistics.mean(values),
                "coefficient_of_variation": statistics.pstdev(values) / statistics.mean(values),
                "operations_per_second": 1 / statistics.median(values),
                "mean_cpu_percent": statistics.mean(
                    100 * (sample["user_seconds"] + sample["system_seconds"]) / sample["seconds"]
                    for sample in samples),
                "language_bytes_per_second": sum(details["size"] for details in outputs[-1].values()) / statistics.median(values),
            }
            for field, quantile in (("p95_seconds", .95), ("p99_seconds", .99),
                                    ("p999_seconds", .999), ("p9999_seconds", .9999)):
                recomputed[field] = ordered[math.ceil(quantile * runs) - 1]
            for field, expected_value in recomputed.items():
                actual = timing.get(field)
                if (not isinstance(actual, (int, float)) or isinstance(actual, bool)
                        or not math.isfinite(actual)
                        or not math.isclose(expected_value, actual, rel_tol=1e-12, abs_tol=1e-12)):
                    parser.error(f"{field} does not match raw samples for {project['name']}/{tool}")
        if outputs[0] != outputs[1]:
            parser.error(f"retained candidate and reference outputs differ for {project['name']}")
        if project["timings"]["auragaze"]["median_seconds"] > project["timings"]["linguist"]["median_seconds"]:
            parser.error(f"candidate slower on {project['name']}")

    args.output.mkdir(parents=True, exist_ok=True)
    archive, archive_hash = write_artifact_archive(args.output, retained_artifacts)

    rows = []
    memory_rows = []
    cpu_rows = []
    variable_cells = []
    for project in projects:
        auragaze, linguist = (project["timings"][tool] for tool in ("auragaze", "linguist"))
        speedup = linguist["median_seconds"] / auragaze["median_seconds"]
        rows.append(f"| {project['name']} {project['tag']} | {project['tracked_files']:,} | {linguist['median_seconds'] * 1000:,.1f} | {auragaze['median_seconds'] * 1000:,.1f} | {speedup:.2f}× |")
        memory_rows.append(f"| {project['name']} | {linguist['p95_seconds'] * 1000:,.1f} | {auragaze['p95_seconds'] * 1000:,.1f} | {linguist['peak_rss_kib'] / 1024:.1f} | {auragaze['peak_rss_kib'] / 1024:.1f} |")
        cpu = {tool: statistics.median(sample["user_seconds"] + sample["system_seconds"]
                                      for sample in project["samples"][tool]) for tool in TOOLS}
        cpu_rows.append(f"| {project['name']} | {cpu['linguist']:.3f} | {cpu['auragaze']:.3f} |")
        for tool in TOOLS:
            variation = project["timings"][tool]["coefficient_of_variation"]
            if variation > .10:
                variable_cells.append(f"{project['name']}/{tool} ({variation:.1%} CV)")
    variability_note = ("Runtime variability exceeded 10% coefficient of variation for "
                        + ", ".join(variable_cells)
                        + ". All observations, including outliers, are retained. Interpret medians alongside p95 and the raw maximums.\n") if variable_cells else ""
    summary = f"""# v0.1 performance evidence

All **{len(projects)} pinned repositories matched {report['reference_version']} exactly** for language names, byte totals, percentage strings, and file sets. Every Auragaze median was faster. This report is generated from completed raw evidence by `tests/performance/record.py`, which independently checks timing summaries, CPU usage, throughput, and peak RSS against all recorded samples and reopens every correctness artifact.

Measured {report['started_at_utc']} to {report['finished_at_utc']}; {runs} runs per tool per repository after {methodology['warmup']} warmups. Both complete CLI processes ran in the same Linux {report['environment']['machine']} container with the same read-only corpus on a Docker volume. Host: {receipt['host_hardware'].get('model', receipt['host_hardware'].get('machdep.cpu.brand_string', 'see build receipt'))}. Other project builds and tests were paused. The host scheduler and unrelated system activity were not controlled.

| Project / release | Tracked files | Linguist median (ms) | Auragaze median (ms) | Speedup |
|---|---:|---:|---:|---:|
""" + "\n".join(rows) + """

| Project | Linguist p95 (ms) | Auragaze p95 (ms) | Linguist peak RSS (MiB) | Auragaze peak RSS (MiB) |
|---|---:|---:|---:|---:|
""" + "\n".join(memory_rows) + """

| Project | Linguist median CPU (s) | Auragaze median CPU (s) |
|---|---:|---:|
""" + "\n".join(cpu_rows) + f"""

CPU is aggregate child-process user plus system time, distinct from elapsed time. Parallel execution can consume multiple CPU seconds per wall-clock second. GNU time records these counters to hundredths of a second, limiting precision for short scans. Wall time, CPU cost, and peak memory are separate comparisons.

{variability_note}
Candidate SHA-256: `{report['candidate_sha256']}`. Production source commit: `{receipt['source_commit']}`. The build receipt confirms no production changes from that commit and records compiler/module details and source hashes. Release archives identify their executable hashes separately, allowing comparison with this candidate.

[Raw timings and environment](comparison.json) include every recorded run, source commits, output hashes, CPU time, RSS, variation, throughput, and quantiles. [Build receipt](build-receipt.json) identifies the binary and reference image. [Validated raw correctness outputs](correctness-artifacts.tar.gz) are retained with deterministic archive metadata (SHA-256 `{archive_hash}`); inspect them with `tar -xzf correctness-artifacts.tar.gz`.

These are warm-cache measurements on these specific releases. They establish the measured compatibility and performance result, not universal equivalence or a performance guarantee for another repository, platform, filesystem, or Linguist version. Twenty runs do not estimate rare-tail latency: p99 and higher fields in the JSON are conservative observed order statistics. Framework/ecosystem profiling and cold-cache behavior are outside this benchmark.
"""
    for source, name in ((args.report, "comparison.json"), (args.receipt, "build-receipt.json")):
        if source.resolve() != (args.output / name).resolve():
            shutil.copyfile(source, args.output / name)
    (args.output / "README.md").write_text(summary if report["harness_sha256"] == HISTORICAL_HARNESS_SHA256 else summary.replace("Auragaze", "Dircue").replace("auragaze", "dircue"))
    print(args.output / "README.md")
    print(f"{archive} sha256={archive_hash}")


if __name__ == "__main__":
    main()
