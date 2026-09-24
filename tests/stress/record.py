#!/usr/bin/env python3
"""Audit a complete acceptance run and render a concise, reproducible evidence report."""
import argparse
import gzip
import hashlib
import io
import json
import math
import os
from pathlib import Path
import shutil
import statistics
import tarfile

# These exact source identities retain pre-rename report replay. Result keys are
# historical identifiers; new benchmark commands use the current CLI name.
HISTORICAL_HARNESS_SHA256 = "53511bf72475bd5430fa8c09797dcb32c1f22f1b4cc6c89ac4220cb754afc5fe"
HISTORICAL_GENERATOR_SHA256 = "1ccdc2094ff0357b728dfa35877ab73b522bb7c6d8b40e2d5561944d742fedf1"

EXPECTED_CASES = {
    "etl-pipeline-generated-excluded", "etl-pipeline-generated-included", "xml-log-default", "xml-log-detectable",
    "dotnet-graph-default", "boundaries-default", "tree-count-default", "tree-count-default-limit99999",
    "tree-count-default-limit100000", "tree-count-default-limit100001",
    "etl-pipeline-packed-generated-excluded", "etl-pipeline-packed-generated-included",
    "xml-log-packed-default", "xml-log-packed-detectable",
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--fixture-manifest", required=True, type=Path)
    parser.add_argument("--output", type=Path, default=Path("tests/stress/results"))
    args = parser.parse_args()
    report = json.loads(args.report.read_text())
    manifest_bytes = args.fixture_manifest.read_bytes()
    manifest = json.loads(manifest_bytes)
    if report.get("profile") != "acceptance" or not report.get("finished_at_utc") or not report.get("passed") or not report.get("performance_passed"):
        parser.error("only complete, passing acceptance-profile timing evidence can be recorded")
    if hashlib.sha256(manifest_bytes).hexdigest() != report["fixture_manifest_sha256"]:
        parser.error("fixture manifest hash mismatch")
    directory = Path(__file__).resolve().parent
    if report["harness_sha256"] not in {HISTORICAL_HARNESS_SHA256, hashlib.sha256((directory/"compare.py").read_bytes()).hexdigest()}:
        parser.error("report does not identify the current or pinned pre-rename comparison harness")
    if manifest["generator_sha256"] not in {HISTORICAL_GENERATOR_SHA256, hashlib.sha256((directory/"generate.py").read_bytes()).hexdigest()}:
        parser.error("manifest does not identify the current or pinned pre-rename fixture generator")
    if hashlib.sha256((directory/"pack_views.py").read_bytes()).hexdigest() != manifest.get("packed_views", {}).get("script_sha256"):
        parser.error("manifest does not identify the current packed-view generator")
    if report.get("candidate_sha256_after") != report["candidate_sha256"]:
        parser.error("candidate identity was not stable through the run")
    receipt = report.get("build_receipt")
    if not receipt or receipt["candidate_sha256"] != report["candidate_sha256"]:
        parser.error("matching production build receipt must be embedded in the report")
    cases = report["cases"]
    if len(cases) != len(EXPECTED_CASES) or {case["name"] for case in cases} != EXPECTED_CASES:
        parser.error("all fourteen acceptance cases are required exactly once")
    runs = report["methodology"]["runs"]
    if runs < 20 or report["methodology"]["warmup"] < 3:
        parser.error("at least 20 measured runs and three warmups are required")
    artifact_dir = args.report.parent / (args.report.stem+"-details")
    retained_artifacts = set()
    for case in cases:
        if not case["match"] or case.get("error"):
            parser.error(f"unresolved mismatch: {case['name']}")
        outputs = []
        for tool in ["linguist_git", "auragaze_git", "auragaze_flat"]:
            result = case["results"][tool]
            if Path(result["artifact"]).name != result["artifact"]:
                parser.error("artifact paths must be basenames")
            retained_artifacts.add(result["artifact"])
            artifact = (artifact_dir/result["artifact"]).read_bytes()
            if hashlib.sha256(artifact).hexdigest() != result["sha256"]:
                parser.error("raw correctness artifact hash mismatch")
            data = json.loads(artifact)
            if data["exit_code"] or data["timed_out"]:
                parser.error("raw correctness invocation failed")
            parsed = json.loads(data["stdout"])
            outputs.append({language: {**details, "files": sorted(details["files"])} for language, details in parsed.items()})
            samples = case["samples"][tool]
            values = [sample["seconds"] for sample in samples]
            if len(values) != runs or any(not math.isfinite(value) or value <= 0 for value in values):
                parser.error("invalid timing samples")
            if any(not isinstance(sample["max_rss_kib"], int) or sample["max_rss_kib"] <= 0 for sample in samples):
                parser.error("invalid child RSS measurements")
            for sample in samples:
                for field in ["user_seconds", "system_seconds"]:
                    value = sample.get(field)
                    if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value < 0:
                        parser.error(f"invalid child CPU measurement: {field}")
                if not isinstance(sample.get("stderr_bytes"), int) or sample["stderr_bytes"] < 0:
                    parser.error("invalid stderr byte count")
            timing = case["timings"][tool]
            recomputed = {"median_seconds": statistics.median(values),
                          "p95_seconds": sorted(values)[math.ceil(.95*len(values))-1],
                          "max_seconds": max(values), "min_seconds": min(values),
                          "coefficient_of_variation": statistics.pstdev(values)/statistics.mean(values)}
            if timing.get("runs") != len(samples):
                parser.error("recorded run count does not match raw samples")
            for field, actual in recomputed.items():
                recorded = timing.get(field)
                if not isinstance(recorded, (int, float)) or not math.isfinite(recorded) or not math.isclose(actual, recorded, rel_tol=1e-12, abs_tol=1e-15):
                    parser.error(f"recorded {field} does not match raw samples")
            if max(sample["max_rss_kib"] for sample in samples) != timing["peak_rss_kib"]:
                parser.error("recorded peak RSS does not match raw samples")
        if outputs[0] != outputs[1] or outputs[0] != outputs[2]:
            parser.error("actual retained candidate and reference language outputs differ")
        for extended in case.get("extended", {}).values():
            name = extended["artifact"]
            if Path(name).name != name:
                parser.error("extended artifact paths must be basenames")
            data = json.loads((artifact_dir/name).read_bytes())
            if data["exit_code"] or data["timed_out"]:
                parser.error("extended correctness invocation failed")
            actual = json.loads(data["stdout"])
            languages = {item["name"]: {"size": item["bytes"], "files": sorted(item.get("files", []))} for item in actual["languages"]}
            expected = {name: {"size": details["size"], "files": details["files"]} for name, details in outputs[0].items()}
            if languages != expected:
                parser.error("extended artifact language accounting differs from the reference")
            retained_artifacts.add(name)
        if case["timings"]["auragaze_git"]["median_seconds"] > case["timings"]["linguist_git"]["median_seconds"]:
            parser.error(f"candidate Git median is slower: {case['name']}")
    evidence_path = Path(os.path.relpath(args.output, directory.parents[1])).as_posix()
    methodology_path = Path(os.path.relpath(directory / "README.md", args.output)).as_posix()
    lines = ["# Synthetic scale acceptance evidence", "",
             "All fourteen scenarios matched actual Ruby Linguist 9.7.0 in both Git and Git-free modes. Each candidate Git median was faster in this measured environment.", "",
             f"Measured {report['started_at_utc']} to {report['finished_at_utc']}, with {runs} recorded runs per tool after {report['methodology']['warmup']} warmups. Timing order rotates across Ruby Git, auragaze Git, and auragaze flat. Inputs reside on the same Docker volume and are mounted read-only; networking is disabled.", "",
             "The three cutoff cases return an empty result after the tree-size check. Their timings measure that policy check, not classification of 100,000 files. The limit of 100,001 permits the complete 100,000-file scan and is listed as a full traversal.", "",
             "| Case | Work | Counted bytes | Ruby Git median (ms) | Auragaze Git (ms) | Auragaze flat (ms) | Git speedup |", "|---|---|---:|---:|---:|---:|---:|"]
    for case in cases:
        timings = case["timings"]
        medians = [timings[tool]["median_seconds"]*1000 for tool in ["linguist_git", "auragaze_git", "auragaze_flat"]]
        cutoff = case["name"].startswith("tree-count-") and case["counted_files"] == 0
        work = "Cutoff; empty" if cutoff else "Full traversal"
        lines.append(f"| {case['name']} | {work} | {case['language_bytes']:,} | {medians[0]:,.2f} | {medians[1]:,.2f} | {medians[2]:,.2f} | {medians[0]/medians[1]:.2f}× |")
    lines += ["", "Full traversal means visiting the directory/tree entries and applying the bounded classification policy. It does not mean reading or validating every XML byte. Huge XML ratios reflect bounded prefix classification, default XML exclusion where applicable, and avoiding full-blob materialization; they are not full-XML processing throughput.", "", "Empirical p95 uses the nearest-rank sample; CV is population standard deviation divided by mean across these runs. Neither establishes rare-tail behavior or repeat-window stability.", "",
              "| Case | Ruby Git p95 (ms) | Auragaze Git p95 (ms) | Auragaze flat p95 (ms) | Ruby CV | Git CV | Flat CV |",
              "|---|---:|---:|---:|---:|---:|---:|"]
    variability = []
    for case in cases:
        timings = [case["timings"][tool] for tool in ["linguist_git", "auragaze_git", "auragaze_flat"]]
        cells = [f"{timing['p95_seconds']*1000:,.2f}" for timing in timings]+[f"{timing['coefficient_of_variation']:.3f}" for timing in timings]
        lines.append("| "+case["name"]+" | "+" | ".join(cells)+" |")
        for tool, timing in zip(["Ruby Git", "Auragaze Git", "Auragaze flat"], timings):
            if timing["coefficient_of_variation"] >= .10:
                variability.append(f"{case['name']} ({tool}): CV {timing['coefficient_of_variation']:.3f}, maximum {timing['max_seconds']:.3f} s, median {timing['median_seconds']:.3f} s")
    if variability:
        lines += ["", "Observed variability deserves attention: "+"; ".join(variability)+". Every sample, including these slow observations, is retained. The measurements do not identify their cause; no outlier was removed and this single window does not prove stability across repeated windows."]
    lines += ["", "| Case | Ruby Git median CPU (s) | Auragaze Git CPU (s) | Auragaze flat CPU (s) |", "|---|---:|---:|---:|"]
    for case in cases:
        cpu = [statistics.median(sample["user_seconds"]+sample["system_seconds"] for sample in case["samples"][tool])
               for tool in ["linguist_git", "auragaze_git", "auragaze_flat"]]
        lines.append(f"| {case['name']} | {cpu[0]:.3f} | {cpu[1]:.3f} | {cpu[2]:.3f} |")
    lines += ["", "CPU is user plus system time reported for the measured child, distinct from elapsed wall time. GNU time reports these CPU counters to hundredths of a second, so short-process CPU ratios would have limited precision."]
    lines += ["", "| Case | Ruby Git peak RSS (MiB) | Auragaze Git (MiB) | Auragaze flat (MiB) |", "|---|---:|---:|---:|"]
    for case in cases:
        rss = [case["timings"][tool]["peak_rss_kib"]/1024 for tool in ["linguist_git", "auragaze_git", "auragaze_flat"]]
        lines.append(f"| {case['name']} | {rss[0]:.1f} | {rss[1]:.1f} | {rss[2]:.1f} |")
    lines += ["", f"Unique allocated fixture file blocks: {manifest['unique_allocated_bytes']:,} bytes. Payloads are fully written and hash-verified; hardlinked flat views share their storage. Checkout bytes, Git-history storage and compressed pack sizes are distinct fields in the raw report.", "",
              f"Candidate SHA-256: `{report['candidate_sha256']}`. Production commit: `{receipt['source_commit']}`. Fixture generator SHA-256: `{manifest['generator_sha256']}`. The [raw report](comparison.json) contains every timing sample and the build receipt.", "",
              "GNU time measures target-child CPU/RSS; wall time includes the same launcher overhead for every tool. Caches are warm, and fixture verification reads all unique payloads before profiling. Prefix reads are valid: logical source size is not physical bytes read during a scan. Twenty samples do not establish rare tails.", "",
              f"These are explicitly synthetic layouts and formats, not verified ETL-pipeline exports or log-transfer samples. The .NET graph is never built or restored. The results establish bounded compatibility and performance evidence for these inputs and this environment, not a guarantee for other repositories, histories, machines or future Linguist versions. See the [methodology and reproduction commands]({methodology_path}).", ""]
    args.output.mkdir(parents=True, exist_ok=True)
    if args.report.resolve() != (args.output/"comparison.json").resolve():
        shutil.copyfile(args.report, args.output/"comparison.json")
    with (args.output/"fixture-manifest.json.gz").open("wb") as output:
        with gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=0) as compressed:
            compressed.write(manifest_bytes)
    with (args.output/"correctness-artifacts.tar.gz").open("wb") as output:
        with gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w|", format=tarfile.USTAR_FORMAT) as archive:
                for name in sorted(retained_artifacts):
                    data = (artifact_dir/name).read_bytes()
                    entry = tarfile.TarInfo(name)
                    entry.size, entry.mode, entry.mtime = len(data), 0o644, 0
                    entry.uid = entry.gid = 0
                    entry.uname = entry.gname = ""
                    archive.addfile(entry, io.BytesIO(data))
    archive_hashes = {name: hashlib.sha256((args.output/name).read_bytes()).hexdigest()
                      for name in ["fixture-manifest.json.gz", "correctness-artifacts.tar.gz"]}
    (args.output/"archive-sha256.json").write_text(json.dumps(archive_hashes, indent=2)+"\n")
    lines += ["", "The [fixture manifest](fixture-manifest.json.gz) retains every generated path, byte size, SHA-256 and Git blob ID. The [correctness outputs](correctness-artifacts.tar.gz) retain stdout, stderr, exit status and diagnostic resources, including `analyze all`. Both archives have deterministic metadata; [archive hashes](archive-sha256.json) permit integrity checks.", "", "To extract and independently re-run this audit from the repository root:", "", "```sh", "mkdir -p .cache/stress-reaudit/comparison-details", f"cp {evidence_path}/comparison.json .cache/stress-reaudit/comparison.json", f"gzip -dc {evidence_path}/fixture-manifest.json.gz > .cache/stress-reaudit/manifest.json", f"tar -xzf {evidence_path}/correctness-artifacts.tar.gz -C .cache/stress-reaudit/comparison-details", "python3 tests/stress/record.py --report .cache/stress-reaudit/comparison.json --fixture-manifest .cache/stress-reaudit/manifest.json --output .cache/stress-reaudit/verified", "```", ""]
    summary = "\n".join(lines)
    (args.output/"README.md").write_text(summary if report["harness_sha256"] == HISTORICAL_HARNESS_SHA256 else summary.replace("Auragaze", "Dircue").replace("auragaze", "dircue"))
    print(args.output/"README.md")

if __name__ == "__main__":
    main()
