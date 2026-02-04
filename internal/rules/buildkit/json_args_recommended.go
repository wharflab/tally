package buildkit

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
)

// JSONArgsRecommendedRule implements BuildKit's JSONArgsRecommended check.
//
// BuildKit normally runs this during LLB conversion. tally reimplements it as a
// static rule based on the parsed CMD/ENTRYPOINT instructions.
type JSONArgsRecommendedRule struct{}

func NewJSONArgsRecommendedRule() *JSONArgsRecommendedRule {
	return &JSONArgsRecommendedRule{}
}

func (r *JSONArgsRecommendedRule) Metadata() rules.RuleMetadata {
	// Keep metadata aligned with internal BuildKit registry for docs.
	const name = "JSONArgsRecommended"
	return *GetMetadata(name)
}

func (r *JSONArgsRecommendedRule) Check(input rules.LintInput) []rules.Violation {
	var out []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.CmdCommand:
				if c.PrependShell {
					out = append(out, newJSONArgsRecommendedViolation(input.File, "CMD", c.Location(), r.Metadata())...)
				}
			case *instructions.EntrypointCommand:
				if c.PrependShell {
					out = append(out, newJSONArgsRecommendedViolation(input.File, "ENTRYPOINT", c.Location(), r.Metadata())...)
				}
			}
		}
	}

	return out
}

func newJSONArgsRecommendedViolation(file, instructionName string, locRanges []parser.Range, meta rules.RuleMetadata) []rules.Violation {
	loc := rules.NewLocationFromRanges(file, locRanges)
	msg := linter.RuleJSONArgsRecommended.Format(strings.ToUpper(instructionName))
	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL)
	return []rules.Violation{v}
}

func init() {
	rules.Register(NewJSONArgsRecommendedRule())
}
