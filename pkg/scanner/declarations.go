package scanner

import (
	"context"
	"os"

	"dircue/pkg/declarations"
)

func declarationCandidate(root *os.Root, item job) *declarations.Candidate {
	if !declarations.IsManifest(item.path) {
		return nil
	}
	originalRead, filename := item.read, item.path
	return &declarations.Candidate{Path: filename, Size: item.size, Read: func(ctx context.Context, maximum int64) ([]byte, int64, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if originalRead != nil {
			return originalRead(maximum)
		}
		content, _, size, err := readBoundedSize(root, filename, maximum-1)
		return content, size, err
	}}
}
