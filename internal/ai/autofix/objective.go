package autofix

import (
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/dockerfile"
	patchutil "github.com/wharflab/tally/internal/patch"
)

// Objective encapsulates objective-specific behavior for the AI AutoFix resolver.
// Each objective provides prompt construction, validation, and acceptance
// heuristics while sharing the ACP runner, retries, redaction, and patch parsing.
//
// Objectives are registered at init time and looked up by ObjectiveKind when
// the resolver processes a fix request.
type Objective interface {
	// Kind returns the unique identifier for this objective.
	Kind() autofixdata.ObjectiveKind

	// BuildPrompt constructs the initial (round 1) prompt for the agent.
	BuildPrompt(ctx PromptContext) (string, error)

	// BuildRetryPrompt constructs a follow-up prompt (round 2+) that includes
	// blocking issues from the previous round for the agent to fix.
	BuildRetryPrompt(ctx RetryPromptContext) (string, error)

	// BuildSimplifiedPrompt constructs a minimal fallback prompt used when
	// the agent produces malformed output.
	BuildSimplifiedPrompt(ctx SimplifiedPromptContext) string

	// ValidateProposal performs objective-specific structural validation on
	// the proposed Dockerfile beyond basic syntax checking.
	// Returns blocking issues that must be resolved.
	ValidateProposal(orig, proposed *dockerfile.ParseResult) []blockingIssue

	// ValidatePatch performs objective-specific validation on patch metadata
	// (e.g. ensuring certain instructions were added). Returns blocking issues.
	ValidatePatch(meta patchutil.Meta) []blockingIssue
}

// PromptContext provides inputs for building the initial (round 1) prompt.
type PromptContext struct {
	FilePath string
	Source   []byte
	Request  *autofixdata.ObjectiveRequest
	Config   *config.Config

	// AbsPath is the absolute filesystem path to the Dockerfile.
	// Empty for stdin or virtual files (e.g. LSP unsaved buffers).
	// Agents can use this to access surrounding files in the build context.
	AbsPath string

	// ContextDir is the explicit build context directory (from --context).
	// Empty when not provided by the user.
	ContextDir string

	OrigParse *dockerfile.ParseResult
	Mode      agentOutputMode
}

// RetryPromptContext provides inputs for building a retry (round 2+) prompt.
type RetryPromptContext struct {
	FilePath       string
	Proposed       []byte
	BlockingIssues []blockingIssue
	Config         *config.Config
	Mode           agentOutputMode
}

// SimplifiedPromptContext provides inputs for building a minimal fallback prompt.
type SimplifiedPromptContext struct {
	FilePath string
	Source   []byte
	Mode     agentOutputMode
}

var objectives = make(map[autofixdata.ObjectiveKind]Objective)

func registerObjective(o Objective) {
	objectives[o.Kind()] = o
}

func getObjective(kind autofixdata.ObjectiveKind) (Objective, bool) {
	o, ok := objectives[kind]
	return o, ok
}
