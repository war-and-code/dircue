package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/war-and-code/dircue/internal/jsontext"
	"github.com/war-and-code/dircue/pkg/declarations"
)

const semanticsReference = "https://learn.microsoft.com/en-us/dotnet/core/tools/global-json (last updated 2026-03-09; accessed 2026-09-21)"

// Skip returns a valid zero-evidence report when source traversal could not
// safely supply the complete inventory required for environment selection.
func Skip(source, tree, reason string) *Report {
	l := defaults(Limits{})
	return &Report{Provider: Provider, ProviderVersion: ProviderVersion, Status: "skipped", Source: source, Tree: tree, SemanticsReference: semanticsReference, Limits: l, Requirements: []Requirement{}, Selections: []Selection{}, Conflicts: []Conflict{}, Boundaries: []Boundary{{Reason: reason, Detail: "Environment inventory was omitted because the selected source traversal did not complete."}}, Diagnostics: []Diagnostic{}}
}

func Analyze(ctx context.Context, in Input, limits Limits) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits = defaults(limits)
	r := &Report{Provider: Provider, ProviderVersion: ProviderVersion, Status: "complete", Source: in.Source, Tree: in.Tree, SemanticsReference: semanticsReference, Limits: limits, Requirements: []Requirement{}, Selections: []Selection{}, Conflicts: []Conflict{}, Boundaries: []Boundary{}, Diagnostics: []Diagnostic{}}
	r.Coverage.OmittedFiles = in.OmittedFiles
	if !in.InventoryComplete || in.OmittedFiles > 0 {
		r.Status = "partial"
	}
	// A declarations gap normally means environment requirements may be wholly
	// absent, so Requirement.State cannot disclose it. The sole independently
	// checkable exception is strict-JSON rejection of global.json: this module
	// reparses selected global.json files with their documented JSONC leniency.
	// Keep track of those paths and require an actual successful environment
	// parse below before allowing a complete result.
	declarationRechecks, independentlyCheckable := independentlyCheckableDeclarationGaps(in)
	if in.Declarations.Status != "complete" {
		detail := "The upstream declarations pass reported incomplete requirement coverage; environment requirements may be omitted."
		if independentlyCheckable {
			detail = "The upstream declarations pass rejected global.json syntax; selected files are reparsed independently and must succeed before environment coverage is complete."
		} else {
			r.Status = "partial"
		}
		r.Boundaries = append(r.Boundaries, Boundary{Reason: "declarations-partial", Detail: detail})
	}
	files := map[string]File{}
	inventoryComplete := in.InventoryComplete
	ordered := slices.Clone(in.Inventory)
	slices.SortFunc(ordered, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	for _, f := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.Coverage.InventoryPaths >= limits.InventoryPaths {
			r.Status = "partial"
			r.Coverage.OmittedFiles++
			inventoryComplete = false
			continue
		}
		clean, ok := cleanRelative(f.Path)
		if !ok {
			r.Status = "partial"
			inventoryComplete = false
			r.Diagnostics = append(r.Diagnostics, Diagnostic{f.Path, "invalid-inventory-path", "Inventory path is not a confined root-relative path."})
			continue
		}
		if _, exists := files[clean]; !exists {
			files[clean] = File{Path: clean, Size: f.Size, NonRegular: f.NonRegular}
			r.Coverage.InventoryPaths++
		}
	}
	records := slices.Clone(in.ProjectRecords)
	slices.SortFunc(records, func(a, b declarations.ProjectRecord) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	projectRoots := map[string]string{}
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.Coverage.ProjectRecords++
		if rec.Parsed {
			r.Coverage.ParsedProjectRecords++
		}
		projectRoots[rec.Project.ID] = rec.Project.Root
		for _, req := range rec.Project.Requirements {
			addNormalized(r, limits, rec, req)
		}
	}
	detectPythonConflicts(r)
	starts := slices.Clone(in.InvocationStarts)
	if len(starts) == 0 {
		for _, rec := range records {
			if rec.Parsed && (rec.Project.Kind == "dotnet" || rec.Project.Kind == "solution") {
				starts = append(starts, Invocation{rec.Project.ID, rec.Project.Root})
			}
		}
		if len(starts) == 0 {
			for _, rec := range records {
				if rec.Parsed && rec.Project.Kind == "dotnet-configuration" && path.Base(rec.Project.ID) == "global.json" {
					starts = append(starts, Invocation{rec.Project.ID, rec.Project.Root})
				}
			}
		}
	}
	slices.SortFunc(starts, func(a, b Invocation) int {
		if a.ProjectID == b.ProjectID {
			return strings.Compare(a.Directory, b.Directory)
		}
		return strings.Compare(a.ProjectID, b.ProjectID)
	})
	type parsedSelection struct {
		selection   Selection
		status      string
		boundaries  []Boundary
		diagnostics []Diagnostic
	}
	parseCache := map[string]parsedSelection{}
	diagnosticsEmitted := map[string]bool{}
	for i, start := range starts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i >= limits.Contexts {
			r.Status = "partial"
			r.Boundaries = append(r.Boundaries, Boundary{Reason: "context-limit", Detail: "Additional invocation contexts were omitted."})
			break
		}
		dir, ok := cleanDirectory(start.Directory)
		if !ok {
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{start.Directory, "invalid-invocation-start", "Invocation start is not a confined root-relative directory."})
			continue
		}
		basis := "explicit-invocation-start"
		if len(in.InvocationStarts) == 0 {
			basis = "modeled-project-root"
		}
		contextID := fmt.Sprintf("%s@%s", start.ProjectID, dir)
		sel := Selection{ContextID: contextID, ProjectID: start.ProjectID, StartDirectory: dir, StartBasis: basis, State: "unconstrained", Applicability: "candidate SDK selection for an invocation starting at the recorded directory; actual CLI/MSBuild start may differ"}
		if shared := nearestNamed(dir, files, "Directory.Build.props"); shared != "" && shared != start.ProjectID {
			r.Boundaries = append(r.Boundaries, Boundary{Path: shared, ProjectID: start.ProjectID, ContextID: contextID, Reason: "shared-properties-applicability-unresolved", Detail: "A nearest ancestor Directory.Build.props may contribute project requirements; MSBuild import and condition semantics were not evaluated."})
		}
		candidate := nearestGlobal(dir, files)
		if candidate == "" {
			if !inventoryComplete {
				sel.State = "unresolved"
				r.Boundaries = append(r.Boundaries, Boundary{ProjectID: start.ProjectID, ContextID: contextID, Reason: "incomplete-inventory", Detail: "A nearer or ancestor global.json might be omitted."})
			}
			r.Boundaries = append(r.Boundaries, Boundary{ProjectID: start.ProjectID, ContextID: contextID, Reason: "outside-selected-root-not-inspected", Detail: "SDK configuration above the selected source root was not inspected."})
			r.Selections = append(r.Selections, sel)
			r.Coverage.Contexts++
			continue
		}
		r.Coverage.GlobalJSONCandidates++
		sel.GlobalJSON = candidate
		f := files[candidate]
		if !inventoryComplete {
			sel.State = "unresolved"
			r.Status = "partial"
			r.Boundaries = append(r.Boundaries, Boundary{Path: candidate, ProjectID: start.ProjectID, ContextID: contextID, Reason: "incomplete-inventory", Detail: "A nearer global.json might be omitted, so the retained candidate was not parsed."})
			r.Selections = append(r.Selections, sel)
			r.Coverage.Contexts++
			continue
		}
		parsed, cached := parseCache[candidate]
		if cached {
			sel.SDKVersion = parsed.selection.SDKVersion
			sel.RollForward = parsed.selection.RollForward
			sel.AllowPrerelease = parsed.selection.AllowPrerelease
			sel.State = parsed.selection.State
			if parsed.status == "partial" {
				r.Status = "partial"
			}
			for _, b := range parsed.boundaries {
				b.ProjectID = sel.ProjectID
				b.ContextID = sel.ContextID
				r.Boundaries = append(r.Boundaries, b)
			}
			if !diagnosticsEmitted[candidate] {
				r.Diagnostics = append(r.Diagnostics, parsed.diagnostics...)
				diagnosticsEmitted[candidate] = true
			}
			r.Selections = append(r.Selections, sel)
			r.Coverage.Contexts++
			continue
		}
		if f.NonRegular || f.Size < 0 || f.Size > limits.GlobalJSONBytes || f.Size > limits.InputBytes-r.Coverage.InputBytes || in.ReadSelected == nil {
			sel.State = "unresolved"
			r.Status = "partial"
			r.Boundaries = append(r.Boundaries, Boundary{Path: candidate, ProjectID: start.ProjectID, ContextID: contextID, Reason: "global-json-unread", Detail: "Selected file lacks a bounded selected-source read."})
			r.Selections = append(r.Selections, sel)
			r.Coverage.Contexts++
			continue
		}
		content, size, err := in.ReadSelected(ctx, candidate, limits.GlobalJSONBytes+1)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// ReadSelected may use a derived context whose cancellation is not
			// reflected in the parent yet. Cancellation remains fatal under every
			// error policy.
			if errors.Is(err, context.Canceled) {
				return nil, context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, context.DeadlineExceeded
			}
			if in.ErrorPolicy == "continue" {
				// Preserve remaining selections. The per-path diagnostic
				// keeps the omission attributable and matches the other
				// aggregation modules' file-read-error vocabulary.
				sel.State = "unresolved"
				r.Status = "partial"
				diagnostic := Diagnostic{Path: candidate, Code: "file-read-error", Message: "Selected global.json could not be read."}
				// Cache the failed selection just like a parsed selection. A live
				// source must be read at most once per analysis; otherwise two
				// contexts selecting the same file could observe contradictory
				// states after a transient failure or source replacement.
				parseCache[candidate] = parsedSelection{selection: sel, status: "partial", diagnostics: []Diagnostic{diagnostic}}
				diagnosticsEmitted[candidate] = true
				r.Diagnostics = append(r.Diagnostics, diagnostic)
				r.Coverage.OmittedFiles++
				r.Selections = append(r.Selections, sel)
				r.Coverage.Contexts++
				continue
			}
			return nil, errors.New("could not read selected global.json")
		}
		if size != int64(len(content)) || size != f.Size || size > limits.GlobalJSONBytes || size > limits.InputBytes-r.Coverage.InputBytes {
			sel.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{candidate, "incomplete-global-json", "Selected global.json changed, was incomplete, or exceeded the read limit."})
			r.Selections = append(r.Selections, sel)
			r.Coverage.Contexts++
			continue
		}
		r.Coverage.GlobalJSONRead++
		r.Coverage.InputBytes += size
		tmp := &Report{Status: "complete", Boundaries: []Boundary{}, Diagnostics: []Diagnostic{}}
		parseGlobal(tmp, &sel, content)
		parseCache[candidate] = parsedSelection{selection: sel, status: tmp.Status, boundaries: slices.Clone(tmp.Boundaries), diagnostics: slices.Clone(tmp.Diagnostics)}
		if tmp.Status == "complete" {
			if _, needsRecheck := declarationRechecks[candidate]; needsRecheck && !hasDeclarationOnlyGlobalJSONFields(content) {
				delete(declarationRechecks, candidate)
			}
		}
		diagnosticsEmitted[candidate] = true
		if tmp.Status == "partial" {
			r.Status = "partial"
		}
		r.Boundaries = append(r.Boundaries, tmp.Boundaries...)
		r.Diagnostics = append(r.Diagnostics, tmp.Diagnostics...)
		r.Selections = append(r.Selections, sel)
		r.Coverage.Contexts++
	}
	slices.SortFunc(r.Requirements, func(a, b Requirement) int {
		return strings.Compare(a.ProjectID+"\x00"+a.Dimension+"\x00"+a.Kind+"\x00"+a.Value+"\x00"+a.Evidence, b.ProjectID+"\x00"+b.Dimension+"\x00"+b.Kind+"\x00"+b.Value+"\x00"+b.Evidence)
	})
	// A strict declarations parse can be excused only by successful independent
	// parsing of every affected selected global.json. An unselected or genuinely
	// malformed file leaves the upstream coverage gap unresolved.
	if len(declarationRechecks) > 0 {
		r.Status = "partial"
	}
	_ = projectRoots
	if err := ValidateReport(r); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(encoded) > limits.OutputBytes {
		return nil, errors.New("environment report exceeds output byte limit")
	}
	return r, nil
}

