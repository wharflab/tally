package tally

import (
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// SortPackagesRuleCode is the full rule code for the sort-packages rule.
const SortPackagesRuleCode = rules.TallyRulePrefix + "sort-packages"

// SortPackagesRule enforces alphabetical sorting of packages in install commands.
type SortPackagesRule struct{}

// NewSortPackagesRule creates a new sort-packages rule instance.
func NewSortPackagesRule() *SortPackagesRule {
	return &SortPackagesRule{}
}

// Metadata returns the rule metadata.
func (r *SortPackagesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            SortPackagesRuleCode,
		Name:            "Sort Packages",
		Description:     "Package lists in install commands should be sorted alphabetically",
		DocURL:          rules.TallyDocURL(SortPackagesRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     15,
	}
}

// Check runs the sort-packages rule.
func (r *SortPackagesRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Type assertion OK returns false for nil

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		variant := resolveShellVariant(sem, stageIdx)
		if !variant.IsParseable() {
			continue
		}
		for _, cmd := range stage.Commands {
			if run, ok := cmd.(*instructions.RunCommand); ok {
				violations = append(violations, r.checkRun(run, variant, input.File, sm, meta)...)
			}
		}
	}
	return violations
}

// resolveShellVariant returns the shell variant for a stage, defaulting to Bash.
func resolveShellVariant(sem *semantic.Model, stageIdx int) shell.Variant {
	if sem == nil {
		return shell.VariantBash
	}
	if info := sem.StageInfo(stageIdx); info != nil {
		return info.ShellSetting.Variant
	}
	return shell.VariantBash
}

// checkRun checks a single RUN instruction for unsorted package lists.
func (r *SortPackagesRule) checkRun(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	if len(run.Location()) == 0 {
		return nil
	}

	// Skip exec-form RUN: RUN ["apt-get", "install", "curl"]
	if !run.PrependShell {
		return nil
	}

	// Skip heredoc-form RUN
	if len(run.Files) > 0 {
		return nil
	}

	script := getRunScriptFromCmd(run)
	if script == "" {
		return nil
	}

	startLine := run.Location()[0].Start.Line
	endLine := resolveEndLine(sm, run.Location())

	// Get instruction source lines for position mapping
	instrLines := make([]string, 0, endLine-startLine+1)
	for l := startLine; l <= endLine; l++ {
		instrLines = append(instrLines, sm.Line(l-1))
	}

	cmdStartCol := findCmdStartCol(instrLines[0])

	// Reconstruct the source text that the shell parser will see
	sourceText := shell.ReconstructSourceText(instrLines, cmdStartCol)

	// Find install commands with per-argument positions
	installCmds := shell.FindInstallPackages(sourceText, shellVariant)

	var violations []rules.Violation
	loc := rules.NewLocationFromRanges(file, run.Location())

	for _, ic := range installCmds {
		if v := r.checkInstallCommand(ic, startLine, cmdStartCol, file, loc, meta); v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// checkInstallCommand checks a single install command for unsorted packages.
func (r *SortPackagesRule) checkInstallCommand(
	ic shell.InstallCommand,
	startLine int,
	cmdStartCol int,
	file string,
	loc rules.Location,
	meta rules.RuleMetadata,
) *rules.Violation {
	// Partition into literals (skip variables — they are not sorted)
	literals := make([]shell.PackageArg, 0, len(ic.Packages))
	for _, pkg := range ic.Packages {
		if !pkg.IsVar {
			literals = append(literals, pkg)
		}
	}

	// Need at least 2 literal packages to sort
	if len(literals) < 2 {
		return nil
	}

	// Sort literals by case-insensitive sort key (using Normalized for comparison)
	sorted := make([]shell.PackageArg, len(literals))
	copy(sorted, literals)
	slices.SortStableFunc(sorted, func(a, b shell.PackageArg) int {
		return strings.Compare(sortKey(a.Normalized), sortKey(b.Normalized))
	})

	// Check if already sorted
	alreadySorted := true
	for i, lit := range literals {
		if lit.Normalized != sorted[i].Normalized {
			alreadySorted = false
			break
		}
	}
	if alreadySorted {
		return nil
	}

	// Build slot-based replacement edits
	edits := r.buildSlotEdits(literals, sorted, startLine, cmdStartCol, file)

	msg := fmt.Sprintf("packages in %s %s are not sorted alphabetically", ic.Manager, ic.Subcommand)

	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Sort packages alphabetically",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       edits,
			IsPreferred: true,
		})

	return &v
}

// buildSlotEdits generates narrow per-slot TextEdits for each position where
// the current package differs from the sorted order.
func (r *SortPackagesRule) buildSlotEdits(
	originals []shell.PackageArg,
	sorted []shell.PackageArg,
	startLine int,
	cmdStartCol int,
	file string,
) []rules.TextEdit {
	edits := make([]rules.TextEdit, 0, len(originals))

	for i, orig := range originals {
		if orig.Normalized == sorted[i].Normalized {
			continue // Already in correct position
		}

		// Map source text position to Dockerfile position
		// Shell source line 0 → Dockerfile startLine, with cmdStartCol offset
		// Shell source line N>0 → Dockerfile startLine+N, no offset
		docLine := startLine + orig.Line
		docStartCol := orig.StartCol
		docEndCol := orig.EndCol
		if orig.Line == 0 {
			docStartCol += cmdStartCol
			docEndCol += cmdStartCol
		}

		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, docLine, docStartCol, docLine, docEndCol),
			NewText:  sorted[i].Value,
		})
	}

	return edits
}

// sortKey extracts a case-insensitive sort key from a package name by stripping
// version specifiers. Examples:
//   - "flask==2.0" → "flask"
//   - "curl=7.88.1" → "curl"
//   - "@eslint/js" → "@eslint/js"
//   - "@eslint/js@8.0.0" → "@eslint/js" (npm scoped: strip version after last @)
func sortKey(pkg string) string {
	key := pkg

	// Handle npm scoped packages: @scope/name@version → @scope/name
	if strings.HasPrefix(key, "@") {
		// Find the last @ which separates the version
		lastAt := strings.LastIndex(key, "@")
		if lastAt > 0 { // Must be > 0 to skip the leading @
			key = key[:lastAt]
		}
		return strings.ToLower(key)
	}

	// Strip version specifiers: first occurrence of =, >=, <=, ~=, !=, <, >
	for i, ch := range key {
		if ch == '=' || ch == '<' || ch == '>' || ch == '~' || ch == '!' {
			key = key[:i]
			break
		}
	}

	return strings.ToLower(key)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewSortPackagesRule())
}
