//! Exact bounded distributions over all valid BCA Function-kind spaces.

use big_code_analysis::{FuncSpace, SpaceKind};
use serde::Serialize;

pub const LIMIT: usize = 10;
const BUCKETS: usize = 65;

#[derive(Debug, Serialize)]
pub struct Entry<'a> {
    index: usize,
    #[serde(skip_serializing_if = "Option::is_none")]
    name: Option<&'a str>,
    name_status: &'static str,
    start_line: usize,
    end_line: usize,
    value: u64,
}

#[derive(Debug, Serialize)]
pub struct Metric<'a> {
    metric: &'static str,
    count: usize,
    min: Option<u64>,
    max: Option<u64>,
    histogram: Vec<u64>,
    top: Vec<Entry<'a>>,
}

impl<'a> Metric<'a> {
    fn new(metric: &'static str) -> Self {
        Self {
            metric,
            count: 0,
            min: None,
            max: None,
            histogram: vec![0; BUCKETS],
            top: Vec::new(),
        }
    }
    fn add(&mut self, space: &'a FuncSpace, index: usize, value: u64) {
        self.count += 1;
        self.min = Some(self.min.map_or(value, |v| v.min(value)));
        self.max = Some(self.max.map_or(value, |v| v.max(value)));
        self.histogram[(u64::BITS - value.leading_zeros()) as usize] += 1;
        let insertion = self.top.partition_point(|entry| entry.value >= value);
        if insertion >= LIMIT {
            return;
        }
        let (name, name_status) = super::functions::name(space.name.as_deref());
        self.top.insert(
            insertion,
            Entry {
                index,
                name,
                name_status,
                start_line: space.start_line,
                end_line: space.end_line,
                value,
            },
        );
        self.top.truncate(LIMIT);
    }
}

#[derive(Debug, Serialize)]
pub struct Report<'a> {
    provider: &'static str,
    rule: &'static str,
    rule_version: &'static str,
    syntax_errors: bool,
    total_spaces: usize,
    invalid_span_spaces: usize,
    metrics: Vec<Metric<'a>>,
}

pub fn collect(space: &FuncSpace, source_lines: usize, syntax_errors: bool) -> Report<'_> {
    let mut report = Report {
        provider: "big-code-analysis@2.2.0",
        rule: "function-population",
        rule_version: "1.0.0",
        syntax_errors,
        total_spaces: 0,
        invalid_span_spaces: 0,
        metrics: vec![Metric::new("cyclomatic_sum"), Metric::new("span_lines")],
    };
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
            } else {
                report.metrics[0].add(
                    current,
                    report.total_spaces,
                    current.metrics.cyclomatic.cyclomatic_sum(),
                );
                report.metrics[1].add(
                    current,
                    report.total_spaces,
                    (current.end_line - current.start_line + 1) as u64,
                );
            }
        }
        if !current.spaces.is_empty() {
            stack.push(current.spaces.iter());
        }
    }
    report
}

