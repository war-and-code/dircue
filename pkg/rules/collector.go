package rules

import (
	"container/heap"
	"encoding/hex"
	"math"
	"slices"
	"strings"
)

// Collector aggregates one scan. A single owner must call ObserveMetadata once
// for each selected regular file, then either ObserveContent or OmitContent once
// for each eligible candidate. The scanner admits the lexical first 256 eligible
// paths. The collector bounds retained evidence and admission independently; it
// does not retain an unbounded inventory to verify delivery of every path.
type Collector struct {
	program  *Program
	report   Report
	evidence observationHeap
	omitted  []uint64
	admitted map[string]bool
	sealed   bool
}

func New(program *Program, source Source, options Options) (*Collector, error) {
	if program == nil || len(program.rules) == 0 || options.MaxFileBytes < 0 {
		return nil, ErrContext
	}
	consistency := "live_directory"
	switch source.Kind {
	case "directory":
		if source.Tree != "" {
			return nil, ErrContext
		}
	case "git":
		if len(source.Tree) != 40 {
			return nil, ErrContext
		}
		decoded, err := hex.DecodeString(source.Tree)
		if err != nil || len(decoded) != 20 || strings.ToLower(source.Tree) != source.Tree {
			return nil, ErrContext
		}
		consistency = "selected_git_tree"
	default:
		return nil, ErrContext
	}
	limits := EffectiveLimits()
	limits.CallerMaxFileBytes = options.MaxFileBytes
	if options.MaxFileBytes > 0 && options.MaxFileBytes < int64(limits.ContentBytes) {
		limits.ContentBytes = int(options.MaxFileBytes)
	}
	report := Report{Provider: "dircue", EvaluatorVersion: EvaluatorVersion, RulesSchemaVersion: ConfigVersion, RulesSHA256: program.digest,
		Source: source, SourceConsistency: consistency, Scope: "selected_regular_files_including_vendor_generated_and_data", Status: "complete",
		Order: "path_then_rule_id", Limits: limits}
	report.Omissions = make(map[OmissionReason]uint64)
	report.Rules = make([]RuleSummary, len(program.rules))
	report.Observations = []Observation{}
	for i, r := range program.rules {
		report.Rules[i] = RuleSummary{ID: r.id, RequiresContent: r.literal != nil}
	}
	return &Collector{program: program, report: report, omitted: make([]uint64, len(program.rules)), admitted: make(map[string]bool)}, nil
}

func (c *Collector) available() error {
	if c == nil || c.program == nil || c.sealed {
		return ErrSequence
	}
	return nil
}
func canAdd(value, delta uint64) bool { return delta <= math.MaxUint64-value }
func (c *Collector) contentMask(file File) mask {
	var result mask
	c.program.candidates(file).indices(func(i int) {
		if c.program.rules[i].literal != nil {
			result.add(i)
		}
	})
	return result
}
func hasMask(m mask) bool {
	for _, word := range m {
		if word != 0 {
			return true
		}
	}
	return false
}
func (c *Collector) sizeReason(file File) OmissionReason {
	if file.Size <= int64(c.report.Limits.ContentBytes) {
		return ""
	}
	if c.report.Limits.ContentBytes < MaxContentBytes {
		return CallerFileLimit
	}
	return FileTooLarge
}

