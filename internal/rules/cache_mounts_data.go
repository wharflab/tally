package rules

// CacheMountsResolverID is the unique identifier for the cache-mounts
// post-sync fix resolver. The resolver runs after sync fixes (e.g.,
// shellcheck/SC2086 quoting, curl-should-follow-redirects flag insertion)
// and produces a whole-RUN tail rewrite that preserves those narrow edits.
//
// Only emitted when prefer-package-cache-mounts needs a tail rewrite
// (mount flags mutated, or script cleanup needed but targeted edits
// unavailable). Targeted edits still go out as plain sync edits.
const CacheMountsResolverID = "prefer-package-cache-mounts"

// CacheMountsResolveData carries the information the cache-mounts resolver
// needs to re-find the target RUN instruction in post-sync content and
// emit a fresh tail rewrite. The resolver re-runs the mount detection on
// the updated script so narrow sync fixes on that RUN are preserved.
type CacheMountsResolveData struct {
	// StageIndex is the 0-based index of the stage containing the RUN.
	StageIndex int

	// RunOrdinal is the 1-based ordinal of the target RUN instruction
	// within its stage. Stable across earlier sync fixes that insert
	// non-RUN instructions (e.g., SHELL) before it.
	RunOrdinal int
}