#[cfg(test)]
mod tests {
    use super::*;
    use big_code_analysis::{Ast, LANG, MetricsOptions, Source};
    fn parse(source: &str) -> FuncSpace {
        Ast::parse(Source::from_bytes(LANG::Python, source.as_bytes().to_vec()))
            .unwrap()
            .metrics(MetricsOptions::default())
            .unwrap()
    }
    #[test]
    fn selection_covers_late_functions_beyond_old_retention_limit() {
        let mut source = (0..140)
            .map(|i| format!("def small_{i}():\n    return 0\n"))
            .collect::<String>();
        source.push_str("def late(x):\n");
        for i in 0..40 {
            source.push_str(&format!("    if x == {i}:\n        return {i}\n"));
        }
        let tree = parse(&source);
        let report = collect(&tree, source.lines().count(), false);
        assert_eq!(report.total_spaces, 141);
        for metric in &report.metrics {
            assert_eq!(metric.count, 141);
            assert_eq!(metric.histogram.iter().sum::<u64>(), 141);
            assert_eq!(metric.top.len(), LIMIT);
            assert_eq!(metric.top[0].name, Some("late"));
            assert_eq!(metric.top[0].index, 141);
            assert_eq!(metric.top[1].index, 1);
        }
    }
    #[test]
    fn exact_buckets_and_extrema_include_invalid_span_exclusion() {
        let source = "def outer(x):\n    def inner(y):\n        if y:\n            return 1\n        return 0\n    return inner(x)\n";
        let mut tree = parse(source);
        let report = collect(&tree, source.lines().count(), true);
        assert!(report.syntax_errors);
        assert_eq!(
            report.metrics[0].top[0].value,
            tree.spaces[0].metrics.cyclomatic.cyclomatic_sum()
        );
        assert_eq!(report.metrics[1].min, Some(4));
        assert_eq!(report.metrics[1].max, Some(6));
        assert_eq!(report.metrics[1].histogram[3], 2);
        tree.spaces[0].start_line = 0;
        let report = collect(&tree, source.lines().count(), false);
        assert_eq!(report.total_spaces, 2);
        assert_eq!(report.invalid_span_spaces, 1);
        assert_eq!(report.metrics[0].count, 1);
        assert_eq!(report.metrics[0].top[0].index, 2);
    }
    #[test]
    fn all_uint64_bucket_boundaries_are_exact() {
        let source = "def f():\n    return 0\n";
        let tree = parse(source);
        let mut metric = Metric::new("test");
        metric.add(&tree.spaces[0], 1, 0);
        for shift in 0..64 {
            metric.add(&tree.spaces[0], shift + 2, 1 << shift);
        }
        assert!(metric.histogram.iter().all(|count| *count == 1));
        metric.add(&tree.spaces[0], 66, u64::MAX);
        assert_eq!(metric.histogram[64], 2);
        assert_eq!(metric.max, Some(u64::MAX));
        assert_eq!(metric.min, Some(0));
    }

    // Full sorting and division deliberately avoid the production insertion and
    // leading-zero algorithms. The input index is assigned in traversal order.
    fn assert_metric_oracle(metric: &Metric<'_>, values: &[(usize, u64)]) {
        let mut histogram = vec![0; 65];
        for &(_, mut value) in values {
            let mut bucket = 0;
            while value != 0 {
                value /= 2;
                bucket += 1;
            }
            histogram[bucket] += 1;
        }
        let mut expected = values.to_vec();
        expected.sort_by_key(|&(index, value)| (std::cmp::Reverse(value), index));
        expected.truncate(10);
        let actual: Vec<_> = metric.top.iter().map(|e| (e.index, e.value)).collect();
        assert_eq!(metric.count, values.len());
        assert_eq!(metric.min, values.iter().map(|v| v.1).min());
        assert_eq!(metric.max, values.iter().map(|v| v.1).max());
        assert_eq!(metric.histogram, histogram);
        assert_eq!(actual, expected);
    }

    fn next_generated(state: &mut u64) -> u64 {
        *state = state
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        *state
    }

