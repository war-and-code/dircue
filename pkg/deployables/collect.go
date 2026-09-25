package deployables

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

// Observe reads candidate files in lexical order and returns deterministic,
// evidence-backed declarations. Candidate paths/readers must come from the same
// selected-source inventory used by the caller's other observers.
func Observe(ctx context.Context, files []Candidate, options Options) (*Report, error) {
	limits := normalizedLimits(options)
	r := &Report{Provider: "dircue", ProviderVersion: ProviderVersion, Status: "complete", Source: options.Source,
		Selection: "supported-static-declarations-in-selected-regular-files", Limits: limits,
		Definitions: []Definition{}, Diagnostics: []Diagnostic{}, Omissions: map[string]int64{}}
	r.Coverage.SelectedFiles = int64(len(files))
	candidates := make([]Candidate, 0)
	for _, f := range files {
		if IsCandidate(f.Path) {
			r.Coverage.CandidateFiles++
			candidates = append(candidates, f)
		}
	}
	slices.SortFunc(candidates, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	if len(candidates) > limits.Files {
		r.omit("file_limit", int64(len(candidates)-limits.Files), "", "Only the lexically first supported declaration candidates were inspected.")
		candidates = candidates[:limits.Files]
	}
	// helmValuesRefs accumulates image references from values.yaml files keyed
	// by the directory that contains them. These are used in the post-pass to
	// enrich Helm chart definitions with image references from co-located values.
	helmValuesRefs := map[string][]Reference{}
	var input int64
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validPath(candidate.Path, limits.StringBytes) {
			r.omit("invalid_path", 1, bounded(candidate.Path), "A candidate path was invalid or exceeded the string limit.")
			continue
		}
		if candidate.Read == nil {
			return nil, errors.New("deployable candidate has no selected-source reader")
		}
		if candidate.Size < 0 || candidate.Size > limits.FileBytes || candidate.Size > limits.InputBytes-input {
			r.omit("read_limit", 1, candidate.Path, "The declaration was not read because a file or total input limit was reached.")
			continue
		}
		content, size, err := candidate.Read(ctx, limits.FileBytes+1)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read selected deployable declaration: %w", err)
		}
		if size != candidate.Size || int64(len(content)) != size || size > limits.FileBytes || size > limits.InputBytes-input {
			r.omit("incomplete_read", 1, candidate.Path, "The selected declaration changed, was incomplete, or exceeded a read limit.")
			continue
		}
		input += size
		r.Coverage.ReadFiles++
		r.Coverage.InspectedBytes += size
		// While processing each candidate, also check for Helm values files so
		// we can enrich Chart.yaml definitions without a second read pass.
		if strings.ToLower(path.Base(candidate.Path)) == "values.yaml" {
			refs := parseHelmValuesRefs(candidate.Path, content)
			if len(refs) > 0 {
				dir := path.Dir(candidate.Path)
				helmValuesRefs[dir] = append(helmValuesRefs[dir], refs...)
			}
		}
		defs, recognized, parseErr := parse(candidate.Path, content)
		if parseErr != nil {
			var limitErr *yamlDocLimitError
			if errors.As(parseErr, &limitErr) {
				// Keep definitions parsed before the limit; record a distinct reason.
				r.omit("yaml_document_limit", 1, candidate.Path, "File has more than 128 YAML documents; only the first 128 were parsed.")
				// fall through and use partial defs if any were recognized
			} else {
				r.omit("parse_error", 1, candidate.Path, parseErr.Error())
				continue
			}
		}
		if !recognized {
			continue
		} // A supported filename alone is never evidence.
		r.Coverage.ParsedFiles++
		digest := fmt.Sprintf("%x", sha256.Sum256(content))
		for i := range defs {
			defs[i].Path = candidate.Path
			defs[i].SourceSHA256 = digest
			defs[i].ID = stableID(defs[i])
			slices.SortFunc(defs[i].Evidence, compareEvidence)
			slices.SortFunc(defs[i].References, compareReference)
			if len(defs[i].References) > limits.References-r.Coverage.RetainedReferences {
				keep := max(0, limits.References-r.Coverage.RetainedReferences)
				r.omit("reference_limit", int64(len(defs[i].References)-keep), candidate.Path, "Some references were omitted at the report limit.")
				defs[i].References = defs[i].References[:keep]
				defs[i].Coverage = "qualified"
			}
			r.Coverage.RetainedReferences += len(defs[i].References)
			if len(r.Definitions) >= limits.Definitions {
				r.omit("definition_limit", int64(len(defs)-i), candidate.Path, "Some definitions were omitted at the report limit.")
				break
			}
			r.Definitions = append(r.Definitions, defs[i])
		}
	}
	// Post-pass: enrich Helm chart definitions with image references from
	// co-located values.yaml files. Chart.yaml is parsed before values.yaml in
	// lexicographic order, so the chart definition already exists at this point.
	if len(helmValuesRefs) > 0 {
		for i := range r.Definitions {
			if r.Definitions[i].Provider != "helm" {
				continue
			}
			dir := path.Dir(r.Definitions[i].Path)
			refs, ok := helmValuesRefs[dir]
			if !ok || len(refs) == 0 {
				continue
			}
			for _, ref := range refs {
				if r.Coverage.RetainedReferences >= limits.References {
					r.omit("reference_limit", 1, r.Definitions[i].Path, "Some Helm image references were omitted at the report limit.")
					break
				}
				r.Definitions[i].References = append(r.Definitions[i].References, ref)
				r.Coverage.RetainedReferences++
			}
			slices.SortFunc(r.Definitions[i].References, compareReference)
			r.Definitions[i].ID = stableID(r.Definitions[i])
		}
	}
	slices.SortFunc(r.Definitions, func(a, b Definition) int { return strings.Compare(a.ID, b.ID) })
	r.Coverage.RetainedDefinitions = len(r.Definitions)
	return r, nil
}

