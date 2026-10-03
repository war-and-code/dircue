package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/structure"
)

// Build makes a deterministic plan from caller-validated report data.
func Build(ctx context.Context, in Input) (*Report, error) {
	if err := validateInput(in); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mods := map[string]capabilities.Module{}
	questions := map[string]string{}
	for _, m := range in.Capabilities.Modules {
		mods[m.ID], questions[m.Question] = m, m.ID
	}
	requested := map[string]string{}
	for _, id := range in.Selection.Modules {
		requested[id] = mods[id].Question
	}
	for _, q := range in.Selection.Questions {
		requested[questions[q]] = q
	}
	ids := make([]string, 0, len(requested))
	for id := range requested {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	inputSet := map[string]bool{}
	for _, v := range in.Selection.Inputs {
		inputSet[v] = true
	}
	projects := cleanSorted(in.Selection.Projects)
	source := sourceIdentity(in.Profile)
	r := &Report{SchemaVersion: SchemaVersion, Kind: "dircue-follow-up-plan", Status: "complete",
		Identity: Identity{ReportSHA256: in.ReportSHA256, ProfileSchemaVersion: in.Profile.SchemaVersion, DeclaredRoot: in.Profile.Root, CapabilitySchemaVersion: in.Capabilities.SchemaVersion, CapabilityProvider: in.Capabilities.Provider, CapabilityVersion: in.Capabilities.ProviderVersion, Source: source},
		Limits:   Limits{MaxRequests, MaxProjects, MaxEvidencePerStep, MaxEvidence, MaxKeyBytes, MaxOutputBytes}, Evidence: []Evidence{}, Decisions: []Decision{}, Steps: []Step{}}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m := mods[id]
		d, step, evidence, err := decide(in, m, projects, inputSet, source)
		if err != nil {
			return nil, err
		}
		if len(evidence) > MaxEvidencePerStep {
			evidence = evidence[:MaxEvidencePerStep]
			d.Reasons = append(d.Reasons, "supporting evidence was capped")
			r.Status = "partial"
		}
		remaining := MaxEvidence - len(r.Evidence)
		if remaining < len(evidence) {
			if remaining < 0 {
				remaining = 0
			}
			evidence = evidence[:remaining]
			d.Reasons = append(d.Reasons, "plan evidence was capped")
			r.Status = "partial"
		}
		for _, e := range evidence {
			d.SupportingObservationIDs = append(d.SupportingObservationIDs, e.ID)
		}
		if step != nil {
			step.SupportingObservationIDs = slices.Clone(d.SupportingObservationIDs)
			r.Steps = append(r.Steps, *step)
		}
		r.Decisions = append(r.Decisions, d)
		r.Evidence = append(r.Evidence, evidence...)
	}
	r.Evidence = dedupEvidence(r.Evidence)
	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(encoded) > MaxOutputBytes {
		return nil, ErrLimit
	}
	return r, nil
}

func validateInput(in Input) error {
	if in.Profile == nil || !isDigest(in.ReportSHA256) || !slices.Contains([]string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0", "1.6.0", "1.7.0", "1.8.0"}, in.Profile.SchemaVersion) || len(in.Profile.Root) > MaxReportedRootBytes || in.Capabilities.Validate() != nil {
		return ErrInvalid
	}
	if len(in.Selection.Modules)+len(in.Selection.Questions) > MaxRequests || len(in.Selection.Inputs) > MaxRequests || len(in.Selection.Projects) > MaxProjects {
		return ErrLimit
	}
	if len(in.Selection.Modules)+len(in.Selection.Questions) == 0 {
		return ErrInvalid
	}
	validModules, validQuestions := map[string]bool{}, map[string]bool{}
	questionModule := map[string]string{}
	for _, m := range in.Capabilities.Modules {
		validModules[m.ID], validQuestions[m.Question] = true, true
		questionModule[m.Question] = m.ID
	}
	for _, values := range [][]string{in.Selection.Modules, in.Selection.Questions, in.Selection.Projects, in.Selection.Inputs} {
		for _, v := range values {
			if v == "" || len(v) > MaxKeyBytes {
				return ErrLimit
			}
		}
	}
	for _, v := range in.Selection.Projects {
		if path.IsAbs(v) || path.Clean(v) != v || v == "." || v == ".." || strings.HasPrefix(v, "-") || strings.HasPrefix(v, "../") || strings.Contains(v, "/../") || strings.ContainsAny(v, "\\\x00\r\n") {
			return ErrInvalid
		}
	}
	for _, v := range in.Selection.Modules {
		if !validModules[v] {
			return ErrInvalid
		}
	}
	for _, v := range in.Selection.Questions {
		if !validQuestions[v] {
			return ErrInvalid
		}
	}
	requested := map[string]bool{}
	for _, id := range in.Selection.Modules {
		requested[id] = true
	}
	for _, q := range in.Selection.Questions {
		requested[questionModule[q]] = true
	}
	if len(in.Selection.Projects) > 0 && !requested["focus"] {
		return ErrInvalid
	}
	allowedInputs := map[string]bool{}
	for _, m := range in.Capabilities.Modules {
		if requested[m.ID] {
			for _, name := range m.RequiredInputs {
				if name != "source" && name != "project" {
					allowedInputs[name] = true
				}
			}
		}
	}
	for _, name := range in.Selection.Inputs {
		if !allowedInputs[name] {
			return ErrInvalid
		}
	}
	return nil
}

