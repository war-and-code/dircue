package formats

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"unicode/utf8"
)

type candidates []Candidate

func (h candidates) Len() int           { return len(h) }
func (h candidates) Less(i, j int) bool { return h[i].Path > h[j].Path }
func (h candidates) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidates) Push(v any)        { *h = append(*h, v.(Candidate)) }
func (h *candidates) Pop() any          { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }

type Collector struct {
	report         Report
	selected       candidates
	maxSourceBytes int64
	finished       bool
	finishErr      error
	// errorPolicy controls per-file read handling at Finish time. "continue"
	// records an omission per unreadable candidate; anything else fails hard,
	// preserving the default trust boundary.
	errorPolicy string
	readErrors  []string
}

// SetErrorPolicy configures how Finish reacts to per-file read failures.
// Pass "continue" to record a "file_read_error" omission and keep the
// remaining candidates; the default preserves the historical fail-fast
// contract. Call before Finish.
func (c *Collector) SetErrorPolicy(policy string) { c.errorPolicy = policy }

// ReadErrors returns the paths of selected candidates that Finish skipped
// because their bounded read failed under the continue policy. The caller
// uses this to emit top-level "file_read_error" warnings alongside the
// module's own omission accounting. Order matches Finish's read order.
func (c *Collector) ReadErrors() []string { return c.readErrors }

func New(mode, tree string, maxSourceBytes int64) *Collector {
	consistency := "live_directory_reads"
	if mode == "git" {
		consistency = "selected_git_tree"
	}
	return &Collector{maxSourceBytes: maxSourceBytes, report: Report{
		Provider: "dircue-formats", ProviderVersion: ProviderVersion, Status: "complete",
		Source:       Source{mode, tree, consistency},
		Scope:        Scope{"selected_regular_files_including_vendor_and_data", "lexically_first_paths_within_limits", false},
		Limits:       Limits{FileBytes: MaxFileBytes, InputBytes: MaxInputBytes, Files: MaxFiles, PathBytes: MaxPathBytes, OutputBytes: MaxOutputBytes, ParserDepth: MaxDepth, ParserTokens: MaxTokens, MaxSourceFileBytes: maxSourceBytes},
		Observations: []Observation{}, Omissions: map[string]int64{},
	}}
}

// Omit records an excluded source entry, such as a symlink; it is not a selected regular file.
func (c *Collector) Omit(reason string)     { c.report.Omissions[reason]++; c.report.Status = "partial" }
func (c *Collector) skipFile(reason string) { c.Omit(reason); c.report.Coverage.OmittedFiles++ }
func (c *Collector) Skip(reason string) *Report {
	c.Omit(reason)
	c.report.Status = "skipped"
	return &c.report
}

// Add accepts each selected regular file once. Calls must be serialized by the scanner.
func (c *Collector) Add(file Candidate) {
	c.report.Coverage.SelectedFiles++
	if file.Size >= 0 {
		c.report.Coverage.SelectedBytes += file.Size
	}
	if !utf8.ValidString(file.Path) || !fs.ValidPath(file.Path) || file.Path == "." || len(file.Path) > MaxPathBytes {
		c.skipFile("unrepresentable_path")
		return
	}
	if file.Size < 0 {
		c.skipFile("invalid_file_size")
		return
	}
	if c.maxSourceBytes > 0 && file.Size > c.maxSourceBytes {
		c.skipFile("source_file_size_limit")
		return
	}
	if len(c.selected) < MaxFiles {
		heap.Push(&c.selected, file)
		return
	}
	c.skipFile("file_count_limit")
	if file.Path < c.selected[0].Path {
		c.selected[0] = file
		heap.Fix(&c.selected, 0)
	}
}
func (c *Collector) Finish(ctx context.Context) (report *Report, err error) {
	if c.finished {
		return &c.report, c.finishErr
	}
	c.finished = true
	defer func() { c.finishErr = err }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(c.selected, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	// Reserve space for report metadata and omissions; each serialized observation is charged separately.
	outputBytes := 4096
	for _, file := range c.selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := MaxInputBytes - c.report.Coverage.InspectedBytes
		if remaining <= 0 {
			c.skipFile("input_byte_limit")
			continue
		}
		if file.Read == nil {
			return nil, errors.New("format candidate has no selected-source reader")
		}
		request := min(MaxFileBytes+1, remaining)
		content, size, err := file.Read(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if c.errorPolicy == "continue" {
				// The candidate remains counted in SelectedFiles and its path is
				// recorded so the scanner emits a same-shaped file_read_error
				// warning alongside this module-owned omission.
				c.skipFile("file_read_error")
				c.readErrors = append(c.readErrors, file.Path)
				continue
			}
			return nil, errors.New("could not read a selected format candidate")
		}
		if int64(len(content)) > request {
			return nil, errors.New("format source reader exceeded its requested byte count")
		}
		c.report.Coverage.InspectedFiles++
		c.report.Coverage.InspectedBytes += int64(len(content))
		readBytes := int64(len(content))
		complete := size >= 0 && size == readBytes && size <= MaxFileBytes
		limited := content[:min(int64(len(content)), MaxFileBytes)]
		observation := inspect(file.Path, limited, complete)
		observation.Bytes = file.Size
		observation.BytesRead = readBytes
		if complete {
			observation.ReadScope = "complete"
			c.report.Coverage.CompleteReads++
		} else {
			observation.ReadScope = "prefix"
			c.report.Coverage.PrefixReads++
		}
		if size != file.Size || size < readBytes || readBytes < min(size, request) {
			observation.Diagnostics = append(observation.Diagnostics, "source_changed_or_incomplete_read")
			c.report.Status = "partial"
			// A live source that changed size cannot establish whole-file validation.
			for i := range observation.Evidence {
				if observation.Evidence[i].Basis == "complete_validation" {
					observation.Evidence[i].Basis = "parsed_prefix"
				}
			}
			if observation.ReadScope == "complete" {
				observation.ReadScope = "prefix"
				c.report.Coverage.CompleteReads--
				c.report.Coverage.PrefixReads++
			}
		}
		encoded, err := json.Marshal(observation)
		if err != nil {
			return nil, err
		}
		if len(encoded)+1 > MaxOutputBytes-outputBytes {
			c.Omit("output_byte_limit")
			continue
		}
		outputBytes += len(encoded) + 1
		c.report.Observations = append(c.report.Observations, observation)
	}
	c.report.Coverage.RetainedObservations = int64(len(c.report.Observations))
	return &c.report, nil
}
