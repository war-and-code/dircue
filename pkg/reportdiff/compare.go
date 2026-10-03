package reportdiff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

// Compare describes saved observations under their declared scope. The caller
// chooses the source pair; equal root strings do not prove repository identity.
func Compare(base, head *Snapshot) (*Report, error) {
	if base == nil || head == nil || !base.validated || !head.validated {
		return nil, ErrInvalid
	}
	r := &Report{SchemaVersion: SchemaVersion, Kind: "dircue-report-comparison", Status: "complete", SourcePairing: "caller-selected; repository identity and report authenticity are not verified", Base: identity("base", base), Head: identity("head", head), Limits: Limits{MaxInputBytes, MaxJSONDepth, MaxJSONNodes, MaxStringBytes, MaxChanges, MaxFieldValueBytes, MaxOutputBytes, MaxEvidencePaths, MaxEvidencePathBytes}, Modules: []Module{}}
	a, b := moduleInputs(base.profile), moduleInputs(head.profile)
	remaining := MaxChanges
	byteBudget := MaxOutputBytes - (1 << 20)
	names := []string{"languages", "summary", "ecosystems", "frameworks", "layouts", "projects", "declarations", "discovery", "registries", "package_evidence", "metrics", "metrics_files", "rules", "graph", "structure"}
	// Keep historical comparison output intact when neither report requests
	// the newer modules. Their presence on just one side is still disclosed.
	for _, name := range []string{"formats", "hotspots"} {
		if a[name].present || b[name].present {
			names = append(names, name)
		}
	}
	for _, name := range []string{"focus_primary", "focus_related", "focus_context", "focus_relations", "focus_affected_projects", "focused_metrics_primary", "focused_metrics_related", "availability_lfs", "availability_gitlinks", "availability_submodules", "availability_sparse", "availability_references", "availability_diagnostics", "explanation", "environments", "lockfiles"} {
		if a[name].present || b[name].present {
			names = append(names, name)
		}
	}
	if a["environments"].present && b["environments"].present && a["environments"].metadata["provider_version"] != b["environments"].metadata["provider_version"] {
		for _, side := range []map[string]moduleData{a, b} {
			m := side["environments"]
			m.complete = false
			m.observedOnly = true
			m.reasons = appendReason(m.reasons, "environment_provider_scope_changed; absence across provider versions is not established")
			side["environments"] = m
		}
	}
	for _, name := range names {
		m := compareModule(name, a[name], b[name], &remaining, &byteBudget)
		if m.Counts.OmittedChanges > 0 {
			r.Status = "partial"
		}
		r.Modules = append(r.Modules, m)
	}
	// Redistribute retention only when limits bind. Ordinary comparisons retain
	// their original byte representation; large early modules cannot starve later ones.
	if r.Status == "partial" {
		demand := make([]int, len(r.Modules))
		for i, m := range r.Modules {
			demand[i] = len(m.Changes) + m.Counts.OmittedChanges
		}
		quotas := fairQuotas(demand, MaxChanges)
		byteDemand := make([]int, len(demand))
		for i, n := range demand {
			if n > 0 {
				byteDemand[i] = MaxOutputBytes
			}
		}
		bytes := fairQuotas(byteDemand, MaxOutputBytes-(1<<20))
		for i, name := range names {
			r.Modules[i] = compareModule(name, a[name], b[name], &quotas[i], &bytes[i])
		}
	}
	// Shrink values first; retain field names, identities and observation counts.
	data, err := json.Marshal(r)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(data) > MaxOutputBytes {
		for i := range r.Modules {
			for j := range r.Modules[i].Changes {
				for k := range r.Modules[i].Changes[j].Fields {
					field := &r.Modules[i].Changes[j].Fields[k]
					omitValue(field.Base)
					omitValue(field.Head)
				}
			}
		}
		data, _ = json.Marshal(r)
	}
	for len(data) > MaxOutputBytes {
		removed := false
		largest := -1
		for i := range r.Modules {
			if len(r.Modules[i].Changes) > 0 && (largest < 0 || len(r.Modules[i].Changes) > len(r.Modules[largest].Changes)) {
				largest = i
			}
		}
		if largest >= 0 {
			m := &r.Modules[largest]
			drop := max(1, len(m.Changes)/2)
			m.Changes = m.Changes[:len(m.Changes)-drop]
			m.Counts.OmittedChanges += drop
			m.Reasons = appendReason(m.Reasons, "comparison_output_limit")
			removed = true
		}
		if !removed {
			return nil, ErrLimit
		}
		r.Status = "partial"
		data, _ = json.Marshal(r)
	}
	return r, nil
}

func identity(role string, s *Snapshot) Identity {
	return Identity{role, s.sha256, s.bytes, s.profile.SchemaVersion, s.profile.Root}
}

