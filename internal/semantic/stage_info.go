package semantic

import (
	"slices"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/shell"
)

// DefaultShell is the default shell used by Docker for RUN instructions.
var DefaultShell = []string{"/bin/sh", "-c"}

// PackageInstall represents a package installation in a RUN command.
type PackageInstall struct {
	// Manager is the package manager used.
	Manager shell.PackageManager

	// Packages is the list of packages being installed.
	Packages []string

	// Line is the 1-based line number of the RUN instruction.
	Line int
}

// ShellSource indicates where the shell setting came from.
type ShellSource int

const (
	// ShellSourceDefault indicates the default shell is being used.
	ShellSourceDefault ShellSource = iota
	// ShellSourceInstruction indicates the shell was set via SHELL instruction.
	ShellSourceInstruction
	// ShellSourceDirective indicates the shell was set via a comment directive.
	ShellSourceDirective
)

// ShellSetting represents the active shell configuration for a stage.
type ShellSetting struct {
	// Shell is the shell command array (e.g., ["/bin/bash", "-c"]).
	Shell []string

	// Variant is the parsed shell variant for use with the shell parser.
	Variant shell.Variant

	// Source indicates where this shell setting came from.
	Source ShellSource

	// Line is the 0-based line number where the shell was set (for directives/instructions).
	// -1 for default shell.
	Line int
}

// StageInfo contains enhanced information about a build stage.
// It augments BuildKit's instructions.Stage with semantic analysis data.
type StageInfo struct {
	// Index is the 0-based stage index.
	Index int

	// Stage is a reference to the BuildKit stage.
	Stage *instructions.Stage

	// Shell is the active shell for this stage (from SHELL instruction).
	// Defaults to ["/bin/sh", "-c"] if no SHELL instruction is present.
	//
	// Deprecated: Use ShellSetting instead for more detailed information.
	Shell []string

	// ShellSetting contains the active shell configuration including variant and source.
	ShellSetting ShellSetting

	// BaseImage contains information about the FROM image reference.
	BaseImage *BaseImageRef

	// Variables contains the variable scope for this stage.
	Variables *VariableScope

	// CopyFromRefs contains all COPY --from references in this stage.
	CopyFromRefs []CopyFromRef

	// OnbuildCopyFromRefs contains COPY --from references in ONBUILD instructions.
	// These are triggered when the image is used as a base for another build.
	OnbuildCopyFromRefs []CopyFromRef

	// InstalledPackages contains packages installed via system package managers.
	// Tracked from RUN commands that use apt-get, apk, yum, dnf, etc.
	InstalledPackages []PackageInstall

	// IsLastStage is true if this is the final stage in the Dockerfile.
	IsLastStage bool
}

// HasPackage checks if a package was installed in this stage.
func (s *StageInfo) HasPackage(pkg string) bool {
	for _, install := range s.InstalledPackages {
		if slices.Contains(install.Packages, pkg) {
			return true
		}
	}
	return false
}

// IsExternalImage returns true if this stage's base image is an external image
// (not "scratch" and not a reference to another stage in the Dockerfile).
// This is useful for rules that need to check image tags/versions.
func (s *StageInfo) IsExternalImage() bool {
	if s.Stage == nil {
		return false
	}
	// scratch is a special "no base" image
	if s.Stage.BaseName == "scratch" {
		return false
	}
	// Check if it references another stage
	if s.BaseImage != nil && s.BaseImage.IsStageRef {
		return false
	}
	return true
}

// PackageManagers returns the set of package managers used in this stage.
func (s *StageInfo) PackageManagers() []shell.PackageManager {
	seen := make(map[shell.PackageManager]bool)
	var managers []shell.PackageManager
	for _, install := range s.InstalledPackages {
		if !seen[install.Manager] {
			seen[install.Manager] = true
			managers = append(managers, install.Manager)
		}
	}
	return managers
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
	defaultShell := make([]string, len(DefaultShell))
	copy(defaultShell, DefaultShell)

	return &StageInfo{
		Index: index,
		Stage: stage,
		Shell: defaultShell,
		ShellSetting: ShellSetting{
			Shell:   defaultShell,
			Variant: shell.VariantFromShellCmd(defaultShell),
			Source:  ShellSourceDefault,
			Line:    -1,
		},
		IsLastStage: isLast,
	}
}
