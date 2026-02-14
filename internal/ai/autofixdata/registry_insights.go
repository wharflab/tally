package autofixdata

// RegistryInsight summarizes registry-resolved metadata for an external base
// image used in a Dockerfile stage.
//
// This information is produced by slow checks (registry-backed async plans) and
// can be embedded into AI prompts to reduce ambiguity about platforms and image
// selection.
type RegistryInsight struct {
	StageIndex int

	// Ref is the normalized base image reference used for registry resolution.
	Ref string

	// RequestedPlatform is the platform string requested for resolution
	// (e.g., "linux/amd64").
	RequestedPlatform string

	// ResolvedPlatform is the resolved platform for the selected manifest, if known.
	ResolvedPlatform string

	// Digest is the resolved manifest digest, if known.
	Digest string

	// AvailablePlatforms is populated when the requested platform did not match
	// any manifest and the resolver returned a list of available platforms.
	AvailablePlatforms []string
}
