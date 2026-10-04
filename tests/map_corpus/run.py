#!/usr/bin/env python3
"""Run the candidate's one-shot map command over the initial corpus."""
import argparse
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
FIXTURES = HERE / "fixtures"

def run_map(binary, source, timeout=60):
    command = [str(binary), "map", "--source", "directory", "--json", str(source)]
    try:
        result = subprocess.run(command, capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        raise ValueError(f"{source.name}: map exceeded {timeout}s") from exc
    if result.returncode != 0:
        stderr = result.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError(f"{source.name}: map failed ({result.returncode}): {stderr}")
    try:
        document = json.loads(result.stdout)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"{source.name}: map did not emit JSON: {exc}") from exc
    return document, result.stdout, result.stderr


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--binary", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    args = p.parse_args()
    manifest = json.loads((FIXTURES / "manifest.json").read_text())
    args.output.mkdir(parents=True, exist_ok=True)
    for entry in manifest["fixtures"]:
        source = FIXTURES / entry["id"]
        try:
            document, stdout, stderr = run_map(args.binary, source)
            repeated, repeat_stdout, repeat_stderr = run_map(args.binary, source)
            if repeated != document or repeat_stdout != stdout or repeat_stderr != stderr:
                raise ValueError(f"{entry['id']}: repeated map invocation was not byte deterministic")
        except ValueError as exc:
            raise SystemExit(str(exc)) from exc
        (args.output / f"{entry['id']}.json").write_text(json.dumps(document, indent=2) + "\n")
    print(f"wrote {len(manifest['fixtures'])} map documents to {args.output}")

if __name__ == "__main__":
    main()
