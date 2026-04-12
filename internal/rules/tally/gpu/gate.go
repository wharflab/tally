package gpu

import (
	"fmt"
	"regexp"
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
	// This is the zero value; for unrecognized nvidia/cuda tags, parseCUDAImageInfo
	// defaults to cudaFlavorDevel instead to avoid false positives.
	cudaFlavorBase cudaFlavor = iota
	// cudaFlavorRuntime adds math libraries and NCCL on top of base.
	cudaFlavorRuntime
	// cudaFlavorDevel adds nvcc, headers, and development libraries on top of runtime.
	cudaFlavorDevel
)

// cudaImageInfo holds the parsed flavor, version, and optional component tags from an nvidia/cuda image tag.
type cudaImageInfo struct {
	Flavor      cudaFlavor
	HasCuDNN    bool
	IsCUDAImage bool
	CUDAMajor   int // CUDA major version (e.g., 12 for "12.2.0-devel-ubuntu22.04"); 0 if unparseable
	CUDAMinor   int // CUDA minor version (e.g., 2 for "12.2.0-devel-ubuntu22.04")
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

	// Extract CUDA major.minor version from the tag.
	if m := cudaTagVersionRe.FindStringSubmatch(tag); m != nil {
		result.CUDAMajor = atoiDigits(m[1])
		result.CUDAMinor = atoiDigits(m[2])
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

// cudaTagVersionRe matches the CUDA major.minor version at the start of an
// nvidia/cuda image tag (e.g., "12.2.0-devel-ubuntu22.04" → "12", "2").
var cudaTagVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)`)

// atoiDigits converts a digit-only string to int. Returns 0 on error (should
// not happen with regex-validated input).
func atoiDigits(s string) int {
	n := 0
	for _, ch := range s {
		n = n*10 + int(ch-'0')
	}
	return n
}

// knownCUDASuffix represents a known published PyTorch CUDA wheel suffix.
type knownCUDASuffix struct {
	Major int
	Minor int
}

// knownCUDASuffixes lists the CUDA versions for which PyTorch publishes
// prebuilt wheels. Past releases are immutable; add new entries when PyTorch
// ships a new cuXYZ variant.
var knownCUDASuffixes = []knownCUDASuffix{
	{11, 6}, {11, 7}, {11, 8},
	{12, 1}, {12, 4}, {12, 6}, {12, 8},
}

// bestCUDASuffix returns the highest known PyTorch cuXYZ suffix where major
// matches and minor <= the given minor. Returns ("", false) if no published
// suffix exists for that major version at or below the given minor.
func bestCUDASuffix(major, minor int) (string, bool) {
	best := -1
	for _, s := range knownCUDASuffixes {
		if s.Major == major && s.Minor <= minor && s.Minor > best {
			best = s.Minor
		}
	}
	if best < 0 {
		return "", false
	}
	return cudaSuffixString(major, best), true
}

// cudaSuffixString formats a CUDA major.minor version as a cuXYZ suffix
// (e.g., 12, 4 → "cu124"; 11, 8 → "cu118").
func cudaSuffixString(major, minor int) string {
	return fmt.Sprintf("cu%d%d", major, minor)
}

// cudaVersionString formats a CUDA major.minor as a dotted version string
// (e.g., 12, 4 → "12.4").
func cudaVersionString(major, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
}