func compareModule(name string, a, b moduleData, remaining, byteBudget *int) Module {
	if !a.present {
		a.status = "unavailable"
	}
	if !b.present {
		b.status = "unavailable"
	}
	m := Module{Name: name, Scope: comparisonScope(name), Status: "unchanged", Compatibility: "compatible", Reasons: []string{}, BaseStatus: a.status, HeadStatus: b.status, Metadata: []FieldChange{}, Changes: []Change{}}
	for _, reason := range append(a.reasons, b.reasons...) {
		m.Reasons = appendReason(m.Reasons, reason)
	}
	if !a.present || !b.present {
		m.Status, m.Compatibility = "unavailable", "unavailable"
		m.Reasons = appendReason(m.Reasons, "module_not_present_in_both_reports")
		return m
	}
	policyChanges := compareFields(a.policy, b.policy)
	for i := range policyChanges {
		policyChanges[i].Field = "policy." + policyChanges[i].Field
	}
	m.Metadata = append(m.Metadata, policyChanges...)
	metadata := compareFields(a.metadata, b.metadata)
	for i := range metadata {
		metadata[i].Field = "coverage_or_source." + metadata[i].Field
	}
	m.Metadata = append(m.Metadata, metadata...)
	if a.status != b.status {
		m.Metadata = append(m.Metadata, FieldChange{Field: "status", Base: value(a.status), Head: value(b.status)})
	}
	if len(policyChanges) > 0 || a.duplicate || b.duplicate || a.unsupported || b.unsupported {
		m.Status, m.Compatibility = "incomparable", "incomparable"
		if len(policyChanges) > 0 {
			m.Reasons = appendReason(m.Reasons, "provider_scope_or_policy_changed")
		}
		if a.duplicate || b.duplicate {
			m.Reasons = appendReason(m.Reasons, "duplicate_or_invalid_matching_identity")
		}
		return m
	}
	if a.status == "skipped" || b.status == "skipped" {
		m.Status, m.Compatibility = "unavailable", "unavailable"
		m.Reasons = appendReason(m.Reasons, "module_was_skipped")
		return m
	}
	if a.observedOnly || b.observedOnly || !a.complete || !b.complete {
		m.Compatibility = "observed_only"
	}
	if !a.complete || !b.complete {
		m.Reasons = appendReason(m.Reasons, "incomplete_coverage_limits_absence_claims")
	}
	for _, id := range unionKeys(a.entities, b.entities) {
		old, oldExists := a.entities[id]
		current, newExists := b.entities[id]
		change := Change{ID: id, Basis: "comparable_report_observation", BaseEvidence: []string{}, HeadEvidence: []string{}, Fields: []FieldChange{}}
		if m.Compatibility == "observed_only" {
			change.Basis = "observed_report_difference; source-change attribution is unproven"
		}
		if oldExists {
			change.BaseEvidence = boundedEvidence(old.evidence)
			change.BaseEvidenceOmitted = len(old.evidence) - len(change.BaseEvidence)
		}
		if newExists {
			change.HeadEvidence = boundedEvidence(current.evidence)
			change.HeadEvidenceOmitted = len(current.evidence) - len(change.HeadEvidence)
		}
		switch {
		case !oldExists:
			change.Status = "added"
			if !a.complete {
				change.Status, change.Reason = "unavailable", "absence_from_base_is_not_proven"
			}
			change.Fields = compareFields(nil, current.fields)
		case !newExists:
			change.Status = "removed"
			if !b.complete {
				change.Status, change.Reason = "unavailable", "absence_from_head_is_not_proven"
			}
			change.Fields = compareFields(old.fields, nil)
		default:
			change.Fields = compareFields(old.fields, current.fields)
			if len(change.Fields) == 0 {
				m.Counts.Unchanged++
				continue
			}
			change.Status = "changed"
		}
		switch change.Status {
		case "added":
			m.Counts.Added++
		case "removed":
			m.Counts.Removed++
		case "changed":
			m.Counts.Changed++
		case "unavailable":
			m.Counts.Unavailable++
		}
		encoded, _ := json.Marshal(change)
		if *remaining > 0 && len(encoded) <= *byteBudget {
			m.Changes = append(m.Changes, change)
			*remaining--
			*byteBudget -= len(encoded)
		} else {
			m.Counts.OmittedChanges++
			reason := "comparison_change_limit"
			if len(encoded) > *byteBudget {
				reason = "comparison_output_limit"
			}
			m.Reasons = appendReason(m.Reasons, reason)
		}
	}
	if m.Counts.Added+m.Counts.Removed+m.Counts.Changed > 0 {
		m.Status = "changed"
	} else if m.Counts.Unavailable > 0 {
		m.Status = "unavailable"
	}
	return m
}

