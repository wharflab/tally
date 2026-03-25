package gpu

import (
	"strings"

	"github.com/distribution/reference"

	"github.com/wharflab/tally/internal/semantic"
)

const (
	nvidiaCUDAImage   = "nvidia/cuda"
	nvidiaCUDAGLImage = "nvidia/cudagl"
)

// stageUsesNVIDIACUDABase returns true if the stage's base image is nvidia/cuda:*.
func stageUsesNVIDIACUDABase(info *semantic.StageInfo) bool {
	name := stageBaseImageName(info)
	return name == nvidiaCUDAImage || name == "docker.io/"+nvidiaCUDAImage
}

// stageUsesNVIDIABase returns true if the stage's base image is from NVIDIA
// (nvidia/cuda, nvcr.io/nvidia/*, nvidia/cudagl).
func stageUsesNVIDIABase(info *semantic.StageInfo) bool {
	name := stageBaseImageName(info)
	if name == nvidiaCUDAImage || name == "docker.io/"+nvidiaCUDAImage ||
		name == nvidiaCUDAGLImage || name == "docker.io/"+nvidiaCUDAGLImage {
		return true
	}
	return strings.HasPrefix(name, "nvcr.io/nvidia/")
}

// cudaFlavor represents the variant of an nvidia/cuda image.
type cudaFlavor int

const (
	// cudaFlavorBase contains only the CUDA runtime library (cudart).
	// Also the zero value, used when the flavor cannot be determined.
	cudaFlavorBase cudaFlavor = iota
	// cudaFlavorRuntime adds math libraries and NCCL on top of base.
	cudaFlavorRuntime
	// cudaFlavorDevel adds nvcc, headers, and development libraries on top of runtime.
	cudaFlavorDevel
)

// cudaImageInfo holds the parsed flavor and optional component tags from an nvidia/cuda image tag.
type cudaImageInfo struct {
	Flavor      cudaFlavor
	HasCuDNN    bool
	IsCUDAImage bool
}

// parseCUDAImageInfo extracts flavor and component info from an nvidia/cuda stage's base image
// using the distribution/reference library for proper image reference parsing.
func parseCUDAImageInfo(info *semantic.StageInfo) cudaImageInfo {
	if info == nil || info.BaseImage == nil || info.BaseImage.IsStageRef {
		return cudaImageInfo{}
	}

	// Dockerfile image references may use uppercase; normalize before parsing
	// since OCI references must be lowercase.
	named, err := reference.ParseNormalizedNamed(strings.ToLower(info.BaseImage.Raw))
	if err != nil {
		return cudaImageInfo{}
	}

	familiarName := reference.FamiliarName(named)
	if familiarName != nvidiaCUDAImage {
		return cudaImageInfo{}
	}

	tag := ""
	if tagged, ok := named.(reference.Tagged); ok {
		tag = strings.ToLower(tagged.Tag())
	}

	result := cudaImageInfo{IsCUDAImage: true}
	result.HasCuDNN = strings.Contains(tag, "cudnn")

	// NVIDIA CUDA tags follow the pattern: <version>-<flavor>-<os>
	// e.g. 12.2.0-devel-ubuntu22.04, 12.2.0-cudnn-runtime-ubuntu22.04
	switch {
	case strings.Contains(tag, "-devel"):
		result.Flavor = cudaFlavorDevel
	case strings.Contains(tag, "-runtime"):
		result.Flavor = cudaFlavorRuntime
	case strings.Contains(tag, "-base"):
		result.Flavor = cudaFlavorBase
	default:
		// Unrecognized tag structure (e.g. just a version, digest, or ARG-based).
		// Default to devel to avoid false positives.
		result.Flavor = cudaFlavorDevel
	}

	return result
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
