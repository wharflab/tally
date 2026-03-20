package autofixdata

import (
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/dockerfile"
	patchutil "github.com/wharflab/tally/internal/patch"
)

// OutputMode controls whether the agent outputs a unified diff patch or a
// full Dockerfile.
type OutputMode string

const (
	OutputPatch      OutputMode = "patch"
	OutputDockerfile OutputMode = "dockerfile"
)

// BlockingIssue represents a validation problem that blocks acceptance of a
// proposed Dockerfile. It is serialized as JSON in retry prompts.
type BlockingIssue struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Objective encapsulates objective-specific behavior for the AI AutoFix resolver.
// Each objective provides prompt construction, validation, and acceptance
// heuristics while sharing the ACP runner, retries, redaction, and patch parsing.
//
// Objectives are registered at init time and looked up by ObjectiveKind when
// the resolver processes a fix request.
type Objective interface {
	// Kind returns the unique identifier for this objective.
	Kind() ObjectiveKind

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
	ValidateProposal(orig, proposed *dockerfile.ParseResult) []BlockingIssue

	// ValidatePatch performs objective-specific validation on patch metadata
	// (e.g. ensuring certain instructions were added). Returns blocking issues.
	ValidatePatch(meta patchutil.Meta) []BlockingIssue
}

// PromptContext provides inputs for building the initial (round 1) prompt.
type PromptContext struct {
	FilePath string
	Source   []byte
	Request  *ObjectiveRequest
	Config   *config.Config

	// AbsPath is the absolute filesystem path to the Dockerfile.
	// Empty for stdin or virtual files (e.g. LSP unsaved buffers).
	// Agents can use this to access surrounding files in the build context.
	AbsPath string

	// ContextDir is the explicit build context directory (from --context).
	// Empty when not provided by the user.
	ContextDir string

	OrigParse *dockerfile.ParseResult
	Mode      OutputMode
}

// RetryPromptContext provides inputs for building a retry (round 2+) prompt.
type RetryPromptContext struct {
	FilePath       string
	Proposed       []byte
	BlockingIssues []BlockingIssue
	Config         *config.Config
	Mode           OutputMode
}

// SimplifiedPromptContext provides inputs for building a minimal fallback prompt.
type SimplifiedPromptContext struct {
	FilePath string
	Source   []byte
	Mode     OutputMode
}

var objectives = make(map[ObjectiveKind]Objective)

// RegisterObjective registers an Objective implementation.
// Panics if an objective with the same Kind is already registered.
// Typically called from init() in the package that defines the objective.
func RegisterObjective(o Objective) {
	kind := o.Kind()
	if _, exists := objectives[kind]; exists {
		panic("autofixdata: duplicate objective registration: " + string(kind))
	}
	objectives[kind] = o
}

// GetObjective returns the registered Objective for the given kind.
func GetObjective(kind ObjectiveKind) (Objective, bool) {
	o, ok := objectives[kind]
	return o, ok
}
