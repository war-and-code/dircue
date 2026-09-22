#!/usr/bin/env python3
"""Build a source-bound comparison candidate with a fixed comparison version.

The 0.8.0 version override isolates intentional CLI changes from release-version
metadata. This executable is an experiment artifact, not a distributable release.
"""

import argparse
from datetime import datetime, timezone
import os
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tests/context_v080"))
import common
import build as inherited_build


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--build-receipt", type=Path, required=True)
    parser.add_argument("--cgo-enabled", choices=("0", "1"), default="0",
                        help="Use 1 only to match the isolated performance build; release core uses 0")
    args = parser.parse_args()
    output, receipt = args.candidate.resolve(), args.build_receipt.resolve()
    if output.exists() or receipt.exists():
        raise AssertionError("use fresh candidate and receipt paths")
    output.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED=args.cgo_enabled, GOWORK="off", GOFLAGS="")
    inputs = inherited_build.build_inputs(env)
    source = inherited_build.source_state()
    command = ["go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath",
               "-ldflags", "-X dircue/internal/cli.Version=0.8.0", "-o", str(output), "."]
    started = datetime.now(timezone.utc).isoformat()
    subprocess.run(command, cwd=ROOT, env=env, check=True)
    if inputs != inherited_build.build_inputs(env):
        output.unlink()
        raise AssertionError("compilation inputs changed during build")
    common.write_json(receipt, {
        "schema": "dircue-context-v080-build-1",
        "purpose": "v100 preparation comparison; not a release binary",
        "comparison_version_override": "0.8.0",
        "started_at_utc": started,
        "finished_at_utc": datetime.now(timezone.utc).isoformat(),
        "source_at_build": source, "files": inputs,
        "candidate_sha256": common.sha256(output), "command": command,
        "environment": {key: env[key] for key in ("CGO_ENABLED", "GOWORK", "GOFLAGS")},
        "go_version": subprocess.check_output(["go", "version"], env=env).decode().strip(),
        "go_build_info": subprocess.check_output(["go", "version", "-m", str(output)], env=env).decode().strip(),
        "builder_sha256": common.sha256(Path(__file__)),
        "inherited_builder_sha256": common.sha256(Path(inherited_build.__file__)),
    })
    print(receipt)


if __name__ == "__main__":
    main()
