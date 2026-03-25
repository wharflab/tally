package gpu

import (
	"strings"

	"github.com/wharflab/tally/internal/semantic"
)

// stageUsesNVIDIACUDABase returns true if the stage's base image is nvidia/cuda:*.
func stageUsesNVIDIACUDABase(info *semantic.StageInfo) bool {
	name := stageBaseImageName(info)
	return name == "nvidia/cuda" || name == "docker.io/nvidia/cuda"
}

// stageUsesNVIDIABase returns true if the stage's base image is from NVIDIA
// (nvidia/cuda, nvcr.io/nvidia/*, nvidia/cudagl).
func stageUsesNVIDIABase(info *semantic.StageInfo) bool {
	name := stageBaseImageName(info)
	if name == "nvidia/cuda" || name == "docker.io/nvidia/cuda" ||
		name == "nvidia/cudagl" || name == "docker.io/nvidia/cudagl" {
		return true
	}
	return strings.HasPrefix(name, "nvcr.io/nvidia/")
}

// stageBaseImageName returns the lowercased base image name without tag for matching.
// Returns empty string if the stage has no base image info.
func stageBaseImageName(info *semantic.StageInfo) string {
	if info == nil || info.BaseImage == nil || info.BaseImage.IsStageRef {
		return ""
	}
	raw := strings.ToLower(info.BaseImage.Raw)
	// Strip tag/digest
	if i := strings.IndexAny(raw, ":@"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}
