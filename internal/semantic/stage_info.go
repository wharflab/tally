package semantic

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/shell"
)

var windowsDrivePathPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9_])[a-z]:[\\/]`)

var windowsEnvVarPattern = regexp.MustCompile(`(?i)%[a-z_][a-z0-9_()]*%`)

var windowsFileSuffixPattern = regexp.MustCompile(`(?i)\.(exe|msi|ps1|cmd|bat)\b`)

var linuxPathHintPattern = regexp.MustCompile(`(^|[^a-z0-9_])/(bin|usr|etc|var|tmp|home|opt|sbin)\b`)

var windowsCommandHints = []string{
	"setx",
	"icacls",
	"certutil",
	"robocopy",
	"dism",
	"msiexec",
	"reg ",
	"choco",
	"ngen",
}

var windowsIdentityHints = []string{
	"containeradministrator",
	"containeruser",
	"defaultaccount",
}

var linuxCommandHints = []string{
	"apt-get",
	"apt ",
	"apk ",
	"yum ",
	"dnf ",
	"microdnf",
	"pacman",
	"zypper",
	"chmod",
	"chown",
}

// BaseImageOS represents the detected operating system of a stage's base image.
type BaseImageOS int

const (
	// BaseImageOSUnknown means the OS could not be determined from static analysis.
	BaseImageOSUnknown BaseImageOS = iota
	// BaseImageOSLinux indicates a Linux-based base image.
	BaseImageOSLinux
	// BaseImageOSWindows indicates a Windows-based base image.
	BaseImageOSWindows
)

// DefaultShell is the default shell used by Docker for Linux RUN instructions.
var DefaultShell = []string{"/bin/sh", "-c"}

// defaultWindowsShellExe is the Windows cmd.exe executable name used as the
// default shell for Windows container RUN instructions.
const defaultWindowsShellExe = "cmd" //nolint:customlint // not a Dockerfile CMD instruction

// DefaultWindowsShell returns the default shell for Windows containers.
// Returns a fresh copy to avoid mutation.
func DefaultWindowsShell() []string {
	return []string{defaultWindowsShellExe, "/S", "/C"}
}

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
	// Shell is the shell command array used to execute RUN instructions
	// (Docker semantics), e.g., ["/bin/bash", "-c"].
	Shell []string

	// Variant is the shell variant used for lint parsing (may be influenced
	// by inline directives like "# hadolint shell=bash").
	Variant shell.Variant

	// Source indicates where Variant came from.
	Source ShellSource

	// Line is the 0-based line number where the shell was set (for directives/instructions).
	// -1 for default shell.
	Line int
}

// HeredocShellOverride records a per-instruction shell override from a
// BuildKit heredoc shebang line (e.g., #!/bin/bash in a RUN <<EOF body).
// Docker respects these shebangs and uses the specified interpreter.
type HeredocShellOverride struct {
	// Line is the 1-based Dockerfile line of the RUN instruction.
	Line int

	// Shell is the shell name from the shebang (e.g., "bash", "sh", "ksh").
	Shell string

	// Variant is the shell variant derived from Shell.
	Variant shell.Variant
}

// OnbuildInstruction represents a parsed ONBUILD trigger command.
type OnbuildInstruction struct {
	// Command is the parsed typed command (RunCommand, CopyCommand, etc.).
	Command instructions.Command

	// SourceLine is the original 1-based line number of the ONBUILD instruction
	// in the Dockerfile.
	SourceLine int
}

// StageInfo contains enhanced information about a build stage.
// It augments BuildKit's instructions.Stage with semantic analysis data.
type StageInfo struct {
	// Index is the 0-based stage index.
	Index int

	// Stage is a reference to the BuildKit stage.
	Stage *instructions.Stage

	// BaseImageOS is the detected operating system of the base image.
	// Determined by heuristics (image name, platform, escape directive, SHELL instruction).
	BaseImageOS BaseImageOS

	// ShellSetting contains the active shell configuration including variant and source.
	ShellSetting ShellSetting

	// BaseImage contains information about the FROM image reference.
	BaseImage *BaseImageRef

	// FromArgs contains semantic analysis results for the stage's FROM instruction
	// (ARG usage in base name and platform, and default validity checks).
	FromArgs FromArgsInfo

	// Variables contains the variable scope for this stage.
	Variables *VariableScope

	// EffectiveEnv is the approximate effective environment for this stage after
	// evaluating ARG and ENV instructions (matching BuildKit's word expansion
	// environment semantics for linting).
	//
	// It is used for UndefinedVar analysis and for inheriting environment keys
	// when another stage uses this stage as its base.
	EffectiveEnv map[string]string

	// UndefinedVars contains variable references (e.g., $FOO) used in stage
	// commands that are not defined at the point of use.
	UndefinedVars []UndefinedVarRef

	// CopyFromRefs contains all COPY --from references in this stage.
	CopyFromRefs []CopyFromRef

	// OnbuildCopyFromRefs contains COPY --from references in ONBUILD instructions.
	// These are triggered when the image is used as a base for another build.
	OnbuildCopyFromRefs []CopyFromRef

	// OnbuildInstructions contains all parsed ONBUILD trigger commands for this stage.
	// Each ONBUILD expression is parsed into a typed command using BuildKit's parser.
	OnbuildInstructions []OnbuildInstruction

	// HeredocShellOverrides contains per-instruction shell overrides detected
	// from heredoc shebang lines. Rules can use this to determine the effective
	// shell for a specific RUN instruction instead of the stage-level shell.
	HeredocShellOverrides []HeredocShellOverride

	// InstalledPackages contains packages installed via system package managers.
	// Tracked from RUN commands that use apt-get, apk, yum, dnf, etc.
	InstalledPackages []PackageInstall

	// IsLastStage is true if this is the final stage in the Dockerfile.
	IsLastStage bool
}

// IsWindows returns true if the base image was detected as Windows.
func (s *StageInfo) IsWindows() bool {
	return s.BaseImageOS == BaseImageOSWindows
}

// IsLinux returns true if the base image was detected as Linux.
func (s *StageInfo) IsLinux() bool {
	return s.BaseImageOS == BaseImageOSLinux
}

// IsScratch returns true if this stage uses FROM scratch as its base image.
func (s *StageInfo) IsScratch() bool {
	return s.Stage != nil && s.Stage.BaseName == "scratch"
}

// parseImageRef parses a Docker image reference into domain, repository path, and tag.
// Uses github.com/distribution/reference for correct handling of registries, digests, etc.
// Returns lowercased components. On parse failure, falls back to simple string splitting.
func parseImageRef(raw string) (string, string, string) {
	named, err := reference.ParseNormalizedNamed(raw)
	if err != nil {
		// Fallback for unparseable refs (e.g. stage names, empty strings).
		// Simple split: everything before first ":" or "@" is the name.
		name := raw
		var tag string
		if i := strings.IndexAny(name, ":@"); i >= 0 {
			tag = name[i+1:]
			name = name[:i]
		}
		return "", strings.ToLower(name), strings.ToLower(tag)
	}

	domain := strings.ToLower(reference.Domain(named))
	repoPath := strings.ToLower(reference.Path(named))
	var tag string
	if tagged, ok := named.(reference.Tagged); ok {
		tag = strings.ToLower(tagged.Tag())
	}
	return domain, repoPath, tag
}

// detectBaseImageOS determines the OS from the base image name and platform.
// Uses a fast heuristic — no network calls.
func detectBaseImageOS(baseName, platform string) BaseImageOS {
	lower := strings.ToLower(baseName)

	// Explicit --platform=windows/* is a strong signal.
	if platform != "" {
		lp := strings.ToLower(platform)
		if strings.Contains(lp, "windows") {
			return BaseImageOSWindows
		}
		if strings.Contains(lp, "linux") {
			return BaseImageOSLinux
		}
	}

	// Windows image name patterns.
	if isWindowsImageName(lower) {
		return BaseImageOSWindows
	}

	// Well-known Linux distros.
	if isLinuxImageName(lower) {
		return BaseImageOSLinux
	}

	return BaseImageOSUnknown
}

// isWindowsImageName returns true if the image name is a known Windows image.
// Uses github.com/distribution/reference for correct parsing of registry prefixes,
// tags, and digests.
func isWindowsImageName(lower string) bool {
	domain, repoPath, tag := parseImageRef(lower)

	if domain != "mcr.microsoft.com" {
		return false
	}

	// MCR Windows images: windows, windows/servercore, windows/nanoserver, etc.
	if repoPath == "windows" || strings.HasPrefix(repoPath, "windows/") {
		return true
	}

	// .NET or PowerShell images with Windows tag markers
	if strings.HasPrefix(repoPath, "dotnet/") || strings.HasPrefix(repoPath, "powershell") {
		if strings.Contains(tag, "nanoserver") || strings.Contains(tag, "windowsservercore") {
			return true
		}
	}

	return false
}

// isLinuxImageName returns true if the image name is a well-known Linux-based image.
// Uses github.com/distribution/reference for correct parsing of registry prefixes,
// tags, and digests.
func isLinuxImageName(lower string) bool {
	domain, repoPath, tag := parseImageRef(lower)

	// Extract the short name (last path segment, e.g. "alpine" from "library/alpine")
	name := path.Base(repoPath)

	switch name {
	case "alpine", "ubuntu", "debian", "fedora", "centos", "rockylinux",
		"almalinux", "amazonlinux", "al2023", "al2",
		"archlinux", "clearlinux", "oraclelinux",
		"busybox", "distroless", "chainguard", "wolfi", "photon",
		"opensuse", "sles", "gentoo":
		return true
	}

	// Images under well-known Linux org prefixes (e.g. kalilinux/kali-rolling)
	if strings.HasPrefix(repoPath, "kalilinux/") {
		return true
	}

	// MCR Linux images (dotnet on Linux, powershell on Linux)
	if domain == "mcr.microsoft.com" {
		if strings.HasPrefix(repoPath, "dotnet/") || strings.HasPrefix(repoPath, "powershell") {
			if !strings.Contains(tag, "nanoserver") && !strings.Contains(tag, "windowsservercore") {
				return true
			}
		}
	}

	return false
}

func inferStageOSHeuristically(stage *instructions.Stage) BaseImageOS {
	if stage == nil {
		return BaseImageOSUnknown
	}

	var windowsScore, linuxScore int
	for _, cmd := range stage.Commands {
		addInstructionOSHeuristics(cmd, &windowsScore, &linuxScore)
	}

	switch {
	case windowsScore >= 6 && windowsScore >= linuxScore+3:
		return BaseImageOSWindows
	case linuxScore >= 6 && linuxScore >= windowsScore+3:
		return BaseImageOSLinux
	default:
		return BaseImageOSUnknown
	}
}

func addInstructionOSHeuristics(cmd instructions.Command, windowsScore, linuxScore *int) {
	if cmd == nil {
		return
	}

	text := strings.ToLower(fmt.Sprint(cmd))
	if text != "" {
		if windowsDrivePathPattern.MatchString(text) {
			*windowsScore += 3
		}
		if windowsEnvVarPattern.MatchString(text) {
			*windowsScore += 2
		}
		if windowsFileSuffixPattern.MatchString(text) {
			*windowsScore++
		}
		if linuxPathHintPattern.MatchString(text) {
			*linuxScore += 2
		}
		*windowsScore += min(2, countTextHints(text, windowsCommandHints)) * 3
		*windowsScore += min(1, countTextHints(text, windowsIdentityHints)) * 2
		*linuxScore += min(2, countTextHints(text, linuxCommandHints)) * 3
	}

	switch c := cmd.(type) {
	case *instructions.RunCommand:
		if len(c.CmdLine) == 0 {
			return
		}
		if c.PrependShell {
			if inv, ok := shell.ParseExplicitShellInvocation(c.CmdLine[0]); ok {
				addShellSignalScore(inv.ShellName, windowsScore, linuxScore)
			}
			return
		}
		addShellSignalScore(c.CmdLine[0], windowsScore, linuxScore)
	case *instructions.CmdCommand:
		if len(c.CmdLine) == 0 {
			return
		}
		if c.PrependShell {
			if inv, ok := shell.ParseExplicitShellInvocation(c.CmdLine[0]); ok {
				addShellSignalScore(inv.ShellName, windowsScore, linuxScore)
			}
			return
		}
		addShellSignalScore(c.CmdLine[0], windowsScore, linuxScore)
	case *instructions.EntrypointCommand:
		if len(c.CmdLine) == 0 {
			return
		}
		if c.PrependShell {
			if inv, ok := shell.ParseExplicitShellInvocation(c.CmdLine[0]); ok {
				addShellSignalScore(inv.ShellName, windowsScore, linuxScore)
			}
			return
		}
		addShellSignalScore(c.CmdLine[0], windowsScore, linuxScore)
	case *instructions.ShellCommand:
		if len(c.Shell) == 0 {
			return
		}
		addShellSignalScore(c.Shell[0], windowsScore, linuxScore)
	}
}

func addShellSignalScore(shellName string, windowsScore, linuxScore *int) {
	switch normalizeShellSignalName(shellName) {
	case command.Cmd, "powershell":
		*windowsScore += 6
	case "sh", "bash", "dash", "ash", "zsh", "ksh", "mksh":
		*linuxScore += 6
	}
}

func countTextHints(text string, hints []string) int {
	count := 0
	for _, hint := range hints {
		if strings.Contains(text, hint) {
			count++
		}
	}
	return count
}

func normalizeShellSignalName(exe string) string {
	exe = strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	return strings.TrimSuffix(exe, ".exe")
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
	if s.IsScratch() {
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
		ShellSetting: ShellSetting{
			Shell:   defaultShell,
			Variant: shell.VariantFromShellCmd(defaultShell),
			Source:  ShellSourceDefault,
			Line:    -1,
		},
		IsLastStage: isLast,
	}
}