func decide(in Input, m capabilities.Module, projects []string, inputs map[string]bool, source SourceIdentity) (Decision, *Step, []Evidence, error) {
	d := Decision{ID: "decision:" + m.ID, Question: m.Question, Module: m.ID, Applicability: "proposed", Reasons: []string{}, SupportingObservationIDs: []string{}, UnresolvedInputs: []string{}}
	scope, ev, overflow := evidenceFor(in.Profile.Discovery, m.ID, in.Profile.Languages, projects)
	if overflow {
		return d, nil, nil, ErrLimit
	}
	presentStatus, sameScope := retainedStatus(in.Profile, m.ID, projects)
	if presentStatus == "complete" && sameScope && source.Status != "conflict" {
		d.Applicability = "already_present"
		d.Reasons = append(d.Reasons, "the saved report already contains this module")
		if source.Status == "unavailable" {
			d.Reasons = append(d.Reasons, "saved source provenance is unavailable; reuse is retained evidence only")
		}
		return d, nil, ev, nil
	}
	if presentStatus != "" {
		d.Applicability = "retained_partial"
		d.Reasons = append(d.Reasons, "retained module evidence is incomplete or has a different requested scope")
	}
	if source.Status == "conflict" {
		d.Applicability = "blocked"
		d.UnresolvedInputs = append(d.UnresolvedInputs, "source-consistency")
		d.Reasons = append(d.Reasons, "retained modules identify conflicting selected sources")
	}
	if in.Profile.Discovery == nil && m.ID != "discovery" {
		d.Reasons = append(d.Reasons, "inventory evidence is absent; applicability remains unresolved rather than skipped")
		scope.EvidenceStatus = "unavailable"
	}
	if in.Profile.Discovery != nil && in.Profile.Discovery.Status != "complete" {
		d.Reasons = append(d.Reasons, "inventory evidence is partial or skipped; candidate absence is not established")
		scope.EvidenceStatus = "partial"
	}
	if m.ID == "focus" && len(projects) == 0 {
		d.Applicability = "blocked"
		d.UnresolvedInputs = append(d.UnresolvedInputs, "project")
		d.Reasons = append(d.Reasons, "a caller-selected project is required")
	}
	if m.ID == "focus" && in.Profile.Declarations == nil {
		d.Applicability = "blocked"
		d.UnresolvedInputs = append(d.UnresolvedInputs, "declarations")
		d.Reasons = append(d.Reasons, "project scope requires declaration evidence")
	}
	if m.ID == "focus" && in.Profile.Declarations != nil && in.Profile.Declarations.Status != "complete" {
		d.Applicability = "blocked"
		d.UnresolvedInputs = append(d.UnresolvedInputs, "complete declarations")
		d.Reasons = append(d.Reasons, "incomplete declarations cannot establish a complete focus selection")
	}
	if m.ID == "focus" && len(projects) == 1 && in.Profile.Declarations != nil {
		eligible := false
		found := false
		for _, p := range in.Profile.Declarations.Projects {
			if p.ID == projects[0] {
				found = true
				eligible = p.Kind == "dotnet" || p.Kind == "python"
				break
			}
		}
		if !found || !eligible {
			d.Applicability = "blocked"
			d.UnresolvedInputs = append(d.UnresolvedInputs, "supported parsed project")
			d.Reasons = append(d.Reasons, "the selected project is absent from retained parsed declarations or unsupported by focus")
		}
	}
	if m.ID == "structure" && !inputs["structural-worker"] {
		d.Applicability = "blocked"
		d.UnresolvedInputs = append(d.UnresolvedInputs, "structural-worker")
		d.Reasons = append(d.Reasons, "worker availability was not supplied by the caller and was not probed")
	}
	if m.ID == "discovery" {
		d.Reasons = append(d.Reasons, "a metadata inventory can qualify later scope and cost decisions")
	}
	if len(ev) == 0 && m.ID != "discovery" {
		if d.Applicability == "proposed" {
			d.Applicability = "unknown"
		}
		d.Reasons = append(d.Reasons, "no retained signal matched this rule; this is not a safe-to-skip conclusion")
	}
	argv := slices.Clone(m.Command.ArgvPrefix)
	if source.Status == "consistent" && (source.Mode == "git" || source.Mode == "directory") {
		argv = append(argv, "--source", source.Mode)
		if source.Mode == "git" && source.Tree != "" {
			argv = append(argv, "--tree", source.Tree)
		}
	}
	if m.ID == "focus" {
		if len(projects) > 0 {
			argv = append(argv, "--project", projects[0])
			for _, p := range projects[1:] {
				argv = append(argv, "--related-project", p)
			}
		}
	}
	if m.ID == "structure" {
		argv = append(argv, "--structural-worker", "{structural-worker}")
	}
	argv = append(argv, "--", "{source}")
	step := &Step{ID: "step:" + m.ID, DecisionID: d.ID, Question: m.Question, Module: m.ID, Scope: scope, Prerequisites: slices.Clone(m.Prerequisites), UnresolvedInputs: slices.Clone(d.UnresolvedInputs), ExpectedEvidence: slices.Clone(m.ExpectedEvidence), SupportingObservationIDs: []string{}, Cost: Cost{Class: costClass(scope, m), Inspection: m.Cost.Inspection, CandidateFiles: scope.CandidateFiles, CandidateBytes: scope.CandidateBytes, ExternalProcess: m.Cost.ExternalProcess, Qualification: "candidate quantities describe retained evidence only; shared traversal, runtime, and memory are not predicted"}, Command: Command{Argv: argv, Executable: false, SourcePlaceholder: "{source}", RevalidationRequired: []string{"source identity", "source boundary", "report freshness"}}}
	return d, step, ev, nil
}

