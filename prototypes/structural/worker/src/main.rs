//! Optional one-file worker. BCA owns the parse shared by both consumers.

use big_code_analysis::{Ast, LANG, MetricsOptions, Source};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::io::{self, Read, Write};
use std::time::Instant;

const MAX_SOURCE_BYTES: usize = 8 * 1024 * 1024;
const MAX_REQUEST_BYTES: usize = 6 * MAX_SOURCE_BYTES + 64 * 1024;

#[derive(Debug, Default, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
enum Mode {
    #[default]
    Combined,
    Structure,
    Metrics,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    path: String,
    language: String,
    source: String,
    #[serde(default)]
    mode: Mode,
}

#[derive(Default, Debug, Serialize, PartialEq)]
struct Observations {
    classes: u64,
    interfaces: u64,
    records: u64,
    structs: u64,
    enums: u64,
    methods: u64,
    constructors: u64,
    properties: u64,
    imports: u64,
    lambdas: u64,
    local_functions: u64,
    syntax_nodes: u64,
    error_nodes: u64,
    missing_nodes: u64,
}

fn observe(ast: &Ast) -> Observations {
    let mut counts = Observations::default();
    let mut cursor = ast.as_tree_sitter().walk();
    loop {
        let node = cursor.node();
        counts.syntax_nodes += 1;
        counts.error_nodes += u64::from(node.is_error());
        counts.missing_nodes += u64::from(node.is_missing());
        match node.kind() {
            "class_declaration" => counts.classes += 1,
            "interface_declaration" | "annotation_type_declaration" => counts.interfaces += 1,
            "record_declaration" => counts.records += 1,
            "struct_declaration" => counts.structs += 1,
            "enum_declaration" => counts.enums += 1,
            "method_declaration" => counts.methods += 1,
            "constructor_declaration" | "compact_constructor_declaration" => {
                counts.constructors += 1
            }
            "property_declaration" => counts.properties += 1,
            "import_declaration" | "using_directive" => counts.imports += 1,
            "lambda_expression" | "anonymous_method_expression" => counts.lambdas += 1,
            "local_function_statement" => counts.local_functions += 1,
            _ => {}
        }
        if cursor.goto_first_child() {
            continue;
        }
        loop {
            if cursor.goto_next_sibling() {
                break;
            }
            if !cursor.goto_parent() {
                return counts;
            }
        }
    }
}

#[derive(Debug)]
struct Failure {
    code: &'static str,
    message: String,
    parse_count: u64,
}

impl Failure {
    fn before_parse(code: &'static str, message: impl Into<String>) -> Self {
        Self {
            code,
            message: message.into(),
            parse_count: 0,
        }
    }
}

fn select_language(language: &str) -> Result<(LANG, &'static str), Failure> {
    let selected = match language {
        "Shell" => (LANG::Bash, "tree-sitter-bash@0.25.1"),
        "C" => (LANG::C, "tree-sitter-c@0.24.2"),
        "C++" => (LANG::Cpp, "tree-sitter-cpp@0.23.4"),
        "C#" => (LANG::Csharp, "tree-sitter-c-sharp@0.23.5"),
        "Objective-C" => (LANG::Objc, "tree-sitter-objc@3.0.2"),
        "Elixir" => (LANG::Elixir, "tree-sitter-elixir@0.3.5"),
        "Go" => (LANG::Go, "tree-sitter-go@0.25.0"),
        "Groovy" => (LANG::Groovy, "dekobon-tree-sitter-groovy@0.2.2"),
        "F5 iRule" => (LANG::Irules, "tree-sitter-irules@0.1.1"),
        "Java" => (LANG::Java, "tree-sitter-java@0.23.5"),
        "JavaScript" => (LANG::Javascript, "tree-sitter-javascript@0.25.0"),
        "Kotlin" => (LANG::Kotlin, "tree-sitter-kotlin-ng@1.1.0"),
        "Lua" => (LANG::Lua, "tree-sitter-lua@0.5.0"),
        "Perl" => (LANG::Perl, "tree-sitter-perl@1.1.2"),
        "PHP" => (LANG::Php, "tree-sitter-php@0.24.2"),
        "Python" => (LANG::Python, "tree-sitter-python@0.25.0"),
        "Ruby" => (LANG::Ruby, "tree-sitter-ruby@0.23.1"),
        "Rust" => (LANG::Rust, "tree-sitter-rust@0.24.2"),
        "Tcl" => (LANG::Tcl, "bca-tree-sitter-tcl@2.2.0"),
        "TSX" => (LANG::Tsx, "tree-sitter-typescript@0.23.2"),
        "TypeScript" => (LANG::Typescript, "tree-sitter-typescript@0.23.2"),
        _ => {
            return Err(Failure::before_parse(
                "unsupported_language",
                "language is not enabled in this worker",
            ));
        }
    };
    Ok(selected)
}

