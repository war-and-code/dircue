package scanner

import (
	"context"
	"github.com/war-and-code/dircue/pkg/formats"
	"os"
)

func formatCandidate(root *os.Root, item job) *formats.Candidate {
	originalRead, filename := item.read, item.path
	return &formats.Candidate{Path: filename, Size: item.size, Read: func(ctx context.Context, maximum int64) ([]byte, int64, error) {
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
