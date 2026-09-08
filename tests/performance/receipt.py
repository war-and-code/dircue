#!/usr/bin/env python3
"""Capture local source and binary identity before a benchmark; never builds."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import subprocess
from datetime import datetime, timezone


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--image", default="dircue-linguist:9.7.0")
    parser.add_argument("--output", type=Path, default=Path(".cache/performance/build-receipt.json"))
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    candidate = args.candidate.resolve()
    def output(command):
        return subprocess.check_output(command, cwd=root, text=True).strip()
    files = output(["git", "ls-files", "*.go", "go.mod", "go.sum", "*.gz", "*.bin", "*.db"]).splitlines()
    production = ["go.mod", "go.sum", "main.go", "internal", "pkg", "third_party/go-enry"]
    changes = output(["git", "diff", "HEAD", "--", *production])
    untracked = output(["git", "ls-files", "--others", "--exclude-standard", "--", *production])
    if changes or untracked:
        parser.error("commit production source before recording release benchmark identity")
    build_info = output(["go", "version", "-m", str(candidate)]).replace(str(root), "workspace")
    architecture = output(["docker", "image", "inspect", "--format", "{{.Architecture}}", args.image])
    for setting in ("-trimpath=true", "CGO_ENABLED=0", "GOOS=linux", f"GOARCH={architecture}"):
        if f"\tbuild\t{setting}" not in build_info:
            parser.error(f"candidate must use the release build profile and reference architecture: missing {setting}")
    hardware = {"model": platform.processor() or platform.machine(), "platform": platform.platform()}
    if platform.system() == "Darwin":
        hardware.update({key: output(["sysctl", "-n", key]) for key in
                         ["machdep.cpu.brand_string", "hw.physicalcpu", "hw.logicalcpu", "hw.memsize"]})
        hardware["model"] = hardware["machdep.cpu.brand_string"]
    receipt = {
        "created_at_utc": datetime.now(timezone.utc).isoformat(),
        "source_commit": output(["git", "rev-parse", "HEAD"]),
        "source_files_sha256": {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in files},
        "candidate_sha256": hashlib.sha256(candidate.read_bytes()).hexdigest(),
        "candidate_go_build_info": build_info,
        "reference_image_id": output(["docker", "image", "inspect", "--format", "{{.Id}}", args.image]),
        "docker_version": output(["docker", "version", "--format", "{{json .}}"]),
        "host_hardware": hardware,
        "production_diff_from_commit": changes,
        "measurement_note": "Identity capture only. Stop other builds and tests before timing; record any uncontrolled host activity with the results."
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + "\n")
    print(args.output)


if __name__ == "__main__":
    main()