// independentlyCheckableDeclarationGaps identifies the one declarations-side
// partial state this analyzer can fully replace with its own evidence: strict
// JSON rejection of global.json. Any omission, lost diagnostic, other parser
// failure, skipped state, or unexplained partial status can hide requirements
// that this package does not independently extract.
func independentlyCheckableDeclarationGaps(in Input) (map[string]struct{}, bool) {
	if in.Declarations.Status != "partial" || in.Declarations.Coverage.OmittedFiles != 0 || in.Declarations.Coverage.OmittedDiagnostics != 0 || len(in.Declarations.Diagnostics) == 0 {
		return nil, false
	}
	paths := make(map[string]struct{}, len(in.Declarations.Diagnostics))
	for _, diagnostic := range in.Declarations.Diagnostics {
		if diagnostic.Code != "invalid-json" || path.Base(diagnostic.Path) != "global.json" {
			return nil, false
		}
		paths[diagnostic.Path] = struct{}{}
	}
	for _, record := range in.ProjectRecords {
		if record.Parsed && record.Complete {
			continue
		}
		if _, ok := paths[record.Project.ID]; !ok {
			return nil, false
		}
	}
	return paths, true
}

// hasDeclarationOnlyGlobalJSONFields reports whether a successfully parsed
// global.json still contains environment requirements that parseGlobal does
// not extract. The declarations adapter turns msbuild-sdks entries into
// dotnet-sdk requirements, so SDK-selection success cannot replace that lost
// evidence. Parse failures are treated conservatively; callers use this only
// after parseGlobal has accepted the same bounded content.
func hasDeclarationOnlyGlobalJSONFields(content []byte) bool {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	cleaned, _, err := stripJSONComments(content)
	if err != nil {
		return true
	}
	cleaned, _ = stripTrailingCommas(cleaned)
	var root map[string]json.RawMessage
	if json.Unmarshal(cleaned, &root) != nil || root == nil {
		return true
	}
	for key := range root {
		if strings.EqualFold(key, "msbuild-sdks") {
			return true
		}
	}
	return false
}

