package rules

// DL4001CleanupResolverID is the unique identifier for the DL4001 post-sync
// cleanup resolver. The resolver runs after sync fixes (including the per-RUN
// curl↔wget invocation rewrites emitted by DL4001 and any sort-packages edits)
// and removes the non-preferred tool from install commands plus any config
// artifacts (.curlrc/.wgetrc heredoc copies, tool-specific ENV bindings, and
// tally-authored annotation comments) that are now dead weight.
const DL4001CleanupResolverID = "hadolint-dl4001-cleanup"

// DL4001CleanupResolveData is the payload for the DL4001 cleanup resolver.
// It identifies which tool to evict; the resolver re-parses the post-sync
// Dockerfile and computes the full edit set from there.
type DL4001CleanupResolveData struct {
	// SourceTool is the tool to remove ("curl" or "wget").
	SourceTool string
}
