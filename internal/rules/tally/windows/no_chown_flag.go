package windows

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NoChownFlagRuleCode is the full rule code for tally/windows/no-chown-flag.
const NoChownFlagRuleCode = rules.TallyRulePrefix + "windows/no-chown-flag"

// NoChownFlagRule flags --chown usage on COPY/ADD in Windows stages.
// The --chown flag is silently ignored on Windows containers because Windows
// does not use POSIX file ownership (uid:gid). Users who add --chown expect
// ownership to be set, but the flag has no effect.
type NoChownFlagRule struct{}

// NewNoChownFlagRule creates a new rule instance.
func NewNoChownFlagRule() *NoChownFlagRule { return &NoChownFlagRule{} }

// Metadata returns the rule metadata.
func (r *NoChownFlagRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoChownFlagRuleCode,
		Name:            "No --chown flag on Windows",
		Description:     "COPY/ADD --chown is silently ignored on Windows containers",
		DocURL:          rules.TallyDocURL(NoChownFlagRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the rule against the given input.
func (r *NoChownFlagRule) Check(input rules.LintInput) []rules.Violation {
	stages := windowsStages(input)
	if len(stages) == 0 {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()
	var violations []rules.Violation

	for _, info := range stages {
		if info.Stage == nil {
			continue
		}
		for _, cmd := range info.Stage.Commands {
			var chown, keyword string
			var loc []parser.Range

			switch c := cmd.(type) {
			case *instructions.CopyCommand:
				chown = c.Chown
				keyword = command.Copy
				loc = c.Location()
			case *instructions.AddCommand:
				chown = c.Chown
				keyword = command.Add
				loc = c.Location()
			default:
				continue
			}

			if chown == "" {
				continue
			}

			instrLoc := rules.NewLocationFromRanges(input.File, loc)
			if instrLoc.IsFileLevel() {
				continue
			}

			upperKeyword := strings.ToUpper(keyword)
			msg := fmt.Sprintf(
				"%s --chown=%s is silently ignored on Windows containers",
				upperKeyword, chown,
			)
			detail := fmt.Sprintf(
				"Windows containers do not use POSIX file ownership (uid:gid). "+
					"The --chown flag on %s has no effect and can be safely removed.",
				upperKeyword,
			)

			v := rules.NewViolation(instrLoc, meta.Code, msg, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail(detail)
			v.StageIndex = info.Index

			if fix := buildChownRemoveFix(input.File, sm, loc, chown); fix != nil {
				v = v.WithSuggestedFix(fix)
			}

			violations = append(violations, v)
		}
	}

	return violations
}

// buildChownRemoveFix creates a fix that removes the --chown=value flag from
// the source line. The fix includes the flag and one trailing space.
func buildChownRemoveFix(
	file string,
	sm *sourcemap.SourceMap,
	loc []parser.Range,
	chown string,
) *rules.SuggestedFix {
	if len(loc) == 0 || sm == nil {
		return nil
	}

	line := loc[0].Start.Line
	lineText := sm.Line(line - 1) // 0-based

	start, end, found := findChownFlagRange(lineText)
	if !found {
		return nil
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Remove --chown=%s (ignored on Windows)", chown),
		Safety:      rules.FixSafe,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, line, start, line, end),
			NewText:  "",
		}},
	}
}

// findChownFlagRange locates the --chown=<value> flag in a source line and
// returns the start and end column positions (0-based, end exclusive) covering
// the flag and one trailing whitespace character. The chown parameter is the
// parsed value from BuildKit (unquoted).
func findChownFlagRange(lineText string) (start, end int, found bool) {
	lower := strings.ToLower(lineText)
	idx := strings.Index(lower, "--chown=")
	if idx < 0 {
		return 0, 0, false
	}

	start = idx
	pos := idx + len("--chown=")

	// Handle quoted values: --chown="user:group" or --chown='user:group'
	if pos < len(lineText) && (lineText[pos] == '"' || lineText[pos] == '\'') {
		quote := lineText[pos]
		pos++ // skip opening quote
		for pos < len(lineText) {
			if lineText[pos] == quote {
				break
			}
			if lineText[pos] == '\\' {
				pos++ // skip escaped character
			}
			pos++
		}
		if pos < len(lineText) {
			pos++ // skip closing quote
		}
	} else {
		// Unquoted value: skip until whitespace
		for pos < len(lineText) && lineText[pos] != ' ' && lineText[pos] != '\t' {
			pos++
		}
	}

	// Include one trailing whitespace character in the removal range
	// so the result doesn't have a double space.
	if pos < len(lineText) && (lineText[pos] == ' ' || lineText[pos] == '\t') {
		pos++
	}

	return start, pos, true
}

func init() {
	rules.Register(NewNoChownFlagRule())
}