func defaults(l Limits) Limits {
	if l.InventoryPaths <= 0 || l.InventoryPaths > DefaultMaxInventoryPaths {
		l.InventoryPaths = DefaultMaxInventoryPaths
	}
	if l.GlobalJSONBytes <= 0 || l.GlobalJSONBytes > DefaultMaxGlobalJSONBytes {
		l.GlobalJSONBytes = DefaultMaxGlobalJSONBytes
	}
	if l.InputBytes <= 0 || l.InputBytes > DefaultMaxInputBytes {
		l.InputBytes = DefaultMaxInputBytes
	}
	if l.Requirements <= 0 || l.Requirements > DefaultMaxRequirements {
		l.Requirements = DefaultMaxRequirements
	}
	if l.Contexts <= 0 || l.Contexts > DefaultMaxContexts {
		l.Contexts = DefaultMaxContexts
	}
	if l.OutputBytes <= 0 || l.OutputBytes > DefaultMaxOutputBytes {
		l.OutputBytes = DefaultMaxOutputBytes
	}
	return l
}
func cleanRelative(v string) (string, bool) {
	if v == "" || strings.Contains(v, "\\") || strings.HasPrefix(v, "/") || strings.ContainsRune(v, 0) {
		return "", false
	}
	c := path.Clean(v)
	return c, c != "." && c != ".." && !strings.HasPrefix(c, "../")
}
func cleanDirectory(v string) (string, bool) {
	if v == "." || v == "" {
		return ".", true
	}
	return cleanRelative(v)
}
func nearestGlobal(dir string, files map[string]File) string {
	return nearestNamed(dir, files, "global.json")
}
func nearestNamed(dir string, files map[string]File, name string) string {
	for {
		p := name
		if dir != "." {
			p = path.Join(dir, p)
		}
		if _, ok := files[p]; ok {
			return p
		}
		if dir == "." {
			return ""
		}
		next := path.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

var sdkVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)
var rollPolicies = map[string]bool{"patch": true, "feature": true, "minor": true, "major": true, "latestPatch": true, "latestFeature": true, "latestMinor": true, "latestMajor": true, "disable": true}

type globalDocument struct {
	SDK json.RawMessage `json:"sdk"`
}
type globalSDK struct {
	Version         json.RawMessage `json:"version"`
	RollForward     json.RawMessage `json:"rollForward"`
	AllowPrerelease json.RawMessage `json:"allowPrerelease"`
	Paths           json.RawMessage `json:"paths"`
}

func parseGlobal(r *Report, s *Selection, content []byte) {
	if int64(len(content)) > DefaultMaxGlobalJSONBytes {
		s.State = "unresolved"
		r.Status = "partial"
		return
	}
	hadBOM := bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf})
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	if !jsontext.ValidUnicode(content) {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "unsupported-json-text", "global.json contains invalid UTF-8 or unpaired Unicode escapes."})
		return
	}
	cleaned, strippedComments, commentErr := stripJSONComments(content)
	if commentErr != nil {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-global-json", "global.json contains an unterminated block comment."})
		return
	}
	var strippedTrailingComma bool
	cleaned, strippedTrailingComma = stripTrailingCommas(cleaned)
	if err := uniqueJSONKeys(cleaned); err != nil {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-global-json", "global.json is malformed or contains duplicate object members."})
		return
	}
	if bytes.Equal(bytes.TrimSpace(cleaned), []byte("null")) {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-global-json", "global.json must contain an object."})
		return
	}
	var rootMembers map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &rootMembers); err != nil || rootMembers == nil {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-global-json", "global.json is malformed or uses unsupported JSON syntax."})
		return
	}
	for key := range rootMembers {
		if key != "sdk" && strings.EqualFold(key, "sdk") {
			s.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "unsupported-sdk-casing", "The sdk member name is case-sensitive."})
			return
		}
	}
	doc := globalDocument{SDK: rootMembers["sdk"]}
	if len(doc.SDK) == 0 {
		s.State = "unconstrained"
		return
	}
	if bytes.Equal(bytes.TrimSpace(doc.SDK), []byte("null")) {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-global-json-sdk", "The sdk member must be an object."})
		return
	}
	var sdk globalSDK
	if err := json.Unmarshal(doc.SDK, &sdk); err != nil {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-global-json-sdk", "The sdk member must be an object."})
		return
	}
	var sdkMembers map[string]json.RawMessage
	if err := json.Unmarshal(doc.SDK, &sdkMembers); err != nil {
		return
	}
	for key := range sdkMembers {
		if !slices.Contains([]string{"version", "rollForward", "allowPrerelease", "paths", "errorMessage"}, key) {
			s.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "unsupported-sdk-field", "global.json sdk contains an unsupported selection field."})
			return
		}
	}
	if len(sdk.Version) > 0 {
		var value string
		if json.Unmarshal(sdk.Version, &value) != nil || len(value) > 128 || !sdkVersion.MatchString(value) {
			s.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "unsupported-sdk-version", "sdk.version must be a full numeric SDK version; ranges and wildcards are unsupported."})
		} else {
			s.SDKVersion = value
		}
	}
	if len(sdk.RollForward) > 0 {
		if json.Unmarshal(sdk.RollForward, &s.RollForward) != nil || !rollPolicies[s.RollForward] {
			s.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "unsupported-roll-forward", "sdk.rollForward is not a documented policy."})
		}
	}
	if s.RollForward == "" && s.SDKVersion != "" {
		s.RollForward = "patch"
	}
	if s.RollForward != "" && s.SDKVersion == "" && s.RollForward != "latestMajor" {
		s.State = "unresolved"
		r.Status = "partial"
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "roll-forward-requires-version", "This rollForward policy requires sdk.version."})
	}
	if len(sdk.AllowPrerelease) > 0 {
		var b bool
		if bytes.Equal(bytes.TrimSpace(sdk.AllowPrerelease), []byte("null")) || json.Unmarshal(sdk.AllowPrerelease, &b) != nil {
			s.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "invalid-allow-prerelease", "sdk.allowPrerelease must be boolean."})
		} else {
			s.AllowPrerelease = &b
		}
	}
	if len(sdk.Paths) > 0 {
		var p []string
		if json.Unmarshal(sdk.Paths, &p) != nil {
			s.State = "unresolved"
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "unsupported-sdk-paths", "sdk.paths must be an array of strings."})
		} else if len(p) > 0 {
			r.Boundaries = append(r.Boundaries, Boundary{Path: s.GlobalJSON, ProjectID: s.ProjectID, ContextID: s.ContextID, Reason: "sdk-search-paths-unresolved", Detail: "Declared SDK search paths are retained as a selection boundary; no filesystem or installed SDK probe was performed."})
		}
	}
	if s.State != "unresolved" {
		if s.SDKVersion != "" || s.RollForward != "" || s.AllowPrerelease != nil {
			s.State = "declared"
		} else {
			s.State = "unconstrained"
		}
	}
	// Disclose accepted JSONC leniency (BOM, comments, or trailing commas) so
	// a consumer can see why the file was accepted; the parse itself succeeded
	// and the status remains complete on the environments side.
	if s.State != "unresolved" && (hadBOM || strippedComments || strippedTrailingComma) {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{s.GlobalJSON, "global-json-lenient-syntax", "global.json was accepted through the documented JSONC leniency for BOM, comments, or trailing commas."})
	}
}

