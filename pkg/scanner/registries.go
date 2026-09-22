package scanner

import (
	"context"
	"errors"
	"os"

	"dircue/pkg/registries"
)

type registryAccumulator struct {
	collector *registries.Collector
}

func newRegistryAccumulator(opts Options, snapshot *gitSnapshot) (*registryAccumulator, error) {
	if !opts.Registries {
		return nil, nil
	}
	source := registries.Source{Mode: "directory"}
	if snapshot != nil {
		source.Mode, source.Tree = "git", snapshot.tree.Hash.String()
	}
	collector, err := registries.New(source, registries.Options{MaxFileBytes: opts.MaxFileBytes})
	if err != nil {
		return nil, err
	}
	collector.SetErrorPolicy(string(opts.ErrorPolicy))
	return &registryAccumulator{collector: collector}, nil
}

// Only a supported filename earns a callback. The original Git reader is
// captured before other profilers install a per-job content cache.
func registryCandidate(root *os.Root, item job) *registries.Candidate {
	if _, ok := registries.MatchPath(item.path); !ok {
		return nil
	}
	originalRead, filename := item.read, item.path
	return &registries.Candidate{
		Path: filename,
		Size: item.size,
		Read: func(ctx context.Context, maximum int64) ([]byte, int64, error) {
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
			if maximum < 1 || maximum > registries.MaxFileBytes+1 {
				return nil, 0, registries.ErrRead
			}
			var content []byte
			var size int64
			var err error
			if originalRead != nil {
				content, size, err = originalRead(maximum)
			} else {
				// readBoundedSize adds its own EOF-check byte.
				content, _, size, err = readBoundedSize(root, filename, maximum-1)
			}
			if canceled := ctx.Err(); canceled != nil {
				return nil, 0, canceled
			}
			if err != nil {
				return nil, 0, registryReadError(err)
			}
			if int64(len(content)) > maximum {
				return nil, 0, registries.ErrRead
			}
			return content, size, nil
		},
	}
}

func registryReadError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return registries.ErrRead
}

func (a *registryAccumulator) add(value result) error {
	if value.registryFile != nil {
		return a.collector.Add(*value.registryFile)
	}
	if _, ok := registries.MatchPath(value.path); ok {
		return a.collector.Omit("non_regular_file", 1)
	}
	return nil
}

func (a *registryAccumulator) finish(ctx context.Context) (*registries.Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.collector.Finish(ctx)
}