func evidenceFor(d *discovery.Report, module string, languages []profile.Language, projects []string) (Scope, []Evidence, bool) {
	scopeProjects := []string{}
	kind := "whole-selected-source"
	if module == "focus" {
		scopeProjects = slices.Clone(projects)
		kind = "selected-project"
	}
	s := Scope{Kind: kind, Projects: scopeProjects, CandidatePaths: []string{}, EvidenceStatus: "available"}
	evidence := []Evidence{}
	overflow := false
	add := func(files, bytes int64) {
		if files < 0 || bytes < 0 || s.CandidateFiles > int64(^uint64(0)>>1)-files || s.CandidateBytes > int64(^uint64(0)>>1)-bytes {
			overflow = true
			return
		}
		s.CandidateFiles += files
		s.CandidateBytes += bytes
	}
	want := func(c discovery.Candidate) bool {
		switch module {
		case "declarations", "environments":
			return c.Kind == "manifest" || c.Kind == "shared_configuration"
		case "lockfiles":
			return c.Kind == "manifest" && (c.Format == "npm" || c.Format == "dotnet" || c.Format == "npm_lock" || c.Format == "nuget_lock")
		case "focus":
			return c.Kind == "manifest" && slices.Contains(projects, c.Path)
		case "formats":
			return c.Kind == "artifact"
		default:
			return false
		}
	}
	if d != nil {
		if d.Status != "complete" {
			s.EvidenceStatus = "partial"
		}
		for _, c := range d.Candidates {
			if !want(c) {
				continue
			}
			add(1, c.Bytes)
			if len(s.CandidatePaths) < MaxEvidencePerStep {
				s.CandidatePaths = append(s.CandidatePaths, c.Path)
			}
			evidence = append(evidence, Evidence{ID: evidenceID("discovery-candidate", c.Kind+":"+c.Path), Kind: "discovery-candidate", Path: c.Path, Value: c.Kind + ":" + c.Format})
		}
		if module == "formats" {
			for _, g := range d.Categories {
				if g.Name == "data_candidate" {
					add(g.Files, g.Bytes)
					evidence = append(evidence, Evidence{ID: evidenceID("discovery-category", g.Name+":"+g.Basis), Kind: "discovery-category", Value: g.Name + ":" + g.Basis})
				}
			}
		}
		if module == "discovery" {
			evidence = append(evidence, Evidence{ID: evidenceID("profile-summary", "scanned-files"), Kind: "profile-summary", Value: "scanned_files"})
		}
		if module == "availability" {
			add(d.Inventory.Files, d.Inventory.Bytes)
			evidence = append(evidence, Evidence{ID: evidenceID("discovery-inventory", d.Source.Mode+":"+d.Source.Tree), Kind: "discovery-inventory", Value: d.Source.Mode})
		}
	}
	if module == "metrics" || module == "structure" {
		for _, l := range languages {
			if module == "structure" && !structure.Supports(l.Name) {
				continue
			}
			add(l.FileCount, l.Bytes)
			evidence = append(evidence, Evidence{ID: evidenceID("language", l.Name), Kind: "language", Value: l.Name})
		}
	}
	return s, evidence, overflow
}

