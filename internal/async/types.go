// Package async provides a concurrency-limited runtime for executing potentially slow
// checks (registry access, network, filesystem) in a controlled, cancellable way.
package async

import "time"

// Category classifies the kind of I/O a check request requires.
type Category string

const (
	CategoryNetwork    Category = "network"
	CategoryFilesystem Category = "filesystem"
	CategoryConsole    Category = "console"
)

// ResultHandler processes a resolved result and returns violations.
// The return value is opaque to the runtime; the caller (lint.go) casts it
// to []rules.Violation. This avoids an import cycle between async and rules.
//
// Return semantics:
//   - nil: handler could not process this value (e.g., wrong type); the
//     runtime will NOT mark this handler's request as completed.
//   - non-nil (including empty slice): handler processed the value; the
//     runtime marks it as completed, replacing fast-path violations.
type ResultHandler interface {
	OnSuccess(resolved any) []any
}

// CheckRequest declares a planned unit of async work.
type CheckRequest struct {
	// RuleCode identifies the rule that created this request.
	RuleCode string

	// Category classifies the I/O type (reserved for future per-category routing).
	Category Category

	// Key is a fully-specific cache/dedupe key encoding ref+platform+options.
	// Requests with the same (ResolverID, Key) are deduplicated.
	Key string

	// ResolverID routes to a registered Resolver implementation.
	ResolverID string

	// Data is resolver-specific typed input.
	Data any

	// Timeout is a per-request budget. Zero means use global timeout only.
	Timeout time.Duration

	// Handler converts resolved data into violations.
	Handler ResultHandler

	// File is the source file this request is associated with.
	File string

	// StageIndex identifies the stage this request is for (used for merging).
	StageIndex int
}

// SkipReason explains why an async check was not completed.
type SkipReason string

const (
	SkipDisabled    SkipReason = "disabled"
	SkipFailFast    SkipReason = "fail-fast"
	SkipAuth        SkipReason = "auth"
	SkipNotFound    SkipReason = "not-found"
	SkipNetwork     SkipReason = "network"
	SkipTimeout     SkipReason = "timeout"
	SkipResolverErr SkipReason = "resolver-error"
)

// Skipped records a check that was planned but not completed.
type Skipped struct {
	Request CheckRequest
	Reason  SkipReason
	Err     error
}

// CompletedCheck records a successfully completed async check (even if it
// produced zero violations). Used by the merge logic to know which fast-path
// violations should be replaced.
type CompletedCheck struct {
	RuleCode   string
	File       string
	StageIndex int
}

// RunResult contains the output of an async runtime execution.
type RunResult struct {
	// Violations is a flat list of opaque violation values.
	// The caller casts these to rules.Violation.
	Violations []any
	Skipped    []Skipped
	// Completed records every (rule, file, stage) that resolved successfully.
	Completed []CompletedCheck
}