// stripTrailingCommas replaces commas immediately before an object or array
// close with whitespace. This matches global.json's documented JSON options
// while retaining byte offsets and leaving string contents untouched. The
// second return value indicates whether the input contained at least one
// trailing comma that was rewritten.
func stripTrailingCommas(in []byte) ([]byte, bool) {
	out := slices.Clone(in)
	inString, escaped := false, false
	stripped := false
	for i := 0; i < len(out); i++ {
		if inString {
			if escaped {
				escaped = false
			} else if out[i] == '\\' {
				escaped = true
			} else if out[i] == '"' {
				inString = false
			}
			continue
		}
		if out[i] == '"' {
			inString = true
			continue
		}
		if out[i] != ',' {
			continue
		}
		j := i + 1
		for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\r' || out[j] == '\n') {
			j++
		}
		k := i - 1
		for k >= 0 && (out[k] == ' ' || out[k] == '\t' || out[k] == '\r' || out[k] == '\n') {
			k--
		}
		if k >= 0 && !strings.ContainsRune("{[:,", rune(out[k])) && j < len(out) && (out[j] == '}' || out[j] == ']') {
			out[i] = ' '
			stripped = true
		}
	}
	return out, stripped
}

func uniqueJSONKeys(content []byte) error {
	d := json.NewDecoder(bytes.NewReader(content))
	tokens := 0
	if err := jsonValue(d, 0, &tokens); err != nil {
		return err
	}
	if _, err := d.Token(); err == nil {
		return errors.New("trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
func jsonValue(d *json.Decoder, depth int, tokens *int) error {
	if depth > 128 || *tokens > 100000 {
		return errors.New("JSON nesting or token limit exceeded")
	}
	*tokens++
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("duplicate or invalid object member")
			}
			seen[key] = true
			if err := jsonValue(d, depth+1, tokens); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		for d.More() {
			if err := jsonValue(d, depth+1, tokens); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return errors.New("unexpected delimiter")
	}
	return nil
}

func stripJSONComments(in []byte) ([]byte, bool, error) {
	out := slices.Clone(in)
	inString := false
	escaped := false
	stripped := false
	for i := 0; i < len(out); i++ {
		if inString {
			if escaped {
				escaped = false
			} else if out[i] == '\\' {
				escaped = true
			} else if out[i] == '"' {
				inString = false
			}
			continue
		}
		if out[i] == '"' {
			inString = true
			continue
		}
		if out[i] == '/' && i+1 < len(out) && out[i+1] == '/' {
			stripped = true
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i < len(out) && out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
				i++
			}
			i--
			continue
		}
		if out[i] == '/' && i+1 < len(out) && out[i+1] == '*' {
			stripped = true
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/') {
				if out[i] != '\n' && out[i] != '\r' {
					out[i] = ' '
				}
				i++
			}
			if i+1 < len(out) {
				out[i] = ' '
				out[i+1] = ' '
				i++
			} else {
				return nil, false, errors.New("unterminated block comment")
			}
			continue
		}
	}
	return out, stripped, nil
}

