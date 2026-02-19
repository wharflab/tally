package rules

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
)

// EpilogueOrderResolverID is the unique identifier for the epilogue-order fix resolver.
const EpilogueOrderResolverID = "epilogue-order"

// EpilogueOrderResolveData carries resolver context.
// The resolver is self-contained (re-parses and re-analyzes the file),
// so no additional data is needed.
type EpilogueOrderResolveData struct{}

// EpilogueOrderRank maps lowercase epilogue instruction names to their canonical position.
// This is the single source of truth for epilogue ordering, shared by the rule and resolver.
var EpilogueOrderRank = map[string]int{
	"stopsignal":  0,
	"healthcheck": 1,
	"entrypoint":  2,
	"cmd":         3,
}

// IsEpilogueInstruction reports whether the lowercase instruction name is an epilogue instruction.
func IsEpilogueInstruction(name string) bool {
	_, ok := EpilogueOrderRank[name]
	return ok
}

// HasDuplicateEpilogueNames reports whether any name in the slice appears more than once.
func HasDuplicateEpilogueNames(names []string) bool {
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return true
		}
		seen[n] = true
	}
	return false
}

// CheckEpilogueOrder reports whether epilogue instructions in the given commands are
// correctly positioned at the end and in canonical order. ONBUILD commands are ignored.
func CheckEpilogueOrder(commands []instructions.Command) bool {
	foundEpilogue := false
	prevRank := -1

	for _, cmd := range commands {
		if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
			continue
		}

		name := strings.ToLower(cmd.Name())
		rank, isEpilogue := EpilogueOrderRank[name]

		if isEpilogue {
			foundEpilogue = true
			if rank < prevRank {
				return false
			}
			prevRank = rank
		} else if foundEpilogue {
			return false
		}
	}

	return true
}