// ObserveMetadata returns whether a content read is eligible after the engine
// and caller file-size limits. Oversized candidates still contribute metadata
// observations and explicit content omissions, without using an admission slot.
func (c *Collector) ObserveMetadata(file File) (bool, error) {
	if err := c.available(); err != nil {
		return false, err
	}
	if err := validateFile(file); err != nil {
		return false, err
	}
	candidates := c.program.candidates(file)
	content := c.contentMask(file)
	hasContent := hasMask(content)
	reason := c.sizeReason(file)
	var matches uint64
	overflow := !canAdd(c.report.InventoryFiles, 1)
	if hasMask(candidates) && !canAdd(c.report.CandidateFiles, 1) {
		overflow = true
	}
	if hasContent {
		if !canAdd(c.report.ContentCandidateFiles, 1) {
			overflow = true
		}
		if reason == "" {
			if !canAdd(c.report.ContentEligibleFiles, 1) {
				overflow = true
			}
		} else {
			if !canAdd(c.report.ContentOmittedFiles, 1) || !canAdd(c.report.Omissions[reason], 1) {
				overflow = true
			}
		}
	}
	candidates.indices(func(i int) {
		r := c.report.Rules[i]
		if !canAdd(r.CandidateFiles, 1) {
			overflow = true
		}
		if !r.RequiresContent {
			matches++
			if !canAdd(r.EvaluatedFiles, 1) || !canAdd(r.MatchedFiles, 1) {
				overflow = true
			}
		} else if reason != "" && !canAdd(c.omitted[i], 1) {
			overflow = true
		}
	})
	if !canAdd(c.report.TotalMatches, matches) {
		overflow = true
	}
	if overflow {
		return false, ErrCounter
	}
	c.report.InventoryFiles++
	if hasMask(candidates) {
		c.report.CandidateFiles++
	}
	if hasContent {
		c.report.ContentCandidateFiles++
		if reason == "" {
			c.report.ContentEligibleFiles++
		} else {
			c.report.ContentOmittedFiles++
			c.report.Omissions[reason]++
		}
	}
	candidates.indices(func(i int) {
		r := &c.report.Rules[i]
		r.CandidateFiles++
		if r.RequiresContent {
			if reason != "" {
				c.omitted[i]++
			}
			return
		}
		r.EvaluatedFiles++
		r.MatchedFiles++
		c.retain(Observation{Path: file.Path, RuleID: r.ID, Evidence: "metadata", Size: file.Size})
	})
	c.report.TotalMatches += matches
	return hasContent && reason == "", nil
}

// ObserveContent evaluates a complete file, including negative matches. On error
// it changes no counters; callers can classify an incomplete or non-UTF-8 read
// with OmitContent. Source bytes are neither retained nor changed.
func (c *Collector) ObserveContent(file File, content []byte) error {
	if err := c.available(); err != nil {
		return err
	}
	if err := validateFile(file); err != nil {
		return err
	}
	if c.sizeReason(file) != "" {
		return ErrContentTooLarge
	}
	selected := c.contentMask(file)
	if !hasMask(selected) {
		return ErrNotCandidate
	}
	if err := c.checkContent(file, selected, true); err != nil {
		return err
	}
	observations, err := c.program.MatchContent(file, content)
	if err != nil {
		return err
	}
	if !canAdd(c.report.TotalMatches, uint64(len(observations))) {
		return ErrCounter
	}
	// Each rule can match at most once per file. Candidate capacity checked above
	// also bounds its evaluated and matched counts before any state is changed.
	c.admitted[file.Path] = true
	c.report.ContentAdmittedFiles++
	c.report.ContentEvaluatedFiles++
	selected.indices(func(i int) { c.report.Rules[i].EvaluatedFiles++ })
	for _, observation := range observations {
		i, _ := slices.BinarySearchFunc(c.report.Rules, observation.RuleID, func(r RuleSummary, id string) int { return strings.Compare(r.ID, id) })
		c.report.Rules[i].MatchedFiles++
		c.retain(observation)
	}
	c.report.TotalMatches += uint64(len(observations))
	return nil
}

func (c *Collector) checkContent(file File, selected mask, admit bool) error {
	if c.admitted[file.Path] {
		return ErrSequence
	}
	if admit && c.report.ContentAdmittedFiles >= MaxContentFiles {
		return ErrSequence
	}
	if c.report.ContentEvaluatedFiles > c.report.ContentCandidateFiles || c.report.ContentOmittedFiles >= c.report.ContentCandidateFiles-c.report.ContentEvaluatedFiles {
		return ErrSequence
	}
	valid := true
	selected.indices(func(i int) {
		r := c.report.Rules[i]
		if r.EvaluatedFiles > r.CandidateFiles || c.omitted[i] >= r.CandidateFiles-r.EvaluatedFiles {
			valid = false
		}
	})
	if !valid {
		return ErrSequence
	}
	return nil
}

// OmitContent records a candidate that was not evaluated. ContentBudget means
// the file was not admitted; Incomplete and InvalidUTF8 count a read attempt.
// Size omissions are recorded automatically by ObserveMetadata.
func (c *Collector) OmitContent(file File, reason OmissionReason) error {
	if err := c.available(); err != nil {
		return err
	}
	if reason != ContentBudget && reason != Incomplete && reason != InvalidUTF8 {
		return ErrContext
	}
	if err := validateFile(file); err != nil {
		return err
	}
	if c.sizeReason(file) != "" {
		return ErrSequence
	}
	selected := c.contentMask(file)
	if !hasMask(selected) {
		return ErrNotCandidate
	}
	admit := reason != ContentBudget
	if err := c.checkContent(file, selected, admit); err != nil {
		return err
	}
	if !canAdd(c.report.Omissions[reason], 1) {
		return ErrCounter
	}
	if admit {
		c.admitted[file.Path] = true
		c.report.ContentAdmittedFiles++
	}
	c.report.ContentOmittedFiles++
	c.report.Omissions[reason]++
	selected.indices(func(i int) { c.omitted[i]++ })
	return nil
}

