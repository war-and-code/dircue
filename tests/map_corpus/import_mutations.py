#!/usr/bin/env python3
"""Run a small, reproducible mutation campaign over import evidence guards.

This opt-in campaign mutates disposable detached Git worktrees only. It does
not change the caller's checkout. The mutations are local probes, not the
reviewer's original mutation set or a substitute for a general mutation tool.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]

# Each single-site replacement names the production mutation, the selected Go
# test expected to reject it, and the exact old/new source fragments.
MUTATIONS = [
    {
        "name": "JS source-import observer disabled",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "return parseJSImportsTokens(name, toks, !limited), limited",
        "new": "_ = toks; return []Observation{}, limited",
        "package": "./pkg/intentmap",
        "test": "TestAdditionalImportScannersBoundaries",
    },
    {
        "name": "JSX text mask disabled",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "if !strings.EqualFold(path.Ext(name), \".ts\") {",
        "new": "if false {",
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "TypeScript angle assertions treated as JSX",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "if !strings.EqualFold(path.Ext(name), \".ts\") {",
        "new": "if true {",
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "JSX mask ignored by parser",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "t.depth != 0 || t.kind != 'i' || jsxText[i]",
        "new": "t.depth != 0 || t.kind != 'i' || false && jsxText[i]",
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "self-closing JSX treated as children",
        "file": "pkg/intentmap/imports_extra.go",
        "old": '''if openEnd > i && t[openEnd-1].text == "/" {
				// A self-closing JSX element has no child-text region.
				continue
			}''',
        "new": "",
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "malformed JSX fail-closed masking removed",
        "file": "pkg/intentmap/imports_extra.go",
        "old": '''} else if !fragment && t[i+1].text != "" && t[i+1].text[0] >= 'a' && t[i+1].text[0] <= 'z' {
			// An unmatched lowercase tag is likely malformed JSX. Omit apparent
			// source tokens through EOF rather than treating its children as code.
			for j := openEnd + 1; j < len(mask); j++ {
				mask[j] = true
			}
		}''',
        "new": '''} else if !fragment && t[i+1].text != "" && t[i+1].text[0] >= 'a' && t[i+1].text[0] <= 'z' {
			_ = openEnd
		}''',
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "nested template boundary broken",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "j = findTemplateLiteralEnd(src, start)",
        "new": "j = start + 1",
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "regex literal in interpolation not opaque",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "if c == '/' && jsTemplateRegexMayStart(src, expressions[len(expressions)-1].start, i) {",
        "new": "if c == '/' && false {",
        "package": "./pkg/intentmap",
        "test": "TestJSImportParserIgnoresTemplateAndJSXText",
    },
    {
        "name": "JS import-specifier strings are not tokenized",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "sourceToken{text: val, kind: 's', line: startLine, depth: depth}",
        "new": "sourceToken{text: val, kind: 'p', line: startLine, depth: depth}",
        "package": "./pkg/intentmap",
        "test": "TestAdditionalImportScannersBoundaries",
    },
    {
        "name": "Java package namespace boundary relaxed",
        "file": "pkg/intentmap/imports_extra.go",
        "old": 'caps := capabilitiesFor("maven-import", pkg)',
        "new": 'caps := capabilitiesFor("maven-import", pkg); if strings.Contains(pkg, "org.postgresqlish") { caps = []string{"datastore:postgresql"} }',
        "package": "./pkg/intentmap",
        "test": "TestLexicalImportEvidenceAdversarialBoundaries",
    },
    {
        "name": "C# method-scoped using accepted",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "if (t.depth != 0 && (vb || !namespaceScope[i])) || t.kind != 'i' {",
        "new": "if (t.depth != 0 && (vb || !namespaceScope[i]) && false) || t.kind != 'i' {",
        "package": "./pkg/intentmap",
        "test": "TestLexicalImportEvidenceAdversarialBoundaries",
    },
    {
        "name": "C# using aliases disabled",
        "file": "pkg/intentmap/imports_extra.go",
        "old": 'if j < len(toks) && toks[j].kind == \'i\' && j+1 < len(toks) && toks[j+1].text == "=" {',
        "new": "if false {",
        "package": "./pkg/intentmap",
        "test": "TestLexicalImportEvidenceAdversarialBoundaries",
    },
    {
        "name": "VB REM comments no longer masked",
        "file": "pkg/intentmap/imports_extra.go",
        "old": 'if lang == "vb" && vbStatementStart && i+3 <= len(src)',
        "new": "if false && vbStatementStart && i+3 <= len(src)",
        "package": "./pkg/intentmap",
        "test": "TestVBRemAndTypeScriptInlineTypeImports",
    },
    {
        "name": "TypeScript type-only qualifier dropped",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "typeOnly := tsImportTypeOnly(toks, i)",
        "new": "typeOnly := false",
        "package": "./pkg/intentmap",
        "test": "TestVBRemAndTypeScriptInlineTypeImports",
    },
    {
        "name": "all files called test code",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "if pathHasTestDirectory(l) {",
        "new": "if true {",
        "package": "./pkg/intentmap",
        "test": "TestImportEvidenceUsesTestPathConvention",
    },
    {
        "name": "C# and JVM Test suffix case folded",
        "file": "pkg/intentmap/imports_extra.go",
        "old": 'strings.HasSuffix(originalStem, "Test")',
        "new": 'strings.HasSuffix(strings.ToLower(originalStem), "test")',
        "package": "./pkg/intentmap",
        "test": "TestImportEvidenceUsesTestPathConvention",
    },
    {
        "name": "code_syntax imports count as non-import evidence",
        "file": "pkg/mapbuild/observers.go",
        "old": 'if o.Basis != "imported" && o.Basis != "code_syntax" {',
        "new": 'if o.Basis != "imported" {',
        "package": "./pkg/mapbuild",
        "test": "TestCapabilityEvidenceQualificationsRetainMixedBases",
    },
    {
        "name": "global coverage suppresses observed test-only evidence",
        "file": "pkg/mapbuild/observers.go",
        "old": "if g.testEvidence && !g.nonTestEvidence && !g.otherEvidence {",
        "new": "if g.testEvidence && !g.nonTestEvidence && !g.otherEvidence && r.Coverage.Status == \"complete\" {",
        "package": "./pkg/mapbuild",
        "test": "TestUnrelatedFileOmissionDoesNotHideObservedTestOnlyEvidence",
    },
    {
        "name": "type/test imports upgrade conditional requirements",
        "file": "pkg/mapbuild/observers.go",
        "old": 'importEvidence := o.Basis == "imported" || o.Basis == "code_syntax"\n\t\truntimeEvidence := !importEvidence || (o.Properties["import_qualifier"] != "type_only" && o.Properties["evidence_scope"] != "test_path_convention")',
        "new": "runtimeEvidence := true",
        "package": "./pkg/mapbuild",
        "test": "TestOptionalDependencyImportQualificationsDoNotPromoteRuntimeState",
    },
    {
        "name": "code_syntax mistaken for independent runtime evidence",
        "file": "pkg/mapbuild/observers.go",
        "old": 'importEvidence := o.Basis == "imported" || o.Basis == "code_syntax"\n\t\truntimeEvidence := !importEvidence || (o.Properties["import_qualifier"] != "type_only" && o.Properties["evidence_scope"] != "test_path_convention")',
        "new": 'runtimeEvidence := o.Basis != "imported" || (o.Properties["import_qualifier"] != "type_only" && o.Properties["evidence_scope"] != "test_path_convention")',
        "package": "./pkg/mapbuild",
        "test": "TestOptionalDependencyImportQualificationsDoNotPromoteRuntimeState",
    },
    {
        "name": "TypeScript import-equals type qualifier dropped",
        "file": "pkg/intentmap/imports_extra.go",
        "old": "addJSImport(&out, name, t.line, toks[end].line, spec, typeOnly)",
        "new": "addJSImport(&out, name, t.line, toks[end].line, spec, false)",
        "package": "./pkg/intentmap",
        "test": "TestLexicalImportEvidenceAdversarialBoundaries",
    },
    {
        "name": "runtime imports cannot corroborate conditional requirements",
        "file": "pkg/mapbuild/observers.go",
        "old": "if runtimeEvidence {\n\t\t\tg.runtimeImportEvidence = true\n\t\t\tg.state = \"observed\"",
        "new": "if runtimeEvidence && false {\n\t\t\tg.runtimeImportEvidence = true\n\t\t\tg.state = \"observed\"",
        "package": "./pkg/mapbuild",
        "test": "TestRuntimeImportCanCorroborateOptionalDependency",
    },
    {
        "name": "generic test_ basename classifier restored",
        "file": "pkg/intentmap/imports_extra.go",
        "old": 'case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":\n\t\treturn strings.Contains(b, ".test.") || strings.Contains(b, ".spec.")',
        "new": 'case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":\n\t\treturn strings.HasPrefix(b, "test_") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.")',
        "package": "./pkg/intentmap",
        "test": "TestImportEvidenceUsesTestPathConvention",
    },
]


def run(argv: list[str], cwd: Path, timeout: float) -> dict:
    started = time.monotonic()
    try:
        proc = subprocess.run(
            argv, cwd=cwd, text=True, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, timeout=timeout, check=False,
        )
        return {
            "argv": argv,
            "returncode": proc.returncode,
            "stdout": proc.stdout,
            "stderr": proc.stderr,
            "duration_seconds": round(time.monotonic() - started, 3),
            "timed_out": False,
        }
    except subprocess.TimeoutExpired as exc:
        return {
            "argv": argv,
            "returncode": None,
            "stdout": decode(exc.stdout),
            "stderr": decode(exc.stderr),
            "duration_seconds": round(time.monotonic() - started, 3),
            "timed_out": True,
        }


def decode(value: str | bytes | None) -> str:
    if value is None:
        return ""
    return value.decode(errors="replace") if isinstance(value, bytes) else value


def assertion_failure(test_name: str, result: dict) -> bool:
    if result["returncode"] == 0 or result["timed_out"]:
        return False
    combined = result["stdout"] + result["stderr"]
    if any(marker in combined.lower() for marker in (
        "[build failed]", "build failed", "syntax error:", "undefined:",
        "panic:", "fatal error:", "runtime error:", "signal: ",
        "no required module provides package", "cannot find module",
    )):
        return False
    return re.search(rf"^--- FAIL: {re.escape(test_name)}(?:/|\s|$)", combined, re.MULTILINE) is not None


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def make_output_path(value: str | None, commit: str) -> Path:
    if value:
        target = Path(value).expanduser().resolve()
        target.parent.mkdir(parents=True, exist_ok=True)
        # Reserve the name atomically so concurrent runs cannot overwrite one
        # another and explicit output paths never replace existing evidence.
        fd = os.open(target, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(fd)
        return target
    fd, name = tempfile.mkstemp(prefix=f"dircue-import-mutations-{commit[:12]}-", suffix=".json")
    os.close(fd)
    return Path(name)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--commit", default="HEAD", help="commit or ref to test (default: HEAD)")
    parser.add_argument("--output", help="new JSON receipt path; default is a fresh file in the system temp directory")
    parser.add_argument("--timeout", type=float, default=30, help="seconds per Go test command (default: 30)")
    args = parser.parse_args()

    git_root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=ROOT, text=True).strip())
    commit = subprocess.check_output(["git", "rev-parse", args.commit], cwd=git_root, text=True).strip()
    output_path = make_output_path(args.output, commit)
    go_version = run(["go", "version"], git_root, args.timeout)
    git_version = run(["git", "--version"], git_root, args.timeout)
    python_version = sys.version
    modified_sources = sorted({m["file"] for m in MUTATIONS})
    selected_tests = sorted({(m["package"], m["test"]) for m in MUTATIONS})
    receipt = {
        "schema": "dircue-import-mutation-campaign-1",
        "source_commit": commit,
        "source_tree": subprocess.check_output(["git", "rev-parse", f"{commit}^{{tree}}"], cwd=git_root, text=True).strip(),
        "toolchain": {"go": go_version, "git": git_version, "python": python_version},
        "source_file_sha256": {},
        "test_file_sha256": {},
        "baseline_tests": [],
        "mutations": [],
        "summary": {},
        "worktree_cleanup": None,
    }
    exit_code = 0
    with tempfile.TemporaryDirectory(prefix="dircue-import-mutations-") as temp_dir:
        worktree = Path(temp_dir) / "checkout"
        add = run(["git", "worktree", "add", "--detach", str(worktree), commit], git_root, args.timeout)
        receipt["worktree_add"] = add
        if add["returncode"] != 0:
            receipt["summary"] = {"baseline_passed": False, "killed": 0, "survived": 0, "unviable": len(MUTATIONS)}
            exit_code = 1
        else:
            try:
                # Hash the detached commit's files, not possibly-dirty files
                # in the caller's checkout.
                receipt["source_file_sha256"] = {
                    p: sha256(worktree / p) for p in modified_sources
                }
                receipt["test_file_sha256"] = {
                    p: sha256(worktree / p) for p in (
                        "pkg/intentmap/intentmap_test.go",
                        "pkg/mapbuild/build_test.go",
                    )
                }
                for package, test in selected_tests:
                    result = run(["go", "test", package, "-run", f"^{test}$", "-count=1"], worktree, args.timeout)
                    result.update({"package": package, "test": test})
                    receipt["baseline_tests"].append(result)
                baseline_ok = all(r["returncode"] == 0 and not r["timed_out"] for r in receipt["baseline_tests"])
                if not baseline_ok:
                    exit_code = 1
                if baseline_ok:
                    for mutation in MUTATIONS:
                        target = worktree / mutation["file"]
                        original = target.read_text(encoding="utf-8")
                        old = mutation["old"]
                        occurrence_count = original.count(old)
                        record = {
                            "name": mutation["name"],
                            "file": mutation["file"],
                            "old_fragment": old,
                            "new_fragment": mutation["new"],
                            "selected_test_argv": ["go", "test", mutation["package"], "-run", f"^{mutation['test']}$", "-count=1"],
                            "expected_test": mutation["test"],
                            "source_file_sha256_before_mutation": sha256(target),
                            "replacement_count": occurrence_count,
                        }
                        if occurrence_count != 1:
                            record["status"] = "unviable"
                            record["reason"] = f"expected one exact match, found {occurrence_count}"
                            exit_code = 1
                        else:
                            target.write_text(original.replace(old, mutation["new"], 1), encoding="utf-8")
                            try:
                                result = run(record["selected_test_argv"], worktree, args.timeout)
                                record["result"] = result
                                if result["timed_out"]:
                                    record["status"] = "unviable"
                                    record["reason"] = "selected test timed out"
                                    exit_code = 1
                                elif assertion_failure(mutation["test"], result):
                                    record["status"] = "killed"
                                elif result["returncode"] == 0:
                                    record["status"] = "survived"
                                    exit_code = 1
                                else:
                                    record["status"] = "unviable"
                                    record["reason"] = "non-assertion failure (build, runtime, or infrastructure); inspect full result output"
                                    exit_code = 1
                            finally:
                                target.write_text(original, encoding="utf-8")
                        record["source_file_sha256_after_restore"] = sha256(target)
                        receipt["mutations"].append(record)
            finally:
                cleanup = run(["git", "worktree", "remove", "--force", str(worktree)], git_root, args.timeout)
                receipt["worktree_cleanup"] = cleanup
                if cleanup["returncode"] != 0:
                    exit_code = 1
        statuses = [m.get("status", "unviable") for m in receipt["mutations"]]
        receipt["summary"] = {
            "baseline_passed": bool(receipt["baseline_tests"]) and all(r["returncode"] == 0 and not r["timed_out"] for r in receipt["baseline_tests"]),
            "total": len(MUTATIONS),
            "killed": statuses.count("killed"),
            "survived": statuses.count("survived"),
            "unviable": statuses.count("unviable") + (len(MUTATIONS) - len(statuses)),
        }
    output_path.write_text(json.dumps(receipt, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"receipt: {output_path}")
    print(json.dumps(receipt["summary"], sort_keys=True))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
