#!/usr/bin/env python3
"""Verify an already-built runtime image offline and record exact results."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="dircue:0.3.0")
    parser.add_argument("--version", default="0.3.0")
    parser.add_argument("--reference", default="dircue-linguist:9.7.0")
    parser.add_argument("--corpus-volume", required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path("tests/release/results/docker-smoke.json"))
    args = parser.parse_args()
    image = json.loads(subprocess.check_output(["docker", "image", "inspect", args.image]))[0]
    checks = []
    common = ["docker", "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
              "--security-opt", "no-new-privileges", "--memory", "256m", "--cpus", "2"]

    def run(extra, expected_status=0):
        command = [*common, *extra]
        result = subprocess.run(command, capture_output=True, text=True, timeout=120)
        checks.append({"command": command, "exit_code": result.returncode,
                       "stdout": result.stdout, "stderr": result.stderr})
        if result.returncode != expected_status:
            raise RuntimeError(f"unexpected status: {checks[-1]}")
        return result.stdout, result.stderr

    with tempfile.TemporaryDirectory(prefix="dircue-smoke-") as directory:
        root = Path(directory)
        (root / "main.go").write_text("package main\nfunc main() {}\n")
        (root / "go.mod").write_text("module example.test/smoke\n\ngo 1.26\n")
        assert run([args.image, "--version"])[0] == f"dircue {args.version}\n"
        modern = json.loads(run(["-v", f"{root}:/repo:ro", args.image, "analyze", "all", "--json", "/repo"])[0])
        assert modern["languages"][0]["name"] == "Go"
        assert any(value["name"] == "go" for value in modern["ecosystems"])
        metrics = json.loads(run(["-v", f"{root}:/repo:ro", args.image, "analyze", "metrics", "--json", "/repo"])[0])
        assert metrics["schema_version"] == "1.1.0"
        assert metrics["metrics"]["status"] == "complete"
        assert metrics["metrics"]["totals"]["code"] == 2
        assert metrics["metrics"]["totals"]["files"] == 1
        projects = json.loads(run(["-v", f"{root}:/repo:ro", args.image, "analyze", "projects", "--json", "/repo"])[0])
        assert projects["schema_version"] == "1.2.0"
        assert projects["projects"]["status"] == "complete"
        assert projects["projects"]["projects"][0]["id"] == "go.mod"
        legacy = json.loads(run(["-v", f"{args.corpus_volume}:/corpus:ro", args.image, "-bj", "/corpus/cobra"])[0])
        reference = json.loads(subprocess.check_output(["docker", "run", "--rm", "--network", "none",
            "-v", f"{args.corpus_volume}:/corpus:ro", args.reference, "github-linguist", "-bj", "/corpus/cobra"], timeout=120))
        assert legacy == reference
        stdout, stderr = run([args.image, "--json", "/missing"], expected_status=1)
        assert not stdout and stderr
        container = subprocess.check_output(["docker", "create", args.image], text=True).strip()
        try:
            subprocess.run(["docker", "cp", f"{container}:/usr/local/bin/dircue", str(root / "image-binary")], check=True)
            image_hash = hashlib.sha256((root / "image-binary").read_bytes()).hexdigest()
        finally:
            subprocess.run(["docker", "rm", container], check=True, stdout=subprocess.DEVNULL)
        assert image_hash == hashlib.sha256(args.candidate.read_bytes()).hexdigest()
        for check in checks:
            check["command"] = [part.replace(directory, "<temporary-fixture>") for part in check["command"]]
    report = {"image_id": image["Id"], "architecture": image["Architecture"],
              "user": image["Config"]["User"], "binary_sha256": image_hash,
              "matches_cross_compiled_binary": True, "checks": checks}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(args.output)


if __name__ == "__main__":
    main()
