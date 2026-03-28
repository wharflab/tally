package tally

import (
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

// finalStageRootCheck holds the result of checking whether the final stage
// runs as root. Multiple security rules share this preamble.
type finalStageRootCheck struct {
	FinalIdx     int
	FileFacts    *facts.FileFacts
	StageFacts   *facts.StageFacts
	ImplicitRoot bool // true when no USER instruction exists (inherits root from base)
}

// checkFinalStageRoot performs the common preamble for rules that need to know
// whether the final stage's effective user is root. Returns nil if the final
// stage is non-root or cannot be determined.
func checkFinalStageRoot(input rules.LintInput) *finalStageRootCheck {
	if len(input.Stages) == 0 {
		return nil
	}

	finalIdx := len(input.Stages) - 1

	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion
	if fileFacts == nil {
		return nil
	}

	sf := fileFacts.Stage(finalIdx)
	if sf == nil {
		return nil
	}

	implicitRoot := false
	switch {
	case sf.EffectiveUser != "":
		if !facts.IsRootUser(sf.EffectiveUser) {
			return nil
		}
	default:
		if isKnownNonRootBase(input.Semantic, fileFacts, finalIdx) {
			return nil
		}
		implicitRoot = true
	}

	return &finalStageRootCheck{
		FinalIdx:     finalIdx,
		FileFacts:    fileFacts,
		StageFacts:   sf,
		ImplicitRoot: implicitRoot,
	}
}
