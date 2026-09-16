package codemetrics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/boyter/scc/v4/processor"
)

const EngineVersion = "4.1.0"

var (
	ErrUnsupported = errors.New("no scc grammar for language")
	ErrBinary      = errors.New("binary content cannot be counted")
	ErrEncoding    = errors.New("code metrics require UTF-8 content")
	initialize     sync.Once
)

// Counts contains physical line counts and scc's token-based complexity estimate.
// Complexity is not an AST-based cyclomatic complexity measurement.
type Counts struct {
	Grammar    string
	Lines      int64
	Code       int64
	Comment    int64
	Blank      int64
	Complexity int64
}

// Count processes the complete supplied file without walking directories or detecting
// its language again. Calls may run concurrently. The scc package globals must not
// be changed by other code in the same process after counting starts.
// Cancellation is checked before and after counting and at source line boundaries.
func Count(ctx context.Context, filename, linguistLanguage string, content []byte) (Counts, error) {
	if err := ctx.Err(); err != nil {
		return Counts{}, err
	}
	grammar, ok := Grammar(filename, linguistLanguage)
	if !ok {
		return Counts{}, fmt.Errorf("%w: %s", ErrUnsupported, linguistLanguage)
	}
	if !utf8.Valid(content) {
		return Counts{}, ErrEncoding
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return Counts{}, ErrBinary
	}

	// ProcessConstants builds grammar features but does not configure the Go GC.
	// Keeping its mutable initialization here leaves the language-only path alone.
	initialize.Do(processor.ProcessConstants)
	if err := ctx.Err(); err != nil {
		return Counts{}, err
	}
	job := processor.FileJob{
		Filename: filename, Language: grammar, Content: content, Bytes: int64(len(content)),
		Callback: cancellation{ctx},
	}
	processor.CountStats(&job)
	if err := ctx.Err(); err != nil {
		return Counts{}, err
	}
	if job.Binary {
		return Counts{}, ErrBinary
	}
	return Counts{Grammar: grammar, Lines: job.Lines, Code: job.Code, Comment: job.Comment, Blank: job.Blank, Complexity: job.Complexity}, nil
}

type cancellation struct{ context.Context }

func (c cancellation) ProcessLine(_ *processor.FileJob, _ int64, _ processor.LineType) bool {
	return c.Err() == nil
}
