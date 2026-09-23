#!/usr/bin/env python3
"""Run the candidate's one-shot map command over the initial corpus."""
import argparse
import json
import os
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
FIXTURES = HERE / "fixtures"

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--binary", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    args = p.parse_args()
    manifest = json.loads((FIXTURES / "manifest.json").read_text())
    args.output.mkdir(parents=True, exist_ok=True)
    for entry in manifest["fixtures"]:
        source = FIXTURES / entry["id"]
        command = [str(args.binary), "map", "--json", str(source)]
        result = subprocess.run(command, capture_output=True, text=True)
        if result.returncode != 0:
            raise SystemExit(f"{entry['id']}: map failed ({result.returncode}): {result.stderr.strip()}")
        try:
            document = json.loads(result.stdout)
        except json.JSONDecodeError as exc:
            raise SystemExit(f"{entry['id']}: map did not emit JSON: {exc}") from exc
        (args.output / f"{entry['id']}.json").write_text(json.dumps(document, indent=2) + "\n")
        repeat = subprocess.run(command, capture_output=True, text=True)
        if repeat.returncode != 0 or json.loads(repeat.stdout) != document:
            raise SystemExit(f"{entry['id']}: repeated map invocation was not deterministic")
    print(f"wrote {len(manifest['fixtures'])} map documents to {args.output}")

if __name__ == "__main__":
    main()
