package structure

import (
	"slices"
	"strings"
)

// Capability describes the pinned worker support for a detected language.
// Observations lists available dircue syntax counts, separately from BCA metrics.
type Capability struct {
	Language     string   `json:"language"`
	Grammar      string   `json:"grammar"`
	Observations []string `json:"observations"`
	MetricGroups []string `json:"metric_groups"`
}

var baseMetricGroups = []string{"abc", "cognitive", "cyclomatic", "halstead", "loc", "mi", "nargs", "nexits", "nom", "tokens"}
var memberMetricGroups = append(slices.Clone(baseMetricGroups), "npa", "npm")

var syntaxObservations = []string{"syntax_nodes", "error_nodes", "missing_nodes"}
var declarationObservations = []string{"classes", "interfaces", "records", "structs", "enums", "methods", "constructors", "properties", "imports", "lambdas", "local_functions", "syntax_nodes", "error_nodes", "missing_nodes"}

// Names follow the pinned Enry catalog. JSX uses JavaScript; Bash uses Shell.
// A grammar's existence does not imply that Enry can identify its language.
var capabilities = map[string]Capability{
	"C":           {Language: "C", Grammar: "tree-sitter-c@0.24.2", MetricGroups: baseMetricGroups, Observations: syntaxObservations},
	"C++":         {Language: "C++", Grammar: "tree-sitter-cpp@0.23.4", MetricGroups: metricGroups, Observations: syntaxObservations},
	"C#":          {Language: "C#", Grammar: "tree-sitter-c-sharp@0.23.5", MetricGroups: metricGroups, Observations: declarationObservations},
	"Elixir":      {Language: "Elixir", Grammar: "tree-sitter-elixir@0.3.5", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Go":          {Language: "Go", Grammar: "tree-sitter-go@0.25.0", MetricGroups: memberMetricGroups, Observations: syntaxObservations},
	"Groovy":      {Language: "Groovy", Grammar: "dekobon-tree-sitter-groovy@0.2.2", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Java":        {Language: "Java", Grammar: "tree-sitter-java@0.23.5", MetricGroups: metricGroups, Observations: declarationObservations},
	"JavaScript":  {Language: "JavaScript", Grammar: "tree-sitter-javascript@0.25.0", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Kotlin":      {Language: "Kotlin", Grammar: "tree-sitter-kotlin-ng@1.1.0", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Lua":         {Language: "Lua", Grammar: "tree-sitter-lua@0.5.0", MetricGroups: baseMetricGroups, Observations: syntaxObservations},
	"Objective-C": {Language: "Objective-C", Grammar: "tree-sitter-objc@3.0.2", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Perl":        {Language: "Perl", Grammar: "tree-sitter-perl@1.1.2", MetricGroups: baseMetricGroups, Observations: syntaxObservations},
	"PHP":         {Language: "PHP", Grammar: "tree-sitter-php@0.24.2", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Python":      {Language: "Python", Grammar: "tree-sitter-python@0.25.0", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Ruby":        {Language: "Ruby", Grammar: "tree-sitter-ruby@0.23.1", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Rust":        {Language: "Rust", Grammar: "tree-sitter-rust@0.24.2", MetricGroups: metricGroups, Observations: syntaxObservations},
	"Shell":       {Language: "Shell", Grammar: "tree-sitter-bash@0.25.1", MetricGroups: baseMetricGroups, Observations: syntaxObservations},
	"Tcl":         {Language: "Tcl", Grammar: "bca-tree-sitter-tcl@2.2.0", MetricGroups: baseMetricGroups, Observations: syntaxObservations},
	"TSX":         {Language: "TSX", Grammar: "tree-sitter-typescript@0.23.2", MetricGroups: metricGroups, Observations: syntaxObservations},
	"TypeScript":  {Language: "TypeScript", Grammar: "tree-sitter-typescript@0.23.2", MetricGroups: metricGroups, Observations: syntaxObservations},
}

// Supports reports whether the detected language has an enabled parser.
func Supports(language string) bool {
	_, ok := capabilities[language]
	return ok
}

// Capabilities returns a sorted, independent snapshot of supported languages.
func Capabilities() []Capability {
	result := make([]Capability, 0, len(capabilities))
	for _, c := range capabilities {
		c.Observations = slices.Clone(c.Observations)
		c.MetricGroups = slices.Clone(c.MetricGroups)
		result = append(result, c)
	}
	slices.SortFunc(result, func(a, b Capability) int {
		return strings.Compare(a.Language, b.Language)
	})
	return result
}