    #[test]
    fn generated_metric_matches_full_population_sort_and_histogram() {
        let tree = parse("def f():\n    return 0\n");
        let space = &tree.spaces[0];
        for seed in 0..128 {
            let mut state = seed;
            let mut values = vec![0, u64::MAX];
            for bit in 0..64 {
                let boundary = 1u64 << bit;
                values.extend([boundary - 1, boundary, boundary.saturating_add(1)]);
            }
            for _ in 0..(seed * 5) {
                // Small values deliberately create ties at the retention boundary.
                let random = next_generated(&mut state);
                values.push(if random % 3 == 0 { random % 16 } else { random });
            }
            for order in 0..3 {
                if order == 1 {
                    values.reverse();
                } else if order == 2 {
                    for i in (1..values.len()).rev() {
                        let j = next_generated(&mut state) as usize % (i + 1);
                        values.swap(i, j);
                    }
                }
                let mut metric = Metric::new("test");
                let indexed: Vec<_> = values
                    .iter()
                    .enumerate()
                    .map(|(i, &v)| (i + 1, v))
                    .collect();
                for &(index, value) in &indexed {
                    metric.add(space, index, value);
                }
                assert_metric_oracle(&metric, &indexed);
            }
        }
        // Every retention transition is exercised separately, including empty.
        for count in 0..=21 {
            let mut metric = Metric::new("test");
            let indexed: Vec<_> = (1..=count).map(|i| (i, 7)).collect();
            for &(index, value) in &indexed {
                metric.add(space, index, value);
            }
            assert_metric_oracle(&metric, &indexed);
        }
    }

    #[test]
    fn generated_collector_matches_recursive_population_oracle() {
        fn flatten<'a>(node: &'a FuncSpace, out: &mut Vec<&'a FuncSpace>) {
            if node.kind == SpaceKind::Function {
                out.push(node);
            }
            for child in &node.spaces {
                flatten(child, out);
            }
        }
        let template = parse("def f(x):\n    if x:\n        return 1\n    return 0\n");
        let function = template.spaces[0].clone();
        for seed in 0..96 {
            let mut state = seed;
            let mut tree = template.clone();
            tree.spaces.clear();
            // Four levels, with non-function containers and invalid ancestors.
            // A child remains eligible even if its enclosing function is invalid.
            for i in 0..(seed % 17 + 1) {
                let mut outer = function.clone();
                outer.spaces.clear();
                if i % 3 == 0 {
                    outer.kind = tree.kind;
                }
                for j in 0..(seed % 13 + 1) {
                    let mut child = function.clone();
                    let draw = next_generated(&mut state);
                    child.start_line = (draw % 60) as usize;
                    child.end_line = child.start_line + (draw % 12) as usize;
                    if j % 4 == 0 {
                        child.end_line = child.start_line.saturating_sub(1);
                    }
                    if j % 3 == 0 {
                        let mut grandchild = function.clone();
                        grandchild.spaces = vec![function.clone()];
                        child.spaces.push(grandchild);
                    }
                    outer.spaces.push(child);
                }
                tree.spaces.push(outer);
            }
            for source_lines in [0, 1, 4, 32, 64] {
                let report = collect(&tree, source_lines, seed % 2 == 0);
                let mut all = Vec::new();
                flatten(&tree, &mut all);
                let valid: Vec<_> = all
                    .iter()
                    .enumerate()
                    .filter(|(_, node)| {
                        node.start_line >= 1
                            && node.end_line >= node.start_line
                            && node.end_line <= source_lines
                    })
                    .collect();
                assert_eq!(report.total_spaces, all.len());
                assert_eq!(report.invalid_span_spaces, all.len() - valid.len());
                assert_eq!(report.syntax_errors, seed % 2 == 0);
                let cyclomatic: Vec<_> = valid
                    .iter()
                    .map(|(i, node)| (i + 1, node.metrics.cyclomatic.cyclomatic_sum()))
                    .collect();
                let spans: Vec<_> = valid
                    .iter()
                    .map(|(i, node)| (i + 1, (node.end_line - node.start_line + 1) as u64))
                    .collect();
                assert_metric_oracle(&report.metrics[0], &cyclomatic);
                assert_metric_oracle(&report.metrics[1], &spans);
                for metric in &report.metrics {
                    for entry in &metric.top {
                        let source = all[entry.index - 1];
                        assert_eq!(entry.start_line, source.start_line);
                        assert_eq!(entry.end_line, source.end_line);
                        assert_eq!(entry.name, source.name.as_deref());
                        assert_eq!(entry.name_status, "present");
                    }
                }
            }
        }
    }
}