fn analyze(request: Request) -> Result<Value, Failure> {
    if request.path.len() > 16 * 1024 {
        return Err(Failure::before_parse(
            "path_too_long",
            "display path exceeds 16 KiB",
        ));
    }
    if request.source.len() > MAX_SOURCE_BYTES {
        return Err(Failure::before_parse(
            "source_too_large",
            "source exceeds 8 MiB",
        ));
    }
    if request.source.contains('\0') {
        return Err(Failure::before_parse(
            "binary_source",
            "source contains NUL bytes",
        ));
    }
    let (language, grammar) = select_language(&request.language)?;
    let source_bytes = request.source.len();
    let mut parse_count = 0;
    let started = Instant::now();
    // This is the only parser entry point. Both consumers borrow this same Ast.
    parse_count += 1;
    let ast = Ast::parse(
        Source::from_bytes(language, request.source.into_bytes())
            .with_name(Some(request.path.clone())),
    )
    .map_err(|error| Failure {
        code: "parse_failed",
        message: error.to_string(),
        parse_count,
    })?;
    let parse_ns = started.elapsed().as_nanos();
    let syntax_errors = ast.as_tree_sitter().root_node().has_error();
    let mut result = json!({
        "status": if syntax_errors { "partial" } else { "complete" },
        "path": request.path,
        "language": request.language,
        "source_bytes": source_bytes,
        "syntax_errors": syntax_errors,
        "parse_count": parse_count,
        "provenance": {
            "bca": "big-code-analysis@2.2.0",
            "tree_sitter": "0.26.12",
            "grammar": grammar,
        },
    });
    let mut structure_ns = 0;
    let mut metrics_ns = 0;
    if request.mode != Mode::Metrics {
        let started = Instant::now();
        let observations = observe(&ast);
        structure_ns = started.elapsed().as_nanos();
        let mut observations = serde_json::to_value(observations).map_err(|error| Failure {
            code: "serialization_failed",
            message: error.to_string(),
            parse_count,
        })?;
        if !matches!(language, LANG::Java | LANG::Csharp) {
            // Declaration node names differ by grammar. Only expose the counters
            // whose semantics are validated across every enabled language.
            observations
                .as_object_mut()
                .expect("observations object")
                .retain(|key, _| {
                    matches!(
                        key.as_str(),
                        "syntax_nodes" | "error_nodes" | "missing_nodes"
                    )
                });
        }
        result["observations"] = observations;
    }
    if request.mode != Mode::Structure {
        let started = Instant::now();
        let space = ast
            .metrics(MetricsOptions::default())
            .map_err(|error| Failure {
                code: "metrics_failed",
                message: error.to_string(),
                parse_count,
            })?;
        metrics_ns = started.elapsed().as_nanos();
        // Only aggregate file metrics cross the wire; function trees can be large.
        result["metrics"] = serde_json::to_value(&space.metrics).map_err(|error| Failure {
            code: "serialization_failed",
            message: error.to_string(),
            parse_count,
        })?;
    }
    result["timings_ns"] =
        json!({"parse": parse_ns, "structure": structure_ns, "metrics": metrics_ns});
    Ok(result)
}

