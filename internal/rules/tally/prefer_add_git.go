package tally

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

var (
	dockerfileAddKeyword = strings.ToUpper(command.Add)
	dockerfileRunKeyword = strings.ToUpper(command.Run)
)

// PreferAddGitRule implements the prefer-add-git linting rule.
type PreferAddGitRule struct{}

// NewPreferAddGitRule creates a new prefer-add-git rule instance.
func NewPreferAddGitRule() *PreferAddGitRule {
	return &PreferAddGitRule{}
}

// Metadata returns the rule metadata.
func (r *PreferAddGitRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.PreferAddGitRuleCode,
		Name:            "Prefer ADD git sources over git clone in RUN",
		Description:     "Use `ADD <git source>` instead of cloning repositories in `RUN` for more hermetic builds",
		DocURL:          rules.TallyDocURL(rules.PreferAddGitRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		IsExperimental:  false,
		FixPriority:     8,
	}
}

// Check runs the prefer-add-git rule.
func (r *PreferAddGitRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	runContexts := buildGitRunContexts(input)
	if len(runContexts) == 0 {
		return nil
	}

	var violations []rules.Violation
	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok || !run.PrependShell {
				continue
			}

			ctx, ok := runContexts[run]
			if !ok || ctx.Script == "" {
				continue
			}

			if !shell.HasGitCloneRemote(ctx.Script, ctx.Variant) {
				continue
			}

			loc := rules.NewLocationFromRanges(input.File, run.Location())
			v := rules.NewViolation(
				loc,
				meta.Code,
				"prefer ADD <git source> over git clone in RUN for more hermetic, supply-chain-friendly builds",
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL).WithDetail(
				"BuildKit git sources make repository fetches explicit in the Dockerfile graph and avoid mutable network acquisition inside RUN. " +
					"When the clone flow is simple enough, tally can extract it into ADD while preserving the surrounding commands.",
			)

			if opportunity, ok := shell.FirstGitSourceOpportunity(ctx.Script, ctx.Variant, ctx.Workdir); ok {
				if fix := buildPreferAddGitFix(input.File, run, sm, meta, opportunity); fix != nil {
					v = v.WithSuggestedFix(fix)
				}
			}

			violations = append(violations, v)
		}
	}

	return violations
}

type gitRunContext struct {
	Script  string
	Workdir string
	Variant shell.Variant
}

func buildGitRunContexts(input rules.LintInput) map[*instructions.RunCommand]gitRunContext {
	contexts := make(map[*instructions.RunCommand]gitRunContext)
	if input.Facts == nil {
		return contexts
	}

	for stageIdx := range input.Stages {
		stageFacts := input.Facts.Stage(stageIdx)
		if stageFacts == nil {
			continue
		}
		for _, runFacts := range stageFacts.Runs {
			if runFacts == nil || runFacts.Run == nil {
				continue
			}
			contexts[runFacts.Run] = gitRunContext{
				Script:  runFacts.SourceScript,
				Workdir: runFacts.Workdir,
				Variant: runFacts.Shell.Variant,
			}
		}
	}

	return contexts
}

func buildPreferAddGitFix(
	file string,
	run *instructions.RunCommand,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	opportunity *shell.GitSourceOpportunity,
) *rules.SuggestedFix {
	if run == nil || opportunity == nil || opportunity.AddSource == "" || opportunity.AddDestination == "" {
		return nil
	}
	if strings.ContainsAny(opportunity.AddSource, " \t\r\n") || strings.ContainsAny(opportunity.AddDestination, " \t\r\n") {
		return nil
	}
	if hasUnsupportedGitRunFlags(run) || len(runmount.GetMounts(run)) > 0 {
		return nil
	}

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	flagSuffix := extractRunFlagSuffix(run, sm)
	indent := replacementIndent(runLoc, sm)
	replacementParts := make([]string, 0, 3)
	if opportunity.PrecedingCommands != "" {
		replacementParts = append(replacementParts, buildRunWithSuffix(flagSuffix, opportunity.PrecedingCommands))
	}
	replacementParts = append(replacementParts, buildAddGitInstruction(opportunity))
	if opportunity.RemainingCommands != "" {
		replacementParts = append(replacementParts, buildRunWithSuffix(flagSuffix, opportunity.RemainingCommands))
	}

	endLine, endCol := resolveRunEndPosition(runLoc, sm, run)
	if sm != nil && endLine > 0 && endLine <= sm.LineCount() {
		endCol = len(sm.Line(endLine - 1))
	}
	return &rules.SuggestedFix{
		Description: "Extract git clone into ADD <git source>",
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(
				file,
				runLoc[0].Start.Line,
				0,
				endLine,
				endCol,
			),
			NewText: joinReplacementLines(replacementParts, indent),
		}},
	}
}

func replacementIndent(runLoc []parser.Range, sm *sourcemap.SourceMap) string {
	if sm == nil || len(runLoc) == 0 || runLoc[0].Start.Line <= 0 {
		return ""
	}
	return leadingWhitespace(sm.Line(runLoc[0].Start.Line - 1))
}

func joinReplacementLines(parts []string, indent string) string {
	if len(parts) == 0 {
		return ""
	}
	if indent == "" {
		return strings.Join(parts, "\n")
	}

	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(indent)
		b.WriteString(part)
	}
	return b.String()
}

func buildAddGitInstruction(opportunity *shell.GitSourceOpportunity) string {
	parts := []string{dockerfileAddKeyword, "--link"}
	if opportunity == nil {
		return strings.Join(parts, " ")
	}
	if opportunity.KeepGitDir {
		parts = append(parts, "--keep-git-dir=true")
	}
	if opportunity.AddChecksum != "" {
		parts = append(parts, "--checksum="+opportunity.AddChecksum)
	}
	parts = append(parts, opportunity.AddSource, opportunity.AddDestination)
	return strings.Join(parts, " ")
}

func hasUnsupportedGitRunFlags(run *instructions.RunCommand) bool {
	if run == nil {
		return false
	}
	for _, flag := range run.FlagsUsed {
		name, _, _ := strings.Cut(flag, "=")
		name = strings.TrimLeft(name, "-")
		if name != "mount" {
			return true
		}
	}
	return false
}

func extractRunFlagSuffix(run *instructions.RunCommand, sm *sourcemap.SourceMap) string {
	if run == nil || sm == nil {
		return " "
	}
	resolved, ok := dockerfile.ResolveRunSource(run, sm)
	if !ok || resolved.ScriptIndex <= 0 || resolved.ScriptIndex > len(resolved.Source) {
		return " "
	}

	prefix := resolved.Source[:resolved.ScriptIndex]
	upper := strings.ToUpper(prefix)
	runIdx := strings.LastIndex(upper, dockerfileRunKeyword)
	if runIdx < 0 {
		return " "
	}

	suffix := prefix[runIdx+len(dockerfileRunKeyword):]
	if suffix == "" {
		return " "
	}
	return suffix
}

func buildRunWithSuffix(flagSuffix, script string) string {
	if flagSuffix == "" {
		flagSuffix = " "
	}
	return dockerfileRunKeyword + flagSuffix + script
}

func init() {
	rules.Register(NewPreferAddGitRule())
}