func comparisonScope(name string) string {
	switch name {
	case "languages":
		return "Reported language statistics by language name, including the reported byte denominator; classifier and selection provenance are unavailable."
	case "summary":
		return "Reported inventory and included-language counts; full source-selection provenance is unavailable."
	case "ecosystems", "frameworks", "layouts":
		return "Reported findings matched by kind, name, root and detector; detector version and selection provenance are unavailable."
	case "projects":
		return "Legacy project/configuration observations, composition and attribution counts; parser-version provenance is unavailable."
	case "declarations":
		return "Manifest identities and their supported requirements, references and interfaces; declaration records are not evaluated build behavior."
	case "discovery":
		return "Metadata inventory, content categories, roles, candidate counts and retained filename candidates."
	case "registries":
		return "Selected registry configuration declarations; not evaluated registry precedence or effective sources."
	case "package_evidence":
		return "Imported package, file and relationship observations matched by their reported IDs; provider scan completeness remains unproven."
	case "metrics":
		return "Reported aggregate totals and language/directory groups; per-file observations are compared separately in metrics_files."
	case "metrics_files":
		return "Explicitly included per-file metrics matched by path; unavailable when either report omits file details."
	case "rules":
		return "Rule summaries and retained observations; source and evaluation coverage counters are separate metadata."
	case "formats":
		return "Retained format evidence matched by file path; prefixes and signatures do not validate entire files or establish their purpose."
	case "hotspots":
		return "Measured distributions and retained top evidence by language, grammar, syntax cohort and metric; leaving a ranking does not establish function removal."
	case "focus_primary":
		return "Files selected for the primary project, separate from related and context populations."
	case "focus_related":
		return "Files selected for each explicitly requested related project; changed requested membership is a scope-policy change."
	case "focus_context":
		return "Shared declaration context used to plan the focused populations; it is not part of either measured file population."
	case "focus_relations":
		return "Static declaration relations retained by the focus plan; they do not assert runtime resolution."
	case "focus_affected_projects":
		return "Projects directly associated with the requested affected path under the retained focus query limits."
	case "focused_metrics_primary":
		return "Metrics measured only over the primary focused population."
	case "focused_metrics_related":
		return "Metrics measured separately for each explicitly requested related-project population."
	case "availability_lfs":
		return "Selected-file LFS pointer observations; attributed non-pointer content does not prove successful hydration."
	case "availability_gitlinks", "availability_submodules":
		return "Committed-tree acquisition-boundary observations; checkout sparsity is kept separate."
	case "availability_sparse":
		return "Local checkout metadata observations for directory sources; these are unavailable for committed-tree sources."
	case "availability_references":
		return "Missing declaration references correlated only with explicitly observed acquisition boundaries."
	case "availability_diagnostics":
		return "Retained availability diagnostics; omission is never evidence that a diagnostic was resolved."
	case "explanation":
		return "Semantic explanation trace comparison is unsupported; rendered prose and trace order are not stable identities."
	case "environments":
		return "Environment comparison is unsupported in this comparison schema version."
	default:
		return "Detailed comparison is not supported for this module."
	}
}

func appendReason(reasons []string, reason string) []string {
	if !slices.Contains(reasons, reason) {
		reasons = append(reasons, reason)
	}
	return reasons
}

func unionKeys[T any](a, b map[string]T) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, found := a[k]; !found {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func compareFields(a, b map[string]any) []FieldChange {
	changes := []FieldChange{}
	for _, field := range unionKeys(a, b) {
		old, oldExists := a[field]
		current, newExists := b[field]
		var before, after *Value
		if oldExists {
			before = value(old)
		}
		if newExists {
			after = value(current)
		}
		if oldExists && newExists && before.SHA256 == after.SHA256 {
			continue
		}
		changes = append(changes, FieldChange{field, before, after})
	}
	return changes
}

func value(v any) *Value {
	data, _ := json.Marshal(v)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var generic any
	_ = decoder.Decode(&generic)
	data = canonical(generic)
	digest := sha256.Sum256(data)
	result := &Value{SHA256: hex.EncodeToString(digest[:]), Bytes: len(data), Omitted: len(data) > MaxFieldValueBytes}
	if !result.Omitted {
		result.Data = data
	}
	return result
}

// Report collections are compared as multisets. Order-bearing declarations
// retain explicit indices; duplicate records are not silently deduplicated.
func canonical(v any) []byte {
	switch x := v.(type) {
	case map[string]any:
		normalized := map[string]json.RawMessage{}
		for k, v := range x {
			normalized[k] = canonical(v)
		}
		data, _ := json.Marshal(normalized)
		return data
	case []any:
		items := make([]json.RawMessage, 0, len(x))
		for _, v := range x {
			items = append(items, canonical(v))
		}
		slices.SortFunc(items, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
		data, _ := json.Marshal(items)
		return data
	default:
		data, _ := json.Marshal(v)
		return data
	}
}

func boundedEvidence(evidence []string) []string {
	result := []string{}
	for _, item := range evidence {
		if len(result) == MaxEvidencePaths {
			break
		}
		if len(item) <= MaxEvidencePathBytes {
			result = append(result, item)
		}
	}
	return result
}

func omitValue(v *Value) {
	if v != nil {
		v.Data = nil
		v.Omitted = true
	}
}

// fairQuotas allocates small demands completely before sharing the remainder.
func fairQuotas(demand []int, budget int) []int {
	out := make([]int, len(demand))
	for budget > 0 {
		active := 0
		for i, n := range demand {
			if out[i] < n {
				active++
			}
		}
		if active == 0 {
			break
		}
		share := max(1, budget/active)
		for i, n := range demand {
			take := min(n-out[i], share, budget)
			out[i] += take
			budget -= take
		}
	}
	return out
}