func addNormalized(r *Report, limits Limits, rec declarations.ProjectRecord, req declarations.Requirement) {
	if len(r.Requirements) >= limits.Requirements {
		r.Status = "partial"
		r.Coverage.OmittedRequirements++
		return
	}
	dim := "advisory"
	app := "project declaration; applicability follows its recorded state and condition"
	switch req.Kind {
	case "go-language-minimum", "cargo-rust-version", "cargo-workspace-rust-version":
		dim = "language-minimum"
	case "python-requires-python", "npm-engine":
		dim = "runtime-constraint"
	case "package-manager", "java-toolchain", "gradle-wrapper", "maven-wrapper":
		dim = "toolchain-selection"
	case "go-toolchain-suggestion":
		dim = "advisory-toolchain"
	case "dotnet-sdk":
		dim = "project-sdk-reference"
	case "target-framework", "target-framework-version":
		dim = "target-framework"
	case "runtime-identifier":
		dim = "platform-target"
	case "language-version", "java-release", "cargo-edition", "cargo-workspace-edition":
		dim = "language-mode"
	default:
		return
	}
	state := req.State
	if !rec.Parsed || !rec.Complete {
		state = "unresolved"
		app += "; parser record incomplete"
	}
	r.Requirements = append(r.Requirements, Requirement{rec.Project.ID, rec.Project.ID, dim, req.Kind, req.Value, state, req.Evidence, req.Condition, app})
	r.Coverage.Requirements++
}

