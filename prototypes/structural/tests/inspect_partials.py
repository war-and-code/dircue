#!/usr/bin/env python3
"""Locate sampled parse errors and probe preprocessing/BOM effects separately."""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker", type=Path, required=True)
    parser.add_argument("--benchmark", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    worker = args.worker.resolve()
    benchmark = json.loads(args.benchmark.read_text())

    def inspect(source, path, language):
        request = dict(source=source, path=path, language=language, mode="structure")
        run = subprocess.run([str(worker)], input=json.dumps(request), text=True,
                             capture_output=True, check=True, timeout=30)
        result = json.loads(run.stdout)
        return {"status": result["status"], "error_nodes": result["observations"]["error_nodes"],
                "missing_nodes": result["observations"]["missing_nodes"]}

    partials = []
    for corpus in benchmark["corpora"]:
        language = "Java" if corpus["name"] == "spring-framework" else "C#"
        for file in corpus["files"]:
            raw = (args.corpus_root / corpus["name"] / file["path"]).read_bytes()
            if hashlib.sha256(raw).hexdigest() != file["sha256"]:
                raise ValueError("Input changed: " + file["path"])
            source = raw.decode("utf-8")
            original = inspect(source, file["path"], language)
            if original["status"] != "partial":
                continue
            partial = {"corpus": corpus["name"], "commit": corpus["commit"], **file,
                             "original": original,
                             "without_bom": inspect(source.lstrip("\ufeff"), file["path"], language),
                             "without_directive_lines": inspect("\n".join(
                                 line for line in source.splitlines() if not line.lstrip().startswith("#")),
                                 file["path"], language)}
            if file["path"] == "src/Scripting/Core/ScriptBuilder.cs":
                original_assignment = "pdbStreamOpt?.Position = 0;"
                replacement = "if (pdbStreamOpt != null) pdbStreamOpt.Position = 0;"
                if source.count(original_assignment) != 1:
                    raise ValueError("Expected one null-conditional assignment in ScriptBuilder.cs")
                partial["null_conditional_assignment_probe"] = {
                    "original_statement": original_assignment,
                    "replacement_statement": replacement,
                    "result": inspect(source.replace(original_assignment, replacement), file["path"], language),
                }
            partials.append(partial)
    minimal_sources = {
        "null_conditional_assignment": "class C { void F(dynamic stream) { stream?.Position = 0; } }",
        "explicit_null_check": "class C { void F(dynamic stream) { if (stream != null) stream.Position = 0; } }",
    }
    report = {"worker_sha256": hashlib.sha256(worker.read_bytes()).hexdigest(),
              "caveat": "Modified-source probes only diagnose parser sensitivity; they are not proposed preprocessing or benchmark inputs.",
              "partial_files": partials,
              "csharp14_minimal_probes": {
                  name: {"source": source, "result": inspect(source, name + ".cs", "C#")}
                  for name, source in minimal_sources.items()
              }}
    args.output.write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
