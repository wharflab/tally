package version

import (
	"runtime"
	"runtime/debug"
)

var version = "dev"

// Version returns the current version string with BuildKit suffix.
func Version() string {
	bkVersion := BuildKitVersion()
	if bkVersion != "" {
		return version + " (buildkit " + bkVersion + ")"
	}
	return version
}

// RawVersion returns the semantic version string without any suffix.
func RawVersion() string {
	return version
}

// BuildKitVersion returns the linked BuildKit version from build info.
func BuildKitVersion() string {
	bk, _ := readBuildInfo()
	return bk
}

// GoVersion returns the Go toolchain version used for the build.
func GoVersion() string {
	return runtime.Version()
}

// GitCommit returns the VCS revision embedded at build time, if available.
func GitCommit() string {
	_, commit := readBuildInfo()
	return commit
}

// readBuildInfo reads debug.ReadBuildInfo once and extracts both
// the BuildKit dependency version and the VCS revision.
func readBuildInfo() (string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	var bkVersion, commit string
	for _, dep := range info.Deps {
		if dep.Path == "github.com/moby/buildkit" {
			bkVersion = dep.Version
			break
		}
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				commit = s.Value[:12]
			} else {
				commit = s.Value
			}
			break
		}
	}
	return bkVersion, commit
}

// Info holds structured version information for machine-readable output.
type Info struct {
	Version         string   `json:"version"`
	BuildkitVersion string   `json:"buildkitVersion,omitempty"`
	Platform        Platform `json:"platform"`
	GoVersion       string   `json:"goVersion"`
	GitCommit       string   `json:"gitCommit,omitempty"`
}

// Platform describes the OS and architecture.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// GetInfo returns structured version information.
func GetInfo() Info {
	bkVersion, commit := readBuildInfo()
	return Info{
		Version:         RawVersion(),
		BuildkitVersion: bkVersion,
		Platform: Platform{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
		},
		GoVersion: GoVersion(),
		GitCommit: commit,
	}
}
