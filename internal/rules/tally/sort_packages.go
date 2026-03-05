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

	escapeToken := rune('\\')
	if input.AST != nil {
		escapeToken = input.AST.EscapeToken
	}

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		variant := resolveShellVariant(sem, stageIdx)
		// For sort-packages, attempt bash parsing even on cmd/PowerShell stages.
		// Simple install commands (choco install -y pkg1 pkg2) are syntactically
		// valid bash. FindInstallPackages returns nil if the parser fails.
		if !variant.IsParseable() {
			variant = shell.VariantBash
		}
		for _, cmd := range stage.Commands {
			if run, ok := cmd.(*instructions.RunCommand); ok {
				violations = append(violations,
					r.checkRun(run, variant, input.File, sm, escapeToken, meta)...)
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
	escapeToken rune,
	meta rules.RuleMetadata,
) []rules.Violation {
	if len(run.Location()) == 0 {
		return nil
	}

	// Skip exec-form RUN: RUN ["apt-get", "install", "curl"]
	if !run.PrependShell {
		return nil
	}

	isHeredoc := len(run.Files) > 0
	script := getRunScriptFromCmd(run)
	if script == "" {
		return nil
	}

	startLine := run.Location()[0].Start.Line
	loc := rules.NewLocationFromRanges(file, run.Location())

	if isHeredoc {
		// Heredoc body starts on the next line; no column offset.
		installCmds := shell.FindInstallPackages(script, shellVariant)
		return r.collectViolations(installCmds, startLine+1, 0, file, loc, meta)
	}

	// Shell-form: reconstruct source text from source map lines.
	endLine := sm.ResolveEndLineWithEscape(run.Location()[0].End.Line, escapeToken)
	instrLines := make([]string, 0, endLine-startLine+1)
	for l := startLine; l <= endLine; l++ {
		instrLines = append(instrLines, sm.Line(l-1))
	}
	cmdStartCol := findCmdStartCol(instrLines[0])
	sourceText := shell.ReconstructSourceText(instrLines, cmdStartCol, escapeToken)
	installCmds := shell.FindInstallPackages(sourceText, shellVariant)
	return r.collectViolations(installCmds, startLine, cmdStartCol, file, loc, meta)
}

// collectViolations checks each install command and returns any violations.
func (r *SortPackagesRule) collectViolations(
	installCmds []shell.InstallCommand,
	startLine int,
	cmdStartCol int,
	file string,
	loc rules.Location,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
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
	// Collect literals only — variables are never touched by edits.
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

	// Check if literals are already sorted
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

	// Choose edit strategy based on whether variables are present.
	// When variables exist, we use insert+delete: insert sorted literals as a
	// block before the first package, then delete each literal from its
	// original position. This moves literals to the front (variables end up at
	// the tail) without ever emitting an edit on a variable token — avoiding
	// conflicts with rules like SC2086 that quote variable references.
	hasVars := len(literals) < len(ic.Packages)
	// Use insert+delete for single-line mixed commands (variables end up at
	// tail without editing variable tokens). For multi-line or literal-only
	// commands, use slot-based swaps among literal positions.
	singleLine := ic.Packages[0].Line == ic.Packages[len(ic.Packages)-1].Line
	var edits []rules.TextEdit
	if hasVars && singleLine {
		edits = r.buildInsertDeleteEdits(ic.Packages, literals, sorted, startLine, cmdStartCol, file)
	} else {
		edits = r.buildSlotEdits(literals, sorted, startLine, cmdStartCol, file)
	}

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

// buildInsertDeleteEdits handles mixed literal+variable commands.
// It inserts sorted literals as a block before the first package, then deletes
// each literal from its original position. Variables are never touched.
// The fix engine applies edits back-to-front, so deletions (later positions)
// apply first, then insertions (earlier positions).
func (r *SortPackagesRule) buildInsertDeleteEdits(
	allPkgs []shell.PackageArg,
	literals []shell.PackageArg,
	sorted []shell.PackageArg,
	startLine int,
	cmdStartCol int,
	file string,
) []rules.TextEdit {
	edits := make([]rules.TextEdit, 0, len(literals)*2)

	// Insertion point: right before the first package argument (literal or variable).
	first := allPkgs[0]
	insertLine := startLine + first.Line
	insertCol := first.StartCol
	if first.Line == 0 {
		insertCol += cmdStartCol
	}
	insertLoc := rules.NewRangeLocation(file, insertLine, insertCol, insertLine, insertCol)

	// Build the insertion text: space-separated sorted literals with a
	// trailing space to separate from the remaining variable arguments.
	var sb strings.Builder
	for i, lit := range sorted {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(lit.Value)
	}
	sb.WriteByte(' ')
	edits = append(edits, rules.TextEdit{
		Location: insertLoc,
		NewText:  sb.String(),
	})

	// Delete each literal from its original position. Include the preceding
	// space in the deletion span so no extra whitespace is left behind.
	for _, lit := range literals {
		docLine := startLine + lit.Line
		docStartCol := lit.StartCol
		docEndCol := lit.EndCol
		if lit.Line == 0 {
			docStartCol += cmdStartCol
			docEndCol += cmdStartCol
		}
		// Extend deletion to include one preceding space.
		if docStartCol > 0 {
			docStartCol--
		}
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, docLine, docStartCol, docLine, docEndCol),
			NewText:  "",
		})
	}

	return edits
}

// sortKey extracts a case-insensitive sort key from a package name by stripping
// version specifiers. Examples:
//   - "flask==2.0" → "flask"
//   - "curl=7.88.1" → "curl"
//   - "react@18.2.0" → "react" (npm unscoped: strip version after @)
//   - "@eslint/js" → "@eslint/js"
//   - "@eslint/js@8.0.0" → "@eslint/js" (npm scoped: strip version after last @)
func sortKey(pkg string) string {
	key := pkg

	// Handle npm scoped packages: @scope/name@version → @scope/name
	if strings.HasPrefix(key, "@") {
		lastAt := strings.LastIndex(key, "@")
		if lastAt > 0 { // Must be > 0 to skip the leading @
			key = key[:lastAt]
		}
		return strings.ToLower(key)
	}

	// Handle unscoped npm packages: name@version → name
	// Guard against URLs (git+ssh://git@host/repo) and path-like specs.
	if at := strings.LastIndex(key, "@"); at > 0 &&
		!strings.Contains(key, "://") &&
		!strings.Contains(key[:at], "/") {
		key = key[:at]
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
