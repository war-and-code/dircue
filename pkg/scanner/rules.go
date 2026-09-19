package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"dircue/pkg/rules"
)

// Pending candidates retain metadata and the original Git object reader only.
// They never retain a classifier's per-job cached content.
type pendingRuleFile struct {
	file rules.File
	read func(int64) ([]byte, int64, error)
}

type rulesAccumulator struct {
	collector *rules.Collector
	pending   []pendingRuleFile
	limit     int64
}

func newRulesAccumulator(opts Options, snapshot *gitSnapshot) (*rulesAccumulator, error) {
	if opts.Rules == nil {
		return nil, nil
	}
	source := rules.Source{Kind: "directory"}
	if snapshot != nil {
		source.Kind, source.Tree = "git", snapshot.tree.Hash.String()
	}
	collector, err := rules.New(opts.Rules, source, rules.Options{MaxFileBytes: opts.MaxFileBytes})
	if err != nil {
		return nil, fmt.Errorf("initialize rules: %w", err)
	}
	limit := int64(rules.MaxContentBytes)
	if opts.MaxFileBytes > 0 {
		limit = min(limit, opts.MaxFileBytes)
	}
	return &rulesAccumulator{collector: collector, limit: limit}, nil
}

func (a *rulesAccumulator) add(value result) error {
	if value.rulesFile == nil {
		if value.path != "" {
			return a.collector.Omit(rules.NonRegularFile, 1)
		}
		return nil
	}
	file := *value.rulesFile
	eligible, err := a.collector.ObserveMetadata(file)
	if errors.Is(err, rules.ErrFile) {
		return a.collector.Omit(rules.UnsupportedPath, 1)
	}
	if err != nil || !eligible {
		return err
	}
	candidate := pendingRuleFile{file: file, read: value.rulesRead}
	index, _ := slices.BinarySearchFunc(a.pending, file.Path, func(v pendingRuleFile, path string) int {
		return strings.Compare(v.file.Path, path)
	})
	if len(a.pending) == rules.MaxContentFiles {
		if index == len(a.pending) {
			return a.collector.OmitContent(file, rules.ContentBudget)
		}
		last := len(a.pending) - 1
		if err := a.collector.OmitContent(a.pending[last].file, rules.ContentBudget); err != nil {
			return err
		}
		// Clear the removed closure before shortening the slice.
		a.pending[last] = pendingRuleFile{}
		a.pending = a.pending[:last]
	}
	a.pending = slices.Insert(a.pending, index, candidate)
	return nil
}

func (a *rulesAccumulator) finish(ctx context.Context, root *os.Root) (*rules.Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, pending := range a.pending {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var content []byte
		var size int64
		var err error
		if pending.read != nil {
			content, size, err = pending.read(a.limit + 1)
		} else {
			content, _, size, err = readBoundedSize(root, pending.file.Path, a.limit)
		}
		if err != nil {
			return nil, fmt.Errorf("read rule candidate %s: %w", pending.file.Path, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch {
		case size != pending.file.Size || int64(len(content)) != size || int64(len(content)) > a.limit:
			err = a.collector.OmitContent(pending.file, rules.Incomplete)
		case !utf8.Valid(content):
			err = a.collector.OmitContent(pending.file, rules.InvalidUTF8)
		default:
			err = a.collector.ObserveContent(pending.file, content)
		}
		if err != nil {
			return nil, fmt.Errorf("evaluate rule candidate %s: %w", pending.file.Path, err)
		}
	}
	clear(a.pending)
	a.pending = nil
	return a.collector.Finish()
}
