//! Isolated feasibility probe. Syntactic evidence is not a runtime route inventory.
use big_code_analysis::{Ast, LANG, MetricsOptions, Source, tree_sitter::Node};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::{
    collections::{BTreeMap, BTreeSet},
    io::{self, Read},
    time::Instant,
};
const MAX_SOURCE: usize = 256 * 1024;
const MAX_ENTRIES: usize = 128;
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    path: String,
    language: String,
    source: String,
}
#[derive(Clone, Serialize)]
struct Span {
    start_byte: usize,
    end_byte: usize,
    start_line: usize,
    end_line: usize,
}
fn span(n: Node<'_>) -> Span {
    Span {
        start_byte: n.start_byte(),
        end_byte: n.end_byte(),
        start_line: n.start_position().row + 1,
        end_line: n.end_position().row + 1,
    }
}
fn text<'a>(n: Node<'_>, s: &'a [u8]) -> &'a str {
    std::str::from_utf8(&s[n.byte_range()]).expect("UTF-8 source")
}
fn field<'a>(n: Node<'a>, name: &str) -> Option<Node<'a>> {
    n.child_by_field_name(name)
}
fn kind_child<'a>(n: Node<'a>, kind: &str) -> Option<Node<'a>> {
    (0..n.named_child_count())
        .filter_map(|i| n.named_child(i as u32))
        .find(|n| n.kind() == kind)
}
fn walk(mut f: impl FnMut(Node<'_>), ast: &Ast) {
    let mut c = ast.as_tree_sitter().walk();
    loop {
        f(c.node());
        if c.goto_first_child() {
            continue;
        }
        loop {
            if c.goto_next_sibling() {
                break;
            }
            if !c.goto_parent() {
                return;
            }
        }
    }
}
fn module_scope(n: Node<'_>) -> bool {
    let mut p = n.parent();
    while let Some(v) = p {
        if matches!(
            v.kind(),
            "function_definition"
                | "class_definition"
                | "if_statement"
                | "try_statement"
                | "for_statement"
                | "with_statement"
        ) {
            return false;
        }
        p = v.parent();
    }
    true
}
fn owner(n: Node<'_>) -> &'static str {
    let mut p = n.parent();
    while let Some(v) = p {
        match v.kind() {
            "method_declaration" => return "method",
            "class_declaration" | "interface_declaration" | "annotation_type_declaration" => {
                return "type";
            }
            "function_definition" => return "nested-scope",
            _ => {}
        }
        p = v.parent();
    }
    "file"
}
fn literal(n: Option<Node<'_>>, s: &[u8]) -> (Option<String>, Option<&'static str>) {
    let Some(n) = n else {
        return (None, Some("route-not-specified"));
    };
    if !matches!(n.kind(), "string_literal" | "string") {
        return (None, Some("nonliteral-route"));
    }
    let t = text(n, s);
    if t.len() > 2048 {
        return (None, Some("route-text-limit"));
    }
    let quote = t.as_bytes().first().copied();
    if !matches!(quote, Some(b'"' | b'\''))
        || t.len() < 2
        || t.as_bytes().last().copied() != quote
        || t.starts_with("\"\"\"")
        || t.starts_with("'''")
        || t.contains('\\')
        || t.chars().any(char::is_control)
    {
        return (None, Some("unsupported-string-form"));
    }
    let value = &t[1..t.len() - 1];
    if value.contains("${")
        || value.contains("#{")
        || value.contains("[controller]")
        || value.contains("[action]")
    {
        return (Some(value.into()), Some("configuration-or-route-token"));
    }
    (Some(value.into()), None)
}
#[derive(Serialize)]
struct Entry {
    index: usize,
    framework_candidate: &'static str,
    rule: &'static str,
    rule_version: &'static str,
    declaration: &'static str,
    scope: &'static str,
    span: Span,
    method: Option<String>,
    route_literal: Option<String>,
    qualification: &'static str,
    evidence: Vec<Span>,
    status: &'static str,
    reasons: Vec<&'static str>,
}
#[derive(Clone)]
struct Binding {
    target: String,
    at: Span,
}
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let mut raw = Vec::new();
    io::stdin()
        .take((MAX_SOURCE * 6 + 4097) as u64)
        .read_to_end(&mut raw)?;
    if raw.len() > MAX_SOURCE * 6 + 4096 {
        return Err("request limit".into());
    }
    let r: Request = serde_json::from_slice(&raw)?;
    if r.source.len() > MAX_SOURCE
        || r.source.contains('\0')
        || r.path.len() > 4096
        || r.path.is_empty()
        || r.path.starts_with('/')
        || r.path.contains('\\')
        || r.path.contains(':')
        || r.path.chars().any(char::is_control)
        || r.path
            .split('/')
            .any(|x| x == ".." || x == "." || x.is_empty())
    {
        return Err("invalid bounded source/path".into());
    }
    let lang = match r.language.as_str() {
        "Java" => LANG::Java,
        "C#" => LANG::Csharp,
        "Python" => LANG::Python,
        _ => return Err("unsupported language".into()),
    };
    let begin = Instant::now();
    // The only parser call. Both the experimental traversal and BCA metrics borrow this Ast.
    let mut parse_count = 0;
    parse_count += 1;
    let ast = Ast::parse(Source::new(lang, r.source.as_bytes()).with_name(Some(r.path.clone())))?;
    let parse_ns = begin.elapsed().as_nanos();
    let source = ast.source();
    let root = ast.as_tree_sitter().root_node();
    let bounds_started = Instant::now();
    let mut node_count = 0usize;
    let mut depth_limit = false;
    walk(
        |n| {
            node_count += 1;
            let mut p = n.parent();
            let mut depth = 0;
            while let Some(v) = p {
                depth += 1;
                if depth > 256 {
                    depth_limit = true;
                    break;
                }
                p = v.parent();
            }
        },
        &ast,
    );
    if node_count > 100_000 || depth_limit {
        return Err("syntax tree node/depth limit".into());
    }

    let bounds_ns = bounds_started.elapsed().as_nanos();
    let begin = Instant::now();
    let mut imports: BTreeMap<String, Binding> = BTreeMap::new();
    let mut duplicate_imports = BTreeSet::new();
    let mut local_types = BTreeSet::new();
    let mut assignments: BTreeMap<String, usize> = BTreeMap::new();
    let mut import_overflow = false;
    let mut mvc_using: Option<Span> = None;
    walk(
        |n| {
            if imports.len() > 256 || local_types.len() > 256 || assignments.len() > 256 {
                import_overflow = true;
                return;
            }
            if matches!(
                n.kind(),
                "class_declaration" | "annotation_type_declaration" | "interface_declaration"
            ) && let Some(name) = field(n, "name")
            {
                local_types.insert(text(name, source).to_string());
            }
            if r.language == "Python"
                && matches!(n.kind(), "function_definition" | "class_definition")
                && let Some(name) = field(n, "name")
            {
                *assignments.entry(text(name, source).into()).or_default() += 1
            }
            if (n.kind() == "assignment" || n.kind() == "augmented_assignment")
                && let Some(left) = field(n, "left")
                && left.kind() == "identifier"
            {
                *assignments.entry(text(left, source).into()).or_default() += 1
            }
            let mut add = |name: String, target: String| {
                if name.len() > 512 || target.len() > 512 {
                    import_overflow = true;
                    return;
                }
                if imports
                    .insert(
                        name.clone(),
                        Binding {
                            target,
                            at: span(n),
                        },
                    )
                    .is_some()
                {
                    duplicate_imports.insert(name);
                }
            };
            if n.kind() == "import_declaration"
                && let Some(name) = kind_child(n, "scoped_identifier")
            {
                let full = text(name, source);
                if !text(n, source).contains('*') {
                    add(full.rsplit('.').next().unwrap_or("").into(), full.into());
                }
            }
            if n.kind() == "using_directive"
                && n.parent().is_some_and(|p| p.kind() == "compilation_unit")
            {
                let name = field(n, "name");
                let last = n.named_child(n.named_child_count().saturating_sub(1) as u32);
                if let Some(target) = last {
                    let t = text(target, source).replace("global::", "");
                    if t == "Microsoft.AspNetCore.Mvc" && name.is_none() {
                        mvc_using = Some(span(n));
                    } else if let Some(alias) = name {
                        add(text(alias, source).into(), t);
                    }
                }
            }
            if n.kind() == "import_from_statement"
                && module_scope(n)
                && field(n, "module_name").is_some_and(|x| text(x, source) == "fastapi")
            {
                let mut c = n.walk();
                for name in n.children_by_field_name("name", &mut c) {
                    if name.kind() == "aliased_import" {
                        if let (Some(original), Some(alias)) =
                            (field(name, "name"), field(name, "alias"))
                        {
                            add(
                                text(alias, source).into(),
                                format!("fastapi.{}", text(original, source)),
                            );
                        }
                    } else {
                        add(
                            text(name, source).into(),
                            format!("fastapi.{}", text(name, source)),
                        );
                    }
                }
            }
        },
        &ast,
    );
    if import_overflow {
        return Err("qualification context limit".into());
    }
    let mut receivers: BTreeMap<String, (Binding, Span)> = BTreeMap::new();
    walk(
        |n| {
            if n.kind() == "assignment"
                && module_scope(n)
                && let (Some(left), Some(right)) = (field(n, "left"), field(n, "right"))
                && left.kind() == "identifier"
                && right.kind() == "call"
                && let Some(f) = field(right, "function")
            {
                let key = text(f, source);
                if let Some(b) = imports.get(key)
                    && matches!(b.target.as_str(), "fastapi.FastAPI" | "fastapi.APIRouter")
                    && !duplicate_imports.contains(key)
                    && assignments.get(key).copied().unwrap_or(0) == 0
                    && b.at.end_byte < n.start_byte()
                {
                    receivers.insert(
                        text(left, source).into(),
                        (
                            Binding {
                                target: b.target.clone(),
                                at: span(n),
                            },
                            b.at.clone(),
                        ),
                    );
                }
            }
        },
        &ast,
    );
    let mut entries = Vec::new();
    let mut total = 0usize;
    let mut syntax_nodes = 0usize;
    walk(
        |n| {
            syntax_nodes += 1;
            let mut framework = "";
            let mut rule = "";
            let mut declaration = "";
            let mut method: Option<String> = None;
            let mut route_node = None;
            let mut qualification = "unqualified-symbol";
            let mut evidence = Vec::new();
            let mut reasons = Vec::new();
            if r.language == "Java" && matches!(n.kind(), "annotation" | "marker_annotation") {
                let Some(name) = field(n, "name") else { return };
                let name = text(name, source);
                let short = name.rsplit('.').next().unwrap_or(name);
                method = match short {
                    "GetMapping" => Some("GET".into()),
                    "PostMapping" => Some("POST".into()),
                    "PutMapping" => Some("PUT".into()),
                    "DeleteMapping" => Some("DELETE".into()),
                    "PatchMapping" => Some("PATCH".into()),
                    "RequestMapping" => None,
                    _ => return,
                };
                framework = "spring";
                rule = "spring-mapping-annotation";
                declaration = "annotation";
                let expected = format!("org.springframework.web.bind.annotation.{short}");
                if name == expected {
                    qualification = "fully-qualified-symbol"
                } else if local_types.contains(short) || duplicate_imports.contains(short) {
                    qualification = "shadowed-or-ambiguous-symbol"
                } else if let Some(b) = imports.get(short) {
                    evidence.push(b.at.clone());
                    qualification = if b.target == expected {
                        "explicit-import"
                    } else {
                        "foreign-symbol"
                    };
                }
                if let Some(args) = field(n, "arguments") {
                    for i in 0..args.named_child_count() {
                        let a = args.named_child(i as u32).unwrap();
                        if a.kind() == "element_value_pair" {
                            if field(a, "key")
                                .is_some_and(|k| matches!(text(k, source), "value" | "path"))
                            {
                                if route_node.is_some() {
                                    reasons.push("multiple-route-expressions");
                                }
                                route_node = field(a, "value");
                            } else {
                                reasons.push("additional-mapping-constraints");
                            }
                        } else {
                            route_node = Some(a);
                        }
                    }
                }
                reasons.push("type-and-method-routes-not-composed");
            } else if r.language == "C#" && n.kind() == "attribute" {
                let Some(name) = field(n, "name") else { return };
                let name = text(name, source).replace("global::", "");
                let short = name.rsplit('.').next().unwrap_or(&name);
                let imported = imports.get(&name);
                let effective = imported
                    .map(|b| b.target.rsplit('.').next().unwrap_or(&b.target))
                    .unwrap_or(short)
                    .trim_end_matches("Attribute");
                method = match effective {
                    "HttpGet" => Some("GET".into()),
                    "HttpPost" => Some("POST".into()),
                    "HttpPut" => Some("PUT".into()),
                    "HttpDelete" => Some("DELETE".into()),
                    "HttpPatch" => Some("PATCH".into()),
                    "HttpHead" => Some("HEAD".into()),
                    "HttpOptions" => Some("OPTIONS".into()),
                    "Route" => None,
                    _ => return,
                };
                framework = "aspnet-core";
                rule = "aspnet-controller-attribute";
                declaration = "attribute";
                let expected = format!("Microsoft.AspNetCore.Mvc.{effective}");
                if name.trim_end_matches("Attribute") == expected {
                    qualification = "fully-qualified-symbol"
                } else if local_types.contains(short)
                    || local_types.contains(&format!("{short}Attribute"))
                    || duplicate_imports.contains(&name)
                {
                    qualification = "shadowed-or-ambiguous-symbol"
                } else if let Some(b) = imported {
                    evidence.push(b.at.clone());
                    qualification = if b.target.trim_end_matches("Attribute") == expected {
                        "explicit-import-alias"
                    } else {
                        "foreign-symbol"
                    };
                } else if let Some(at) = mvc_using.as_ref() {
                    qualification = "namespace-import-candidate";
                    evidence.push(at.clone());
                    reasons.push("namespace-type-resolution-not-performed");
                }
                if let Some(args) = kind_child(n, "attribute_argument_list")
                    && let Some(arg) = args.named_child(0)
                {
                    if field(arg, "name").is_none() {
                        route_node = arg.named_child(0);
                    } else {
                        reasons.push("named-constructor-or-property-argument");
                    }
                }
                reasons.push("controller-inheritance-and-prefix-not-composed");
            } else if r.language == "C#" && n.kind() == "invocation_expression" {
                let Some(f) = field(n, "function") else {
                    return;
                };
                if f.kind() != "member_access_expression" {
                    return;
                }
                let Some(name) = field(f, "name") else { return };
                method = match text(name, source) {
                    "MapGet" => Some("GET".into()),
                    "MapPost" => Some("POST".into()),
                    "MapPut" => Some("PUT".into()),
                    "MapDelete" => Some("DELETE".into()),
                    "MapPatch" => Some("PATCH".into()),
                    "MapMethods" => None,
                    _ => return,
                };
                framework = "aspnet-core";
                rule = "aspnet-registration-call";
                declaration = "registration-call";
                qualification = "receiver-type-unresolved";
                reasons.push("extension-method-resolution-not-performed");
                route_node = field(n, "arguments")
                    .and_then(|a| a.named_child(0))
                    .and_then(|a| a.named_child(0));
            } else if r.language == "Python" && n.kind() == "decorator" {
                let Some(call) = n.named_child(0).filter(|v| v.kind() == "call") else {
                    return;
                };
                let Some(f) = field(call, "function").filter(|v| v.kind() == "attribute") else {
                    return;
                };
                let Some(name) = field(f, "attribute") else {
                    return;
                };
                method = match text(name, source) {
                    "get" | "post" | "put" | "delete" | "patch" | "head" | "options" => {
                        Some(text(name, source).to_uppercase())
                    }
                    _ => return,
                };
                framework = "fastapi";
                rule = "fastapi-operation-decorator";
                declaration = "decorator";
                qualification = "receiver-type-unresolved";
                if let Some(receiver) = field(f, "object").filter(|v| v.kind() == "identifier") {
                    let key = text(receiver, source);
                    if let Some((b, import_at)) = receivers.get(key) {
                        evidence.push(import_at.clone());
                        evidence.push(b.at.clone());
                        qualification = if assignments.get(key) == Some(&1)
                            && module_scope(n)
                            && b.at.end_byte < n.start_byte()
                        {
                            "same-file-constructor-import"
                        } else {
                            "rebound-or-nested-receiver"
                        };
                    }
                }
                route_node = field(call, "arguments").and_then(|a| a.named_child(0));
                reasons.push("router-inclusion-and-prefix-not-composed");
            }
            if framework.is_empty() {
                return;
            }
            total += 1;
            if entries.len() == MAX_ENTRIES {
                return;
            }
            let (route_literal, route_reason) = literal(route_node, source);
            if let Some(reason) = route_reason {
                reasons.push(reason)
            }
            let qualified = matches!(
                qualification,
                "fully-qualified-symbol" | "explicit-import" | "explicit-import-alias"
            );
            if r.language == "Python" {
                reasons.push("python-receiver-binding-not-proven");
            }
            if !qualified {
                reasons.push("framework-identity-not-proven");
                method = None;
            }
            reasons.push("runtime-registration-not-evaluated");
            if root.has_error() {
                reasons.push("syntax-recovery")
            }
            let unresolved = !qualified
                || route_reason.is_some()
                || root.has_error()
                || reasons.contains(&"multiple-route-expressions");
            reasons.sort_unstable();
            reasons.dedup();
            entries.push(Entry {
                index: total,
                framework_candidate: framework,
                rule,
                rule_version: "0.0.1-experiment",
                declaration,
                scope: owner(n),
                span: span(n),
                method,
                route_literal,
                qualification,
                evidence,
                status: if unresolved { "unresolved" } else { "declared" },
                reasons,
            });
        },
        &ast,
    );
    let evidence_ns = begin.elapsed().as_nanos();
    let begin = Instant::now();
    let metrics = ast.metrics(MetricsOptions::default())?;
    let metrics_ns = begin.elapsed().as_nanos();
    println!(
        "{}",
        json!({"experiment":"http-declarations","provider":"big-code-analysis@2.2.0","tree_sitter":"0.26.12","schema_version":"0.0.1","path":r.path,"language":r.language,"source_bytes":source.len(),"span_convention":"zero-based-half-open-bytes;one-based-lines","scope":"single-file-syntactic-declarations","coverage":"supported-spellings-only;not-all-http-apis","status":if root.has_error()||total>MAX_ENTRIES{"partial"}else{"complete"},"syntax_errors":root.has_error(),"parse_count":parse_count,"same_tree_after_metrics":root.id()==ast.as_tree_sitter().root_node().id(),"metrics_computed":true,"metrics_space_kind":format!("{:?}",metrics.kind),"syntax_nodes":syntax_nodes,"entry_limit":MAX_ENTRIES,"total_candidates":total,"omitted_candidates":total-entries.len(),"entries":entries,"timings_ns":{"parse":parse_ns,"bounds":bounds_ns,"evidence":evidence_ns,"metrics":metrics_ns}})
    );
    Ok(())
}