// Omit records excluded inventory without inventing per-rule associations.
// Nonregular files are outside the advertised scope; unsupported paths and
// tree-size exclusions make overall coverage partial.
func (c *Collector) Omit(reason OmissionReason, count uint64) error {
	if err := c.available(); err != nil {
		return err
	}
	if reason != NonRegularFile && reason != UnsupportedPath && reason != TreeSizeLimit {
		return ErrContext
	}
	if !canAdd(c.report.Omissions[reason], count) {
		return ErrCounter
	}
	if count > 0 {
		c.report.Omissions[reason] += count
	}
	return nil
}

// Finish seals the collector and derives coverage from evaluated candidates.
// Repeated calls return independent snapshots of the same report.
func (c *Collector) Finish() (*Report, error) {
	if c == nil || c.program == nil {
		return nil, ErrSequence
	}
	if !c.sealed {
		r := &c.report
		if r.ContentAdmittedFiles > MaxContentFiles || r.ContentEvaluatedFiles > r.ContentAdmittedFiles || r.ContentEligibleFiles > r.ContentCandidateFiles || r.ContentAdmittedFiles > r.ContentEligibleFiles ||
			r.ContentEvaluatedFiles > r.ContentCandidateFiles || r.ContentOmittedFiles > r.ContentCandidateFiles-r.ContentEvaluatedFiles {
			return nil, ErrSequence
		}
		for i, rule := range r.Rules {
			if rule.EvaluatedFiles > rule.CandidateFiles || rule.MatchedFiles > rule.EvaluatedFiles {
				return nil, ErrSequence
			}
			if rule.RequiresContent && c.omitted[i] > rule.CandidateFiles-rule.EvaluatedFiles {
				return nil, ErrSequence
			}
		}
		unseen := r.ContentCandidateFiles - r.ContentEvaluatedFiles - r.ContentOmittedFiles
		if unseen > 0 {
			r.Omissions[NotEvaluated] = unseen
			r.ContentOmittedFiles += unseen
		}
		for i := range r.Rules {
			if r.Rules[i].RequiresContent {
				r.Rules[i].ContentOmittedFiles = r.Rules[i].CandidateFiles - r.Rules[i].EvaluatedFiles
			}
		}
		r.Observations = append([]Observation{}, c.evidence...)
		slices.SortFunc(r.Observations, compareObservation)
		if uint64(len(r.Observations)) > r.TotalMatches {
			return nil, ErrSequence
		}
		r.OmittedMatches = r.TotalMatches - uint64(len(r.Observations))
		if r.ContentOmittedFiles > 0 || r.OmittedMatches > 0 || r.Omissions[UnsupportedPath] > 0 || r.Omissions[TreeSizeLimit] > 0 {
			r.Status = "partial"
		}
		if r.InventoryFiles == 0 && r.Omissions[TreeSizeLimit] > 0 {
			r.Status = "skipped"
		}
		c.sealed = true
	}
	result := c.report
	result.Rules = append([]RuleSummary{}, c.report.Rules...)
	result.Observations = append([]Observation{}, c.report.Observations...)
	result.Omissions = make(map[OmissionReason]uint64, len(c.report.Omissions))
	for reason, count := range c.report.Omissions {
		result.Omissions[reason] = count
	}
	return &result, nil
}

func compareObservation(a, b Observation) int {
	if order := strings.Compare(a.Path, b.Path); order != 0 {
		return order
	}
	return strings.Compare(a.RuleID, b.RuleID)
}

// The largest retained observation sits at the root, making replacement O(log
// MaxObservations), independent of the number of selected files or matches.
type observationHeap []Observation

func (h observationHeap) Len() int           { return len(h) }
func (h observationHeap) Less(i, j int) bool { return compareObservation(h[i], h[j]) > 0 }
func (h observationHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *observationHeap) Push(value any)    { *h = append(*h, value.(Observation)) }
func (h *observationHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}
func (c *Collector) retain(observation Observation) {
	if len(c.evidence) < MaxObservations {
		heap.Push(&c.evidence, observation)
		return
	}
	if compareObservation(observation, c.evidence[0]) < 0 {
		c.evidence[0] = observation
		heap.Fix(&c.evidence, 0)
	}
}