type pythonBound struct {
	version         []int
	inclusive       bool
	evidence, value string
}

func detectPythonConflicts(r *Report) {
	groups := map[string][]Requirement{}
	for i := range r.Requirements {
		q := &r.Requirements[i]
		if q.Kind != "python-requires-python" || q.State != "declared" || q.Condition != "" {
			continue
		}
		if _, _, ok := pythonRange(q.Value); !ok {
			q.State = "unresolved"
			r.Status = "partial"
			r.Boundaries = append(r.Boundaries, Boundary{ProjectID: q.ProjectID, ContextID: q.ContextID, Reason: "unsupported-constraint", Detail: constraintDetail(q.Evidence, q.Value)})
			continue
		}
		groups[q.ContextID] = append(groups[q.ContextID], *q)
	}
	contextIDs := make([]string, 0, len(groups))
	for id := range groups {
		contextIDs = append(contextIDs, id)
	}
	slices.Sort(contextIDs)
	for _, contextID := range contextIDs {
		requirements := groups[contextID]
		var low, high *pythonBound
		for _, q := range requirements {
			l, h, _ := pythonRange(q.Value)
			if l != nil && (low == nil || compareVersion(l.version, low.version) > 0 || (compareVersion(l.version, low.version) == 0 && !l.inclusive)) {
				l.evidence = q.Evidence
				l.value = q.Value
				low = l
			}
			if h != nil && (high == nil || compareVersion(h.version, high.version) < 0 || (compareVersion(h.version, high.version) == 0 && !h.inclusive)) {
				h.evidence = q.Evidence
				h.value = q.Value
				high = h
			}
		}
		if low != nil && high != nil {
			cmp := compareVersion(low.version, high.version)
			if cmp > 0 || (cmp == 0 && (!low.inclusive || !high.inclusive)) {
				values, evidence := []string{low.value}, []string{low.evidence}
				if high.value != low.value || high.evidence != low.evidence {
					values, evidence = append(values, high.value), append(evidence, high.evidence)
				}
				r.Conflicts = append(r.Conflicts, Conflict{ContextID: contextID, Dimension: "runtime-version", Values: values, Evidence: evidence, Explanation: "Supported Python version clauses have an empty intersection in this project context."})
			}
		}
	}
}
func constraintDetail(evidence, value string) string {
	return "Constraint " + value + " at " + evidence + " is outside the supported comma-separated numeric Python comparison subset."
}
func pythonRange(value string) (*pythonBound, *pythonBound, bool) {
	var low, high *pythonBound
	for clause := range strings.SplitSeq(value, ",") {
		clause = strings.TrimSpace(clause)
		op := ""
		for _, candidate := range []string{">=", "<=", "==", ">", "<"} {
			if strings.HasPrefix(clause, candidate) {
				op = candidate
				break
			}
		}
		if op == "" {
			return nil, nil, false
		}
		v, ok := numericVersion(strings.TrimSpace(strings.TrimPrefix(clause, op)))
		if !ok {
			return nil, nil, false
		}
		switch op {
		case ">=", ">":
			candidate := &pythonBound{version: v, inclusive: op == ">="}
			if low == nil || compareVersion(candidate.version, low.version) > 0 || (compareVersion(candidate.version, low.version) == 0 && !candidate.inclusive) {
				low = candidate
			}
		case "<=", "<":
			candidate := &pythonBound{version: v, inclusive: op == "<="}
			if high == nil || compareVersion(candidate.version, high.version) < 0 || (compareVersion(candidate.version, high.version) == 0 && !candidate.inclusive) {
				high = candidate
			}
		case "==":
			candidate := &pythonBound{version: v, inclusive: true}
			if low == nil || compareVersion(candidate.version, low.version) > 0 {
				low = candidate
			}
			if high == nil || compareVersion(candidate.version, high.version) < 0 {
				high = candidate
			}
		}
	}
	return low, high, true
}
func numericVersion(v string) ([]int, bool) {
	if v == "" || len(v) > 128 {
		return nil, false
	}
	parts := strings.Split(v, ".")
	if len(parts) > 16 {
		return nil, false
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			return nil, false
		}
		n, err := strconv.ParseUint(p, 10, 31)
		if err != nil {
			return nil, false
		}
		out[i] = int(n)
	}
	return out, true
}
func compareVersion(a, b []int) int {
	n := max(len(a), len(b))
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
