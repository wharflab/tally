package rules

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
)

// BuildEnvValueReplacementEdit constructs a TextEdit to replace the value of a specific key
// in an ENV instruction. The entire instruction is reconstructed with the updated key-value pair;
// other keys in a multi-key ENV are preserved. Returns nil if the instruction has no location
// or the key is not found.
func BuildEnvValueReplacementEdit(file string, env *instructions.EnvCommand, key, newValue string) *TextEdit {
	if env == nil {
		return nil
	}

	envLoc := env.Location()
	if len(envLoc) == 0 {
		return nil
	}

	found := false
	parts := make([]string, 0, len(env.Env))
	for _, kv := range env.Env {
		if kv.Key == key {
			found = true
			parts = append(parts, key+"="+newValue)
		} else {
			parts = append(parts, kv.String())
		}
	}

	if !found {
		return nil
	}

	startLine := envLoc[0].Start.Line
	startCol := envLoc[0].Start.Character
	endLine := envLoc[len(envLoc)-1].End.Line

	return &TextEdit{
		Location: NewRangeLocation(file, startLine, startCol, endLine+1, 0),
		NewText:  "ENV " + strings.Join(parts, " ") + "\n",
	}
}

// BuildEnvKeyRemovalEdit constructs a TextEdit to remove specific keys from one ENV instruction.
// When all keys are removed, the entire instruction line is deleted. When some keys remain,
// the instruction is reconstructed without the removed keys.
func BuildEnvKeyRemovalEdit(file string, env *instructions.EnvCommand, keysToRemove []string) *TextEdit {
	if env == nil {
		return nil
	}

	envLoc := env.Location()
	if len(envLoc) == 0 {
		return nil
	}

	removeSet := make(map[string]bool, len(keysToRemove))
	for _, k := range keysToRemove {
		removeSet[k] = true
	}

	startLine := envLoc[0].Start.Line
	startCol := envLoc[0].Start.Character

	// Count remaining variables after removal.
	parts := make([]string, 0, len(env.Env))
	for _, kv := range env.Env {
		if removeSet[kv.Key] {
			continue
		}
		parts = append(parts, kv.String())
	}

	if len(parts) == 0 {
		// Remove the entire instruction including its trailing newline.
		endLine := envLoc[len(envLoc)-1].End.Line
		return &TextEdit{
			Location: NewRangeLocation(file, startLine, startCol, endLine+1, 0),
			NewText:  "",
		}
	}

	// Multi-variable ENV: replace the entire instruction line(s) with the
	// reconstructed key list plus a trailing newline. We use endLine+1, col 0
	// (same strategy as the full-deletion path) because BuildKit's End.Character
	// is often 0 for single-line instructions, which would create a zero-width range.
	endLine := envLoc[len(envLoc)-1].End.Line

	return &TextEdit{
		Location: NewRangeLocation(file, startLine, startCol, endLine+1, 0),
		NewText:  "ENV " + strings.Join(parts, " ") + "\n",
	}
}