fn read_request(input: impl Read) -> Result<Request, Failure> {
    let mut bytes = Vec::new();
    input
        .take((MAX_REQUEST_BYTES + 1) as u64)
        .read_to_end(&mut bytes)
        .map_err(|error| Failure::before_parse("input_failed", error.to_string()))?;
    if bytes.len() > MAX_REQUEST_BYTES {
        return Err(Failure::before_parse(
            "request_too_large",
            "JSON request exceeds 48 MiB + 64 KiB",
        ));
    }
    serde_json::from_slice(&bytes)
        .map_err(|error| Failure::before_parse("invalid_request", error.to_string()))
}

fn main() {
    let (response, exit_code) = match read_request(io::stdin().lock()).and_then(analyze) {
        Ok(response) => (response, 0),
        Err(error) => (
            json!({"status": "error", "parse_count": error.parse_count,
            "error": {"code": error.code, "message": error.message}}),
            2,
        ),
    };
    let mut stdout = io::stdout().lock();
    if serde_json::to_writer(&mut stdout, &response).is_err() || stdout.write_all(b"\n").is_err() {
        std::process::exit(2);
    }
    std::process::exit(exit_code);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn request(language: &str, source: &str, mode: Mode) -> Request {
        Request {
            path: "fixture".into(),
            language: language.into(),
            source: source.into(),
            mode,
        }
    }

    // Each fixture exercises a declaration plus branching or calls, rather than
    // only proving that an empty input can pass through a grammar.
    const LANGUAGE_FIXTURES: &[(&str, &str)] = &[
        (
            "Shell",
            "#!/bin/bash\nchoose() { if [ \"$1\" = yes ]; then echo ok; else echo no; fi; }\nchoose yes\n",
        ),
        (
            "C",
            "#include <stddef.h>\nint choose(int x) { if (x > 0) return x; return 0; }\n",
        ),
        (
            "C++",
            "#include <vector>\nclass Choice { public: int choose(int x) { return x > 0 ? x : 0; } };\n",
        ),
        (
            "C#",
            "using System; class Choice { public int Choose(int x) { if (x > 0) return x; return 0; } }\n",
        ),
        (
            "Objective-C",
            "@interface Choice\n- (int)choose:(int)x;\n@end\n@implementation Choice\n- (int)choose:(int)x { if (x > 0) return x; return 0; }\n@end\n",
        ),
        (
            "Elixir",
            "defmodule Choice do\n  def choose(x) do\n    if x > 0, do: x, else: 0\n  end\nend\n",
        ),
        (
            "Go",
            "package main\nfunc choose(x int) int { if x > 0 { return x }; return 0 }\n",
        ),
        (
            "Groovy",
            "class Choice { int choose(int x) { if (x > 0) { return x }; return 0 } }\n",
        ),
        (
            "F5 iRule",
            "when HTTP_REQUEST {\n if { [HTTP::uri] eq \"/health\" } {\n  HTTP::respond 200 content \"ok\"\n }\n}\n",
        ),
        (
            "Java",
            "class Choice { int choose(int x) { if (x > 0) return x; return 0; } }\n",
        ),
        (
            "JavaScript",
            "export function choose(x) { if (x > 0) return x; return 0; }\n",
        ),
        (
            "Kotlin",
            "class Choice {\n fun choose(x: Int): Int {\n  if (x > 0) return x\n  return 0\n }\n}\n",
        ),
        (
            "Lua",
            "function choose(x)\n if x > 0 then return x end\n return 0\nend\n",
        ),
        (
            "Perl",
            "use strict;\nsub choose { my ($x) = @_; if ($x > 0) { return $x; } return 0; }\n",
        ),
        (
            "PHP",
            "<?php\nfunction choose($x) { if ($x > 0) { return $x; } return 0; }\n",
        ),
        (
            "Python",
            "def choose(x):\n    if x > 0:\n        return x\n    return 0\n",
        ),
        (
            "Ruby",
            "def choose(x)\n  if x > 0\n    x\n  else\n    0\n  end\nend\n",
        ),
        (
            "Rust",
            "pub fn choose(x: i32) -> i32 { if x > 0 { x } else { 0 } }\n",
        ),
        (
            "Tcl",
            "proc choose {x} {\n if {$x > 0} {return $x}\n return 0\n}\n",
        ),
        (
            "TSX",
            "export function Choice({x}: {x: number}) { return <div>{x > 0 ? x : 0}</div>; }\n",
        ),
        (
            "TypeScript",
            "export function choose(x: number): number { if (x > 0) return x; return 0; }\n",
        ),
    ];

    #[test]
    fn every_enabled_language_matches_direct_bca_and_separate_consumers() {
        for &(name, source) in LANGUAGE_FIXTURES {
            let combined = analyze(request(name, source, Mode::Combined)).unwrap();
            assert_eq!(combined["status"], "complete", "{name}: {combined}");
            assert_eq!(combined["parse_count"], 1, "{name}");
            let observations = combined["observations"].as_object().unwrap();
            assert!(observations["syntax_nodes"].as_u64().unwrap() > 5, "{name}");
            assert_eq!(observations["error_nodes"], 0, "{name}");
            assert_eq!(observations["missing_nodes"], 0, "{name}");
            let (language, grammar) = select_language(name).unwrap();
            assert_eq!(combined["provenance"]["grammar"], grammar);
            assert_eq!(
                grammar.rsplit_once('@').unwrap().1,
                language.grammar_version(),
                "{name}"
            );
            if !matches!(language, LANG::Java | LANG::Csharp) {
                assert_eq!(observations.len(), 3, "{name}");
                assert!(!observations.contains_key("classes"), "{name}");
            }
            let direct = Ast::parse(
                Source::from_bytes(language, source.as_bytes().to_vec())
                    .with_name(Some("fixture".into())),
            )
            .unwrap();
            let direct_metrics = direct.metrics(MetricsOptions::default()).unwrap();
            assert_eq!(
                combined["metrics"],
                serde_json::to_value(&direct_metrics.metrics).unwrap(),
                "{name}"
            );
            let structure = analyze(request(name, source, Mode::Structure)).unwrap();
            let metrics = analyze(request(name, source, Mode::Metrics)).unwrap();
            assert_eq!(
                combined["observations"], structure["observations"],
                "{name}"
            );
            assert_eq!(combined["metrics"], metrics["metrics"], "{name}");
            assert!(structure.get("metrics").is_none(), "{name}");
            assert!(metrics.get("observations").is_none(), "{name}");
        }
    }

    #[test]
    fn every_enabled_language_accepts_empty_source() {
        for &(name, _) in LANGUAGE_FIXTURES {
            let result = analyze(request(name, "", Mode::Combined)).unwrap();
            assert_eq!(result["status"], "complete", "{name}: {result}");
            assert_eq!(result["source_bytes"], 0, "{name}");
            assert_eq!(result["parse_count"], 1, "{name}");
        }
    }

    #[test]
    fn selected_language_takes_precedence_over_filename_extension() {
        let source = "export const view = <div>hello</div>;";
        let mut typescript = request("TypeScript", source, Mode::Combined);
        typescript.path = "forced.tsx".into();
        let result = analyze(typescript).unwrap();
        assert_eq!(result["language"], "TypeScript");
        assert_eq!(result["status"], "partial");
        let tsx = analyze(request("TSX", source, Mode::Combined)).unwrap();
        assert_eq!(tsx["status"], "complete");
    }

    #[test]
    fn malformed_inputs_preserve_recovery_evidence() {
        for (name, source) in [
            ("Shell", "choose() { if ["),
            ("C", "int broken( {"),
            ("C++", "class Broken { void x( {"),
            ("C#", "class Broken { void X( {"),
            ("Objective-C", "@interface Broken\n- (int)bad:(\n"),
            ("Elixir", "defmodule Broken do\n def broken("),
            ("Go", "package main\nfunc broken( {"),
            ("Groovy", "class Broken { def broken( {"),
            ("F5 iRule", "when HTTP_REQUEST {\n if {"),
            ("Java", "class Broken { void x( {"),
            ("JavaScript", "function broken( {"),
            ("Kotlin", "class Broken { fun broken("),
            ("Lua", "function broken("),
            ("Perl", "sub broken { if ("),
            ("PHP", "<?php function broken( {"),
            ("Python", "def broken(:\n    return"),
            ("Ruby", "def broken("),
            ("Rust", "fn broken( {"),
            ("Tcl", "proc broken {x} {\n if {"),
            ("TSX", "export function Broken() { return <div>"),
            ("TypeScript", "function broken( {"),
        ] {
            let result = analyze(request(name, source, Mode::Combined)).unwrap();
            assert_eq!(result["status"], "partial", "{name}: {result}");
            assert_eq!(result["syntax_errors"], true, "{name}");
            let observations = &result["observations"];
            assert!(
                observations["error_nodes"].as_u64().unwrap()
                    + observations["missing_nodes"].as_u64().unwrap()
                    > 0,
                "{name}"
            );
        }
    }

    #[test]
    fn combined_reuses_parse_and_matches_separate_consumers() {
        for (language, source) in [
            (
                "Java",
                "import java.util.List; record Value(int x) {} interface I { int f(); } class C implements I { C() {} public int f() { return 1; } class Inner {} }",
            ),
            (
                "C#",
                "using System; namespace Example; interface I { int F(); } public record Value(int X); class C : I { public int P { get; set; } public C() {} public int F() { int Local(int x) => x + 1; Func<int,int> f = x => x * 2; return f(Local(1)); } }",
            ),
        ] {
            let combined = analyze(request(language, source, Mode::Combined)).unwrap();
            let structure = analyze(request(language, source, Mode::Structure)).unwrap();
            let metrics = analyze(request(language, source, Mode::Metrics)).unwrap();
            assert_eq!(combined["status"], "complete", "{combined}");
            assert_eq!(combined["parse_count"], 1);
            assert_eq!(combined["observations"], structure["observations"]);
            assert_eq!(combined["metrics"], metrics["metrics"]);
            assert!(structure.get("metrics").is_none());
            assert!(metrics.get("observations").is_none());
            assert_eq!(combined["observations"]["interfaces"], 1);
            assert_eq!(combined["observations"]["records"], 1);
            assert_eq!(combined["observations"]["constructors"], 1);
            assert_eq!(combined["observations"]["imports"], 1);
            assert_eq!(combined["observations"]["methods"], 2);
            if language == "C#" {
                assert_eq!(combined["observations"]["properties"], 1);
                assert_eq!(combined["observations"]["lambdas"], 1);
                assert_eq!(combined["observations"]["local_functions"], 1);
            } else {
                assert_eq!(combined["observations"]["classes"], 2);
            }
        }
    }

    #[test]
    fn malformed_syntax_is_partial() {
        let result = analyze(request("Java", "class Broken { void x( {", Mode::Combined)).unwrap();
        assert_eq!(result["status"], "partial");
        assert_eq!(result["syntax_errors"], true);
        assert_eq!(result["parse_count"], 1);
    }

    #[test]
    fn rejected_requests_do_not_parse() {
        for (language, source, code) in [
            ("XML", "<log/>", "unsupported_language"),
            ("Java", "class X {}\0", "binary_source"),
        ] {
            let error = analyze(request(language, source, Mode::Combined)).unwrap_err();
            assert_eq!(error.code, code);
            assert_eq!(error.parse_count, 0);
        }
        let error = analyze(request(
            "Java",
            &" ".repeat(MAX_SOURCE_BYTES + 1),
            Mode::Combined,
        ))
        .unwrap_err();
        assert_eq!(error.code, "source_too_large");
        assert_eq!(error.parse_count, 0);
        assert!(read_request(b"{}".as_slice()).is_err());
        assert!(
            read_request(
                b"{\"path\":\"a.java\",\"language\":\"Java\",\"source\":\"\",\"mode\":\"bad\"}"
                    .as_slice()
            )
            .is_err()
        );
        assert!(read_request(b"{} {}".as_slice()).is_err());
    }

    #[test]
    fn wire_limit_is_bounded() {
        let error = read_request(io::repeat(b' ').take((MAX_REQUEST_BYTES + 1) as u64))
            .err()
            .unwrap();
        assert_eq!(error.code, "request_too_large");
    }
}
