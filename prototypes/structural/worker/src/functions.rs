//! Bounded evidence from BCA function spaces; these are syntax-derived spaces,
//! not compiler-resolved functions or threshold-based quality judgments.

use big_code_analysis::{CodeMetrics, FuncSpace, SpaceKind};
use serde::Serialize;

pub const LIMIT: usize = 128;
pub const NAME_MAX_BYTES: usize = 256;

#[derive(Debug, Serialize)]
pub struct Entry<'a> {
    index: usize,
    #[serde(skip_serializing_if = "Option::is_none")]
    name: Option<&'a str>,
    name_status: &'static str,
    start_line: usize,
    end_line: usize,
    metrics: &'a CodeMetrics,
}

#[derive(Debug, Serialize)]
pub struct Report<'a> {
    provider: &'static str,
    rule: &'static str,
    rule_version: &'static str,
    scope: &'static str,
    status: &'static str,
    syntax_errors: bool,
    metric_scope: &'static str,
    order: &'static str,
    limit: usize,
    name_max_bytes: usize,
    total_spaces: usize,
    omitted_spaces: usize,
    invalid_span_spaces: usize,
    entries: Vec<Entry<'a>>,
}

pub(crate) fn name(value: Option<&str>) -> (Option<&str>, &'static str) {
    match value {
        None | Some("") => (None, "unavailable"),
        Some(value) if value.len() > NAME_MAX_BYTES => (None, "omitted"),
        Some(value) if value.chars().any(char::is_control) => (None, "omitted"),
        Some(value) => (Some(value), "present"),
    }
}

pub fn collect(space: &FuncSpace, source_lines: usize, syntax_errors: bool) -> Report<'_> {
    let mut report = Report {
        provider: "big-code-analysis@2.2.0",
        rule: "space-kind-function",
        rule_version: "1.0.0",
        scope: "file",
        status: "complete",
        syntax_errors,
        metric_scope: "includes_nested_spaces",
        order: "provider_preorder",
        limit: LIMIT,
        name_max_bytes: NAME_MAX_BYTES,
        total_spaces: 0,
        omitted_spaces: 0,
        invalid_span_spaces: 0,
        entries: Vec::new(),
    };
    // Iterator frames use depth-sized traversal memory. Do not clone the BCA
    // space tree or collect all siblings just to retain the first 128 entries.
    let mut stack = vec![std::slice::from_ref(space).iter()];
    while let Some(frame) = stack.last_mut() {
        let Some(current) = frame.next() else {
            stack.pop();
            continue;
        };
        if current.kind == SpaceKind::Function {
            report.total_spaces += 1;
            if current.start_line == 0
                || current.end_line < current.start_line
                || current.end_line > source_lines
            {
                report.invalid_span_spaces += 1;
            } else if report.entries.len() == LIMIT {
                report.omitted_spaces += 1;
            } else {
                let (name, name_status) = name(current.name.as_deref());
                if name_status == "omitted" {
                    report.status = "partial";
                }
                report.entries.push(Entry {
                    index: report.total_spaces,
                    name,
                    name_status,
                    start_line: current.start_line,
                    end_line: current.end_line,
                    metrics: &current.metrics,
                });
            }
        }
        if !current.spaces.is_empty() {
            stack.push(current.spaces.iter());
        }
    }
    if syntax_errors || report.omitted_spaces > 0 || report.invalid_span_spaces > 0 {
        report.status = "partial";
    }
    report
}

#[cfg(test)]
mod tests {
    use super::*;
    use big_code_analysis::{Ast, LANG, MetricsOptions, Source};

    fn parse(language: LANG, source: &str) -> FuncSpace {
        Ast::parse(Source::from_bytes(language, source.as_bytes().to_vec()))
            .unwrap()
            .metrics(MetricsOptions::default())
            .unwrap()
    }

    #[test]
    fn bounded_entries_keep_exact_totals_and_qualified_spans() {
        let source = (0..140)
            .map(|i| format!("def function_{i}():\n    return {i}\n"))
            .collect::<String>();
        let mut space = parse(LANG::Python, &source);
        assert_eq!(space.spaces.len(), 140);
        space.spaces[1].start_line = 0;
        // A terminal LF does not create an additional source line for spans.
        assert!(source.ends_with('\n'));
        space.spaces[2].end_line = source.lines().count() + 1;
        let report = collect(&space, source.lines().count(), false);
        assert_eq!(report.total_spaces, 140);
        assert_eq!(report.entries.len(), 128);
        assert_eq!(report.invalid_span_spaces, 2);
        assert_eq!(report.omitted_spaces, 10);
        assert_eq!(
            report.total_spaces,
            report.entries.len() + report.omitted_spaces + report.invalid_span_spaces
        );
        assert_eq!(report.status, "partial");
        assert_eq!(report.entries[0].name, Some("function_0"));
        assert_eq!(report.entries[1].name, Some("function_3"));
        assert_eq!(report.entries[1].index, 4);
    }

    #[test]
    fn utf8_name_bounds_do_not_truncate_identifiers() {
        assert_eq!(name(None), (None, "unavailable"));
        assert_eq!(name(Some("\nmisleading")), (None, "omitted"));
        let limit = "λ".repeat(NAME_MAX_BYTES / 2);
        assert_eq!(name(Some(&limit)), (Some(limit.as_str()), "present"));
        let over = limit + "λ";
        assert_eq!(name(Some(&over)), (None, "omitted"));
    }

    #[test]
    fn nested_classes_do_not_create_member_metric_blocks_on_functions() {
        let source = "def outer():\n    class Nested:\n        def member(self):\n            return 1\n    return Nested\n";
        let space = parse(LANG::Python, source);
        let report = collect(&space, source.lines().count(), false);
        assert_eq!(report.total_spaces, 2);
        for entry in report.entries {
            let metrics = serde_json::to_value(entry.metrics).unwrap();
            assert!(metrics.get("npm").is_none());
            assert!(metrics.get("npa").is_none());
            assert!(metrics.get("wmc").is_none());
        }
    }

    #[test]
    fn nested_function_metrics_are_upstream_aggregates() {
        let source = "def outer(x):\n    def inner(y):\n        if y:\n            return 1\n        return 0\n    return inner(x)\n";
        let space = parse(LANG::Python, source);
        let report = collect(&space, source.lines().count(), false);
        assert_eq!(report.total_spaces, 2);
        assert_eq!(report.entries[0].name, Some("outer"));
        assert_eq!(report.entries[1].name, Some("inner"));
        assert_eq!(report.metric_scope, "includes_nested_spaces");
        // The parent metric aggregation contains the child space. Adding both
        // values would double count; this output intentionally does not sum them.
        let outer = serde_json::to_value(report.entries[0].metrics).unwrap();
        let inner = serde_json::to_value(report.entries[1].metrics).unwrap();
        assert_eq!(
            outer,
            serde_json::to_value(&space.spaces[0].metrics).unwrap()
        );
        assert_eq!(
            inner,
            serde_json::to_value(&space.spaces[0].spaces[0].metrics).unwrap()
        );
        assert!(
            outer["cyclomatic"]["sum"].as_f64().unwrap()
                > inner["cyclomatic"]["sum"].as_f64().unwrap()
        );
    }
}
