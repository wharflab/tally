package rules

import "github.com/tinovyatkin/tally/internal/shell"

// HeredocResolverID is the unique identifier for the heredoc fix resolver.
const HeredocResolverID = "prefer-run-heredoc"

// HeredocRuleCode is the full rule code for the prefer-run-heredoc rule.
// Used by other rules (like DL3003) to check if heredoc conversion is enabled.
const HeredocRuleCode = TallyRulePrefix + "prefer-run-heredoc"

// HeredocDefaultMinCommands is the default minimum number of commands
// required to suggest heredoc conversion. Heredocs add 2 lines overhead
// (<<EOF and EOF), so converting 2 commands saves no lines.
const HeredocDefaultMinCommands = 3

// HeredocResolveData contains the data needed to resolve a heredoc fix.
// This is stored in SuggestedFix.ResolverData.
type HeredocResolveData struct {
	// Type indicates whether this is for consecutive RUNs or chained commands.
	Type HeredocFixType

	// StageIndex is the 0-based index of the stage containing the RUN(s).
	StageIndex int

	// Fingerprint identifies the target RUN instruction(s).
	// For consecutive RUNs: the first command of the first RUN.
	// For chained commands: the first command in the chain.
	Fingerprint string

	// ShellVariant is the shell variant for parsing.
	ShellVariant shell.Variant

	// OriginalCommands is the list of commands that should be combined.
	// Used as a secondary fingerprint for matching.
	OriginalCommands []string

	// MinCommands is the minimum number of commands to trigger heredoc conversion.
	MinCommands int
}

// HeredocFixType indicates the type of heredoc fix.
type HeredocFixType int

const (
	// HeredocFixConsecutive is for consecutive RUN instructions.
	HeredocFixConsecutive HeredocFixType = iota
	// HeredocFixChained is for a single RUN with chained commands.
	HeredocFixChained
)
