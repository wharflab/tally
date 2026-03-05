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

// sourceContext bundles source-level information needed for edit generation.
type sourceContext struct {
	file        string
	instrLines  []string // raw Dockerfile lines; nil for heredoc
	escapeToken rune
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
		FixPriority:     9, // Before no-multi-spaces (10) to avoid edit conflicts
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
		src := sourceContext{file: file, escapeToken: escapeToken}
		return r.collectViolations(installCmds, startLine+1, 0, src, loc, meta)
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
	src := sourceContext{file: file, instrLines: instrLines, escapeToken: escapeToken}
	return r.collectViolations(installCmds, startLine, cmdStartCol, src, loc, meta)
}

// collectViolations checks each install command and returns any violations.
func (r *SortPackagesRule) collectViolations(
	installCmds []shell.InstallCommand,
	startLine int,
	cmdStartCol int,
	src sourceContext,
	loc rules.Location,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, ic := range installCmds {
		if v := r.checkInstallCommand(ic, startLine, cmdStartCol, src, loc, meta); v != nil {
			violations = append(violations, *v)
		}
	}
	return violations
}

// packageSortOrder compares two packages for sorting: literals rank above
// variables, literals are compared by case-insensitive sort key, and any two
// variables are treated as equal so stable sort preserves their relative order.
func packageSortOrder(a, b shell.PackageArg) int {
	if a.IsVar != b.IsVar {
		if a.IsVar {
			return 1 // variables sort after literals
		}
		return -1
	}
	if a.IsVar { // both variables — keep original order
		return 0
	}
	return strings.Compare(sortKey(a.Normalized), sortKey(b.Normalized))
}

