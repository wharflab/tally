package rules

import "github.com/tinovyatkin/tally/internal/shell"

// HeredocResolverID is the unique identifier for the heredoc fix resolver.
const HeredocResolverID = "prefer-run-heredoc"

// HeredocRuleCode is the full rule code for the prefer-run-heredoc rule.
// Used by other rules (like DL3003) to check if heredoc conversion is enabled.
const HeredocRuleCode = TallyRulePrefix + "prefer-run-heredoc"

// PipefailRuleCode is the full rule code for the DL4006 pipefail rule.
// Used by the heredoc formatter to determine whether to add set -o pipefail.
const PipefailRuleCode = HadolintRulePrefix + "DL4006"

// HeredocDefaultMinCommands is the default minimum number of commands
// required to suggest heredoc conversion. Heredocs add 2 lines overhead
// (<<EOF and EOF), so converting 2 commands saves no lines.
const HeredocDefaultMinCommands = 3

// HeredocResolveData contains the data needed to resolve a heredoc fix.
// This is stored in SuggestedFix.ResolverData.
//
// The resolver uses re-parsing to find fixes rather than fingerprint matching.
// This approach is more robust because content may have changed due to sync fixes
// (apt → apt-get, cd → WORKDIR, etc.) applied before the heredoc resolver runs.
type HeredocResolveData struct {
	// Type indicates whether this is for consecutive RUNs or chained commands.
	Type HeredocFixType

	// StageIndex is the 0-based index of the stage containing the RUN(s).
	StageIndex int

	// ShellVariant is the shell variant for parsing.
	ShellVariant shell.Variant

	// MinCommands is the minimum number of commands to trigger heredoc conversion.
	MinCommands int

	// PipefailEnabled indicates whether DL4006 (pipefail) is enabled.
	// When true, the heredoc formatter will add "set -o pipefail" inside the
	// heredoc body if any command contains a pipe, avoiding the need for a
	// separate SHELL instruction.
	PipefailEnabled bool
}

// HeredocFixType indicates the type of heredoc fix.
type HeredocFixType int

const (
	// HeredocFixConsecutive is for consecutive RUN instructions.
	HeredocFixConsecutive HeredocFixType = iota
	// HeredocFixChained is for a single RUN with chained commands.
	HeredocFixChained
)
