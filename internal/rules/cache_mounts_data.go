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

	// RunSignature is a whitespace-normalized representation of the original
	// RUN script. The resolver uses it as a stable fingerprint when earlier
	// sync fixes delete or reorder other RUN instructions in the same stage.
	RunSignature string

	// CommandNames is the exact sequence of parsed command names observed in
	// the original RUN script. The resolver uses it to avoid matching a
	// different package-manager RUN that happens to require the same cache
	// mount targets.
	CommandNames []string

	// RequiredTargets is the exact set of cache mount targets detected for the
	// original RUN. The resolver re-runs detection on post-sync content and
	// only emits edits when the matched RUN still requires the same targets.
	RequiredTargets []string

	// CacheEnvSelections identifies the specific cache-disabling ENV bindings
	// consumed during sync analysis for this RUN. Async resolution re-selects
	// only these bindings so repeated ENV keys are removed at most once across
	// multiple RUN fixes in the same stage.
	CacheEnvSelections []CacheMountsEnvSelection
}

// CacheMountsEnvSelection identifies a cache-disabling ENV binding by its
// position in the sorted active binding list for a RUN plus the key name it
// must still match after post-sync reparsing.
type CacheMountsEnvSelection struct {
	BindingIndex int
	Key          string
}
