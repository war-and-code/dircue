#!/usr/bin/env python3
"""Exercise whole-map availability across repeated valid declarations."""

import argparse
import json
import random
import subprocess
import tempfile
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    rng = random.Random(42)

    with tempfile.TemporaryDirectory(prefix="dircue-map-robustness-") as temporary:
        root = Path(temporary)
        workflow = root / ".github" / "workflows" / "check.yml"
        workflow.parent.mkdir(parents=True)
        for iteration in range(80):
            aliases = [rng.choice(("east", "west", "shared")) for _ in range(rng.randrange(1, 7))]
            (root / "main.tf").write_text(
                "\n".join(f'provider "aws" {{\n  alias = "{alias}"\n}}' for alias in aliases)
            )
            (root / "package.json").write_text(json.dumps({"name": f"app-{iteration}", "version": "1.0.0"}))
            workflow.write_text(
                "name: init & validate\n"
                "on:\n  push:\n  schedule:\n    - cron: '0 12 * * *'\n"
                "jobs:\n  check:\n    runs-on: ubuntu-latest\n"
                "    steps:\n      - run: echo ok && ls -d */\n"
            )
            (root / "k8s.yaml").write_text(
                "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n"
                f"  name: app-{iteration}\n---\napiVersion: v1\nkind: Service\n"
                f"metadata:\n  name: app-{iteration}\n"
            )
            result = subprocess.run(
                [str(binary), "map", "--json", "--source", "directory", str(root)],
                capture_output=True, text=True, timeout=10,
            )
            if result.returncode:
                raise AssertionError(f"variant {iteration} lost the map: {result.stderr[:500]}")
            document = json.loads(result.stdout)
            if document["kind"] != "map" or not document["coverage"]:
                raise AssertionError(f"variant {iteration} returned an incomplete document")
    print(json.dumps({"gate": "whole-map-availability", "variants": 80, "status": "passed"}))


if __name__ == "__main__":
    main()