func retainedStatus(p *profile.Report, module string, projects []string) (string, bool) {
	switch module {
	case "availability":
		if p.Availability != nil {
			return p.Availability.Status, true
		}
	case "declarations":
		if p.Declarations != nil {
			return p.Declarations.Status, true
		}
	case "discovery":
		if p.Discovery != nil {
			return p.Discovery.Status, true
		}
	case "environments":
		if p.Environments != nil {
			return p.Environments.Status, true
		}
	case "lockfiles":
		if p.Lockfiles != nil {
			return p.Lockfiles.Status, true
		}
	case "formats":
		if p.Formats != nil {
			return p.Formats.Status, true
		}
	case "metrics":
		if p.Metrics != nil {
			return p.Metrics.Status, true
		}
	case "structure":
		if p.Structure != nil {
			return p.Structure.Status, true
		}
	case "focus":
		if p.Focus != nil {
			same := len(projects) == 1 && p.Focus.PrimaryProject != nil && p.Focus.PrimaryProject.ID == projects[0] && len(p.Focus.Related) == 0
			return p.Focus.Status, same
		}
	}
	return "", false
}

func sourceIdentity(p *profile.Report) SourceIdentity {
	type pair struct{ mode, tree string }
	values := []pair{}
	add := func(mode, tree string) {
		if mode == "git" || mode == "directory" {
			values = append(values, pair{mode, tree})
		}
	}
	if p.Discovery != nil {
		add(p.Discovery.Source.Mode, p.Discovery.Source.Tree)
	}
	if p.Declarations != nil {
		add(p.Declarations.Source, p.Declarations.Tree)
	}
	if p.Formats != nil {
		add(p.Formats.Source.Mode, p.Formats.Source.Tree)
	}
	if p.Metrics != nil {
		add(p.Metrics.Source, p.Metrics.Tree)
	}
	if p.Structure != nil {
		add(p.Structure.Source, p.Structure.Tree)
	}
	if p.Focus != nil {
		add(p.Focus.Source, p.Focus.Tree)
	}
	if p.Availability != nil {
		add(p.Availability.Source.Mode, p.Availability.Source.Tree)
	}
	if p.Environments != nil {
		add(p.Environments.Source, p.Environments.Tree)
	}
	if p.Lockfiles != nil {
		add(p.Lockfiles.Source, p.Lockfiles.Tree)
	}
	if p.Projects != nil {
		add(p.Projects.Source, p.Projects.Tree)
	}
	if len(values) == 0 {
		return SourceIdentity{Status: "unavailable"}
	}
	first := values[0]
	for _, v := range values[1:] {
		if v != first {
			return SourceIdentity{Status: "conflict"}
		}
	}
	return SourceIdentity{Mode: first.mode, Tree: first.tree, Status: "consistent"}
}

func cleanSorted(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}
func isDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil && strings.ToLower(v) == v
}
func evidenceID(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return "obs:" + kind + ":" + hex.EncodeToString(sum[:8])
}
func costClass(s Scope, m capabilities.Module) string {
	if m.Cost.ExternalProcess {
		return "external-worker"
	}
	if m.Cost.Inspection == "metadata" {
		return "metadata"
	}
	if m.Cost.Inspection == "full-content" {
		return "full-content"
	}
	if s.CandidateFiles == 0 {
		return "bounded-content-unknown-population"
	}
	return "bounded-content"
}
func dedupEvidence(in []Evidence) []Evidence {
	seen := map[string]bool{}
	out := make([]Evidence, 0, len(in))
	for _, e := range in {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Evidence) int { return strings.Compare(a.ID, b.ID) })
	return out
}