func normalizedLimits(o Options) Limits {
	l := Limits{FileBytes: o.FileBytes, InputBytes: o.InputBytes, Files: o.Files, Definitions: o.Definitions, References: o.References, Diagnostics: o.Diagnostics, StringBytes: o.StringBytes}
	if l.FileBytes <= 0 {
		l.FileBytes = DefaultFileBytes
	}
	if l.InputBytes <= 0 {
		l.InputBytes = DefaultInputBytes
	}
	if l.Files <= 0 {
		l.Files = DefaultFiles
	}
	if l.Definitions <= 0 {
		l.Definitions = DefaultDefinitions
	}
	if l.References <= 0 {
		l.References = DefaultReferences
	}
	if l.Diagnostics <= 0 {
		l.Diagnostics = DefaultDiagnostics
	}
	if l.StringBytes <= 0 {
		l.StringBytes = DefaultStringBytes
	}
	return l
}

func IsCandidate(name string) bool {
	if documentationPath(name) {
		return false
	}
	base, ext := strings.ToLower(path.Base(name)), strings.ToLower(path.Ext(name))
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") || base == "compose.yml" || base == "compose.yaml" ||
		base == "docker-compose.yml" || base == "docker-compose.yaml" || base == ".gitlab-ci.yml" || base == ".gitlab-ci.yaml" ||
		base == "jenkinsfile" || strings.HasPrefix(base, "jenkinsfile.") || base == "chart.yaml" || base == "serverless.yml" || base == "serverless.yaml" || ext == ".tf" {
		return true
	}
	if ext == ".yml" || ext == ".yaml" {
		return true
	}
	// .NET Aspire AppHost entry point: Program.cs in an AppHost project directory.
	if base == "program.cs" && isAppHostDir(path.Dir(name)) {
		return true
	}
	// Maven project descriptor: WAR/EAR packaging declarations.
	if base == "pom.xml" {
		return true
	}
	return false
}

func documentationPath(name string) bool {
	first := strings.ToLower(strings.SplitN(name, "/", 2)[0])
	switch first {
	case "doc", "docs", "documentation", "example", "examples", "sample", "samples":
		return true
	default:
		return false
	}
}

func validPath(name string, limit int) bool {
	return name != "" && len(name) <= limit && utf8.ValidString(name) && path.Clean(name) == name && name != "." && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../")
}

func (r *Report) omit(kind string, n int64, file, message string) {
	if n <= 0 {
		return
	}
	r.Status = "partial"
	r.Omissions[kind] += n
	r.Coverage.OmittedFiles += n
	if len(r.Diagnostics) < r.Limits.Diagnostics {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: file, Code: kind, Message: message})
	} else {
		r.Omissions["diagnostic_limit"]++
	}
}

func stableID(d Definition) string { return d.Kind + ":" + d.Provider + ":" + d.Path + "#" + d.Name }
func compareEvidence(a, b Evidence) int {
	return strings.Compare(fmt.Sprintf("%08d\x00%s\x00%s", a.Line, a.Field, a.Value), fmt.Sprintf("%08d\x00%s\x00%s", b.Line, b.Field, b.Value))
}
func compareReference(a, b Reference) int {
	return strings.Compare(a.Kind+"\x00"+a.Value+"\x00"+a.Qualification, b.Kind+"\x00"+b.Value+"\x00"+b.Qualification)
}