// checkInstallCommand checks a single install command for unsorted packages.
func (r *SortPackagesRule) checkInstallCommand(
	ic shell.InstallCommand,
	startLine int,
	cmdStartCol int,
	src sourceContext,
	loc rules.Location,
	meta rules.RuleMetadata,
) *rules.Violation {
	// Collect literals in their original order (ignoring variables).
	var literals []shell.PackageArg
	for _, pkg := range ic.Packages {
		if !pkg.IsVar {
			literals = append(literals, pkg)
		}
	}
	if len(literals) < 2 {
		return nil
	}

	// Check if literals are already sorted (ignoring interleaved variables).
	sorted := make([]shell.PackageArg, len(literals))
	copy(sorted, literals)
	slices.SortStableFunc(sorted, packageSortOrder)

	alreadySorted := true
	for i := range literals {
		if literals[i].Value != sorted[i].Value {
			alreadySorted = false
			break
		}
	}
	if alreadySorted {
		return nil
	}

	// Build the full desired order for edits: sorted literals first, then
	// variables in original relative order.
	fullSorted := make([]shell.PackageArg, 0, len(ic.Packages))
	fullSorted = append(fullSorted, sorted...)
	for _, pkg := range ic.Packages {
		if pkg.IsVar {
			fullSorted = append(fullSorted, pkg)
		}
	}

	// Build edits using insert+delete: replace the first literal with the
	// sorted block, delete remaining literals. Variable tokens are never
	// touched, avoiding conflicts with rules like SC2086.
	// For multi-line, each sorted literal is placed at the corresponding
	// literal's original line position to preserve continuation formatting.
	edits := r.buildEdits(ic.Packages, fullSorted, startLine, cmdStartCol, src)

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

// buildEdits generates insert+delete edits to sort packages. Replaces the first
// literal with the sorted block, deletes remaining literals. Variable tokens are
// never edited — they naturally end up at the tail.
//
// When the first package is a variable, a zero-width insert before it is used
// instead (no overlap with literal deletes at different positions).
//
// For multi-line commands, also emits cleanup edits to remove continuation lines
// left empty after literal deletions.
func (r *SortPackagesRule) buildEdits(
	original []shell.PackageArg,
	sorted []shell.PackageArg,
	startLine int,
	cmdStartCol int,
	src sourceContext,
) []rules.TextEdit {
	nLiterals := 0
	for _, pkg := range original {
		if !pkg.IsVar {
			nLiterals++
		}
	}
	edits := make([]rules.TextEdit, 0, nLiterals+1)

	// Build sorted literal text.
	var sb strings.Builder
	for i := range nLiterals {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(sorted[i].Value)
	}
	insertText := sb.String()

	// Track which shell-lines have a literal deleted (for cleanup).
	deletedOnLine := make(map[int]int) // shell line → count of deletions
	insertLine := -1                   // shell line receiving the sorted block

	if original[0].IsVar {
		// Zero-width insert before the first package (a variable).
		first := original[0]
		docLine := startLine + first.Line
		insertCol := first.StartCol
		if first.Line == 0 {
			insertCol += cmdStartCol
		}
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(src.file, docLine, insertCol, docLine, insertCol),
			NewText:  insertText + " ",
		})
		insertLine = first.Line
		// Delete ALL literals including the preceding space.
		for _, pkg := range original {
			if pkg.IsVar {
				continue
			}
			docLine := startLine + pkg.Line
			docStartCol := pkg.StartCol
			docEndCol := pkg.EndCol
			if pkg.Line == 0 {
				docStartCol += cmdStartCol
				docEndCol += cmdStartCol
			}
			if docStartCol > 0 {
				docStartCol--
			}
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(src.file, docLine, docStartCol, docLine, docEndCol),
				NewText:  "",
			})
			deletedOnLine[pkg.Line]++
		}
	} else {
		// First package is a literal — replace it with sorted block, delete rest.
		firstLitDone := false
		for _, pkg := range original {
			if pkg.IsVar {
				continue
			}
			docLine := startLine + pkg.Line
			docStartCol := pkg.StartCol
			docEndCol := pkg.EndCol
			if pkg.Line == 0 {
				docStartCol += cmdStartCol
				docEndCol += cmdStartCol
			}
			if !firstLitDone {
				edits = append(edits, rules.TextEdit{
					Location: rules.NewRangeLocation(src.file, docLine, docStartCol, docLine, docEndCol),
					NewText:  insertText,
				})
				insertLine = pkg.Line
				firstLitDone = true
			} else {
				if docStartCol > 0 {
					docStartCol--
				}
				edits = append(edits, rules.TextEdit{
					Location: rules.NewRangeLocation(src.file, docLine, docStartCol, docLine, docEndCol),
					NewText:  "",
				})
				deletedOnLine[pkg.Line]++
			}
		}
	}

	edits = append(edits, cleanupAfterDeletions(
		original, deletedOnLine, insertLine, startLine, src,
	)...)
	return edits
}

// cleanupAfterDeletions emits edits to remove continuation lines left
// completely empty after literal deletions. For each empty line, removes
// the trailing backslash + whitespace from the previous line through the
// end of the empty line.
func cleanupAfterDeletions(
	original []shell.PackageArg,
	deletedOnLine map[int]int,
	insertLine int,
	startLine int,
	src sourceContext,
) []rules.TextEdit {
	if len(deletedOnLine) == 0 || src.instrLines == nil {
		return nil
	}

	pkgsPerLine := make(map[int]int)
	for _, pkg := range original {
		pkgsPerLine[pkg.Line]++
	}

	edits := make([]rules.TextEdit, 0, len(deletedOnLine))
	for shellLine, nDeleted := range deletedOnLine {
		if shellLine == insertLine || nDeleted < pkgsPerLine[shellLine] {
			continue
		}
		prevIdx := shellLine - 1
		if prevIdx < 0 || prevIdx >= len(src.instrLines) || shellLine >= len(src.instrLines) {
			continue
		}
		prevLine := src.instrLines[prevIdx]
		trimmed := strings.TrimRight(prevLine, " \t")
		if trimmed == "" || trimmed[len(trimmed)-1] != byte(src.escapeToken) {
			continue // no continuation character
		}
		bsCol := len(strings.TrimRight(trimmed[:len(trimmed)-1], " \t"))
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(
				src.file,
				startLine+prevIdx, bsCol,
				startLine+shellLine, len(src.instrLines[shellLine]),
			),
			NewText: "",
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
