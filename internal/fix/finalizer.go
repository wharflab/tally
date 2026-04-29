package fix

import (
	"context"
	"slices"

	"github.com/wharflab/tally/internal/rules"
)

// FinalizeContext contains the current file content after all normal fixes have run.
type FinalizeContext struct {
	FilePath string
	Content  []byte
}

// Finalizer computes a final set of edits after sync and async fixes have been applied.
//
// Finalizers are intended for cleanup rules whose violations may only appear
// after another fix creates new source text.
type Finalizer interface {
	RuleCode() string
	Description() string
	Safety() rules.FixSafety
	Priority() int
	Finalize(ctx context.Context, finalizeCtx FinalizeContext) ([]rules.TextEdit, error)
}

var finalizers []Finalizer

// RegisterFinalizer registers a post-fix finalizer.
func RegisterFinalizer(finalizer Finalizer) {
	if finalizer == nil {
		return
	}
	finalizers = append(finalizers, finalizer)
}

func registeredFinalizers() []Finalizer {
	return slices.Clone(finalizers)
}
