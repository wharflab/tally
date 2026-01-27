package semantic

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// DefaultShell is the default shell used by Docker for RUN instructions.
var DefaultShell = []string{"/bin/sh", "-c"}

// StageInfo contains enhanced information about a build stage.
// It augments BuildKit's instructions.Stage with semantic analysis data.
type StageInfo struct {
	// Index is the 0-based stage index.
	Index int

	// Stage is a reference to the BuildKit stage.
	Stage *instructions.Stage

	// Shell is the active shell for this stage (from SHELL instruction).
	// Defaults to ["/bin/sh", "-c"] if no SHELL instruction is present.
	Shell []string

	// BaseImage contains information about the FROM image reference.
	BaseImage *BaseImageRef

	// Variables contains the variable scope for this stage.
	Variables *VariableScope

	// CopyFromRefs contains all COPY --from references in this stage.
	CopyFromRefs []CopyFromRef

	// OnbuildCopyFromRefs contains COPY --from references in ONBUILD instructions.
	// These are triggered when the image is used as a base for another build.
	OnbuildCopyFromRefs []CopyFromRef

	// IsLastStage is true if this is the final stage in the Dockerfile.
	IsLastStage bool
}

// BaseImageRef contains information about a stage's base image.
type BaseImageRef struct {
	// Raw is the original base image string (e.g., "ubuntu:22.04", "builder").
	Raw string

	// IsStageRef is true if this references another stage in the Dockerfile.
	IsStageRef bool

	// StageIndex is the index of the referenced stage, or -1 if external image.
	StageIndex int

	// Platform is the --platform value if specified.
	Platform string

	// Location is the location of the FROM instruction.
	Location []parser.Range
}

// CopyFromRef contains information about a COPY --from reference.
type CopyFromRef struct {
	// From is the original --from value.
	From string

	// IsStageRef is true if this references another stage.
	IsStageRef bool

	// StageIndex is the index of the referenced stage, or -1 if not found/external.
	StageIndex int

	// Command is a reference to the COPY instruction.
	Command *instructions.CopyCommand

	// Location is the location of the COPY instruction.
	Location []parser.Range
}

// newStageInfo creates a new StageInfo with default values.
func newStageInfo(index int, stage *instructions.Stage, isLast bool) *StageInfo {
	// Copy default shell to avoid mutation
	shell := make([]string, len(DefaultShell))
	copy(shell, DefaultShell)

	return &StageInfo{
		Index:       index,
		Stage:       stage,
		Shell:       shell,
		IsLastStage: isLast,
	}
}
