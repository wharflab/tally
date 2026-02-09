package main

import (
	"bytes"
	"cmp"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
	_ "github.com/tinovyatkin/tally/internal/rules/all"
	bkregistry "github.com/tinovyatkin/tally/internal/rules/buildkit"
	buildkitfixes "github.com/tinovyatkin/tally/internal/rules/buildkit/fixes"
)

const (
	readmePath = "README.md"
	rulesPath  = "RULES.md"

	readmeBeginMarker = "<!-- BEGIN RULES_TABLE -->"
	readmeEndMarker   = "<!-- END RULES_TABLE -->"

	rulesSummaryBeginMarker = "<!-- BEGIN RULES_SUMMARY -->"
	rulesSummaryEndMarker   = "<!-- END RULES_SUMMARY -->"

	buildkitBeginMarker = "<!-- BEGIN BUILDKIT_RULES -->"
	buildkitEndMarker   = "<!-- END BUILDKIT_RULES -->"
)

type buildkitRuleDef struct {
	VarName      string
	Name         string
	Description  string
	URL          string
	Experimental bool
	Deprecated   bool
}

type buildkitRuleUsage struct {
	ParsePhase bool
	LLBPhase   bool
}

type hadolintStatusFile struct {
	Rules map[string]struct {
		Status string `json:"status"`
	} `json:"rules"`
}

func main() {
	targets, err := parseTargets()
	if err != nil {
		fatalf("%v", err)
	}
	if !targets.readme && !targets.summary && !targets.buildkit {
		fmt.Fprintln(os.Stderr, "Nothing to do: use --update/--check or their per-file variants")
		os.Exit(2)
	}

	if err := run(targets); err != nil {
		fatalf("%v", err)
	}
}

type runMode int

const (
	modeUpdate runMode = iota
	modeCheck
)

type targets struct {
	mode     runMode
	readme   bool
	summary  bool
	buildkit bool
}

func parseTargets() (targets, error) {
	update := flag.Bool("update", false, "Update RULES.md and README.md in place")
	updateReadme := flag.Bool("update-readme", false, "Update README.md in place")
	updateSummary := flag.Bool("update-summary", false, "Update RULES.md summary table in place")
	updateBuildkt := flag.Bool("update-buildkit", false, "Update BuildKit section in RULES.md in place")

	check := flag.Bool("check", false, "Verify README.md / RULES.md are up to date (no changes)")
	checkReadme := flag.Bool("check-readme", false, "Verify README.md is up to date (no changes)")
	checkSummary := flag.Bool("check-summary", false, "Verify RULES.md summary table is up to date (no changes)")
	checkBuildkt := flag.Bool("check-buildkit", false, "Verify BuildKit section in RULES.md is up to date (no changes)")
	flag.Parse()

	checkRequested := *check || *checkReadme || *checkSummary || *checkBuildkt
	updateRequested := *update || *updateReadme || *updateSummary || *updateBuildkt
	if checkRequested && updateRequested {
		return targets{}, errors.New("cannot combine --update* and --check* flags")
	}

	mode := modeUpdate
	if checkRequested {
		mode = modeCheck
	}

	if *update || *check {
		*updateReadme = true
		*updateSummary = true
		*updateBuildkt = true
	}

	return targets{
		mode:     mode,
		readme:   *updateReadme || *checkReadme,
		summary:  *updateSummary || *checkSummary,
		buildkit: *updateBuildkt || *checkBuildkt,
	}, nil
}

func run(targets targets) error {
	buildkitDir, err := goListModuleDir("github.com/moby/buildkit")
	if err != nil {
		return fmt.Errorf("failed to locate BuildKit module: %w", err)
	}

	defs, err := parseBuildkitRuleDefinitions(filepath.Join(buildkitDir, "frontend", "dockerfile", "linter"))
	if err != nil {
		return fmt.Errorf("failed to parse BuildKit linter rule definitions: %w", err)
	}
	usages, err := scanBuildkitRuleUsages(filepath.Join(buildkitDir, "frontend", "dockerfile"))
	if err != nil {
		return fmt.Errorf("failed to scan BuildKit rule usages: %w", err)
	}
	parserWarningURLs, err := scanBuildkitParserWarningURLs(filepath.Join(buildkitDir, "frontend", "dockerfile", "parser"))
	if err != nil {
		return fmt.Errorf("failed to scan BuildKit parser warnings: %w", err)
	}

	implBuildkitRules := implementedBuildkitRules()
	implBuildkitMeta := implementedBuildkitRuleMetadata()
	fixable := makeStringSet(buildkitfixes.FixableRuleNames())

	implementedRows, capturedRows, unsupportedRows := classifyBuildkitRules(defs, implBuildkitRules, usages, parserWarningURLs)

	hadolintSupported, hadolintImplemented, hadolintCovered, err := hadolintCounts("internal/rules/hadolint-status.json")
	if err != nil {
		return fmt.Errorf("failed to read Hadolint status file: %w", err)
	}
	hadolintTotal, err := hadolintTotalCount("internal/rules/hadolint-rules.json")
	if err != nil {
		return fmt.Errorf("failed to read Hadolint rules file: %w", err)
	}

	tallyCount := countRegisteredPrefix(rules.TallyRulePrefix)
	buildkitSupported := len(implementedRows) + len(capturedRows)
	buildkitTotal := len(defs)
	if got := len(bkregistry.All()); got != buildkitTotal {
		return fmt.Errorf(
			"internal BuildKit rule registry out of sync: got %d, upstream has %d (update internal/rules/buildkit/registry.go)",
			got,
			buildkitTotal,
		)
	}

	if targets.readme {
		readmeBlock := renderReadmeRulesTable(buildkitSupported, buildkitTotal, tallyCount, hadolintSupported)
		if err := applyOrCheck(
			targets.mode,
			readmePath,
			readmeBeginMarker,
			readmeEndMarker,
			readmeBlock,
			"go run ./scripts/sync-buildkit-rules --update-readme",
		); err != nil {
			return err
		}
	}

	if targets.summary {
		summaryBlock := renderRulesSummaryTable(
			tallyCount,
			len(implementedRows),
			len(capturedRows),
			buildkitTotal,
			hadolintImplemented,
			hadolintCovered,
			hadolintTotal,
		)
		if err := applyOrCheck(
			targets.mode,
			rulesPath,
			rulesSummaryBeginMarker,
			rulesSummaryEndMarker,
			summaryBlock,
			"go run ./scripts/sync-buildkit-rules --update-summary",
		); err != nil {
			return err
		}
	}

	if targets.buildkit {
		buildkitBlock := renderRulesBuildkitSection(
			buildkitSupported,
			buildkitTotal,
			len(capturedRows),
			len(implementedRows),
			len(unsupportedRows),
			implementedRows,
			capturedRows,
			unsupportedRows,
			implBuildkitMeta,
			fixable,
		)
		if err := applyOrCheck(
			targets.mode,
			rulesPath,
			buildkitBeginMarker,
			buildkitEndMarker,
			buildkitBlock,
			"go run ./scripts/sync-buildkit-rules --update-buildkit",
		); err != nil {
			return err
		}
	}

	return nil
}

func applyOrCheck(mode runMode, path, beginMarker, endMarker, newContent, fixCmd string) error {
	switch mode {
	case modeUpdate:
		if err := replaceBetweenMarkers(path, beginMarker, endMarker, newContent); err != nil {
			return fmt.Errorf("failed to update %s: %w", path, err)
		}
		return nil
	case modeCheck:
		if err := checkBetweenMarkers(path, beginMarker, endMarker, newContent); err != nil {
			return fmt.Errorf("%s out of date: %w\nFix: %s", path, err, fixCmd)
		}
		return nil
	default:
		return errors.New("unknown mode")
	}
}

func classifyBuildkitRules(
	defs []buildkitRuleDef,
	implBuildkitRules map[string]bool,
	usages map[string]buildkitRuleUsage,
	parserWarningURLs map[string]bool,
) ([]buildkitRuleDef, []buildkitRuleDef, []buildkitRuleDef) {
	implementedRows := make([]buildkitRuleDef, 0, len(defs))
	capturedRows := make([]buildkitRuleDef, 0, len(defs))
	unsupportedRows := make([]buildkitRuleDef, 0, len(defs))

	for _, d := range defs {
		if implBuildkitRules[d.Name] {
			implementedRows = append(implementedRows, d)
			continue
		}

		u := usages[d.VarName]
		if u.ParsePhase || parserWarningURLs[d.URL] {
			capturedRows = append(capturedRows, d)
			continue
		}

		unsupportedRows = append(unsupportedRows, d)
	}

	slices.SortFunc(implementedRows, func(a, b buildkitRuleDef) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(capturedRows, func(a, b buildkitRuleDef) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(unsupportedRows, func(a, b buildkitRuleDef) int { return cmp.Compare(a.Name, b.Name) })

	return implementedRows, capturedRows, unsupportedRows
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

func goListModuleDir(module string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list failed: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("empty module dir")
	}
	return dir, nil
}

func parseBuildkitRuleDefinitions(linterDir string) ([]buildkitRuleDef, error) {
	entries, err := os.ReadDir(linterDir)
	if err != nil {
		return nil, err
	}

	byVar := make(map[string]buildkitRuleDef)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		defs, err := parseBuildkitRuleDefinitionsFromFile(filepath.Join(linterDir, e.Name()))
		if err != nil {
			return nil, err
		}

		maps.Copy(byVar, defs)
	}

	result := make([]buildkitRuleDef, 0, len(byVar))
	for _, d := range byVar {
		// Exclude deprecated rules (BuildKit won't run them).
		if d.Deprecated {
			continue
		}
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b buildkitRuleDef) int { return cmp.Compare(a.Name, b.Name) })
	return result, nil
}

func parseBuildkitRuleDefinitionsFromFile(path string) (map[string]buildkitRuleDef, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	defs := make(map[string]buildkitRuleDef)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			addRuleDefsFromValueSpec(defs, vs)
		}
	}
	return defs, nil
}

func addRuleDefsFromValueSpec(dst map[string]buildkitRuleDef, vs *ast.ValueSpec) {
	for i, nameIdent := range vs.Names {
		if nameIdent == nil {
			continue
		}
		varName := nameIdent.Name
		if !strings.HasPrefix(varName, "Rule") {
			continue
		}
		if i >= len(vs.Values) {
			continue
		}
		cl, ok := vs.Values[i].(*ast.CompositeLit)
		if !ok {
			continue
		}
		dst[varName] = parseRuleCompositeLit(varName, cl)
	}
}

func parseRuleCompositeLit(varName string, cl *ast.CompositeLit) buildkitRuleDef {
	def := buildkitRuleDef{VarName: varName}
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Name":
			def.Name = mustStringLiteral(kv.Value)
		case "Description":
			def.Description = mustStringLiteral(kv.Value)
		case "URL":
			def.URL = mustStringLiteral(kv.Value)
		case "Experimental":
			def.Experimental = mustBoolLiteral(kv.Value)
		case "Deprecated":
			def.Deprecated = mustBoolLiteral(kv.Value)
		}
	}

	// Some buildkit rules might omit Name/Description/URL; keep but with defaults.
	if def.Name == "" {
		def.Name = strings.TrimPrefix(def.VarName, "Rule")
	}
	return def
}

func mustStringLiteral(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return ""
	}
	return s
}

func mustBoolLiteral(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "true"
}

func scanBuildkitRuleUsages(dockerfileRoot string) (map[string]buildkitRuleUsage, error) {
	fset := token.NewFileSet()
	usages := make(map[string]buildkitRuleUsage)

	err := filepath.WalkDir(dockerfileRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip docs or tests inside BuildKit module (we only care about runtime checks).
			base := filepath.Base(path)
			if base == "docs" || base == "testdata" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Run" {
				return true
			}
			if len(call.Args) < 1 {
				return true
			}
			ruleVar := extractRuleVarName(call.Args[0])
			if ruleVar == "" {
				return true
			}
			if !strings.HasPrefix(ruleVar, "Rule") {
				return true
			}

			u := usages[ruleVar]
			if strings.Contains(path, string(filepath.Separator)+"dockerfile2llb"+string(filepath.Separator)) {
				u.LLBPhase = true
			} else {
				u.ParsePhase = true
			}
			usages[ruleVar] = u
			return true
		})

		return nil
	})
	if err != nil {
		return nil, err
	}
	return usages, nil
}

func scanBuildkitParserWarningURLs(parserDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(parserDir)
	if err != nil {
		return nil, err
	}

	const urlPrefix = "https://docs.docker.com/go/dockerfile/rule/"

	fset := token.NewFileSet()
	urls := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(parserDir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "URL" {
				return true
			}
			url := mustStringLiteral(kv.Value)
			if strings.HasPrefix(url, urlPrefix) {
				urls[url] = true
			}
			return true
		})
	}
	return urls, nil
}

func extractRuleVarName(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.AND {
		expr = u.X
	}
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if e.Sel != nil {
			return e.Sel.Name
		}
	case *ast.Ident:
		return e.Name
	}
	return ""
}

func implementedBuildkitRules() map[string]bool {
	impl := make(map[string]bool)
	for _, r := range rules.All() {
		if !strings.HasPrefix(r.Metadata().Code, rules.BuildKitRulePrefix) {
			continue
		}
		name := strings.TrimPrefix(r.Metadata().Code, rules.BuildKitRulePrefix)
		impl[name] = true
	}
	return impl
}

func implementedBuildkitRuleMetadata() map[string]rules.RuleMetadata {
	meta := make(map[string]rules.RuleMetadata)
	for _, r := range rules.All() {
		if !strings.HasPrefix(r.Metadata().Code, rules.BuildKitRulePrefix) {
			continue
		}
		name := strings.TrimPrefix(r.Metadata().Code, rules.BuildKitRulePrefix)
		meta[name] = r.Metadata()
	}
	return meta
}

func countRegisteredPrefix(prefix string) int {
	n := 0
	for _, r := range rules.All() {
		if strings.HasPrefix(r.Metadata().Code, prefix) {
			n++
		}
	}
	return n
}

func hadolintCounts(path string) (int, int, int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	var st hadolintStatusFile
	if err := json.Unmarshal(b, &st); err != nil {
		return 0, 0, 0, err
	}
	var supported, implemented, covered int
	for _, v := range st.Rules {
		switch v.Status {
		case "implemented":
			supported++
			implemented++
		case "covered_by_buildkit", "covered_by_tally":
			supported++
			covered++
		}
	}
	return supported, implemented, covered, nil
}

func hadolintTotalCount(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var arr []any
	if err := json.Unmarshal(b, &arr); err != nil {
		return 0, err
	}
	return len(arr), nil
}

func renderReadmeRulesTable(buildkitSupported, buildkitTotal, tallyCount, hadolintSupported int) string {
	var b strings.Builder
	b.WriteString("| Source | Rules | Description |\n")
	b.WriteString("|--------|-------|-------------|\n")

	b.WriteString("| **[BuildKit](https://docs.docker.com/reference/build-checks/)** | ")
	fmt.Fprintf(&b, "%d/%d rules | ", buildkitSupported, buildkitTotal)
	b.WriteString("Docker's official Dockerfile checks (captured + reimplemented) |\n")

	b.WriteString("| **tally** | ")
	fmt.Fprintf(&b, "%d rules | ", tallyCount)
	b.WriteString("Custom rules including secret detection with [gitleaks](https://github.com/gitleaks/gitleaks) |\n")

	b.WriteString("| **[Hadolint](https://github.com/hadolint/hadolint)** | ")
	fmt.Fprintf(&b, "%d rules | ", hadolintSupported)
	b.WriteString("Hadolint-compatible Dockerfile rules (expanding) |\n")

	return b.String()
}

func renderRulesSummaryTable(
	tallyCount int,
	buildkitImplemented int,
	buildkitCaptured int,
	buildkitTotal int,
	hadolintImplemented int,
	hadolintCovered int,
	hadolintTotal int,
) string {
	return fmt.Sprintf(
		""+
			"| Namespace | Implemented | Covered by BuildKit | Total |\n"+
			"|-----------|-------------|---------------------|-------|\n"+
			"| tally | %d | - | %d |\n"+
			"| buildkit | %d + %d captured | - | %d |\n"+
			"| hadolint | %d | %d | %d |\n",
		tallyCount,
		tallyCount,
		buildkitImplemented,
		buildkitCaptured,
		buildkitTotal,
		hadolintImplemented,
		hadolintCovered,
		hadolintTotal,
	)
}

func renderRulesBuildkitSection(
	supported,
	total,
	captured,
	implemented,
	unsupported int,
	implementedDefs,
	capturedDefs,
	unsupportedDefs []buildkitRuleDef,
	implementedMeta map[string]rules.RuleMetadata,
	fixable map[string]bool,
) string {
	var b strings.Builder

	b.WriteString("<!-- Auto-generated by scripts/sync-buildkit-rules -->\n\n")
	fmt.Fprintf(&b, "tally supports **%d/%d** BuildKit checks:\n\n", supported, total)
	fmt.Fprintf(&b, "- **%d** captured during parsing\n", captured)
	fmt.Fprintf(&b, "- **%d** reimplemented by tally (BuildKit normally runs these during LLB conversion)\n", implemented)
	if unsupported > 0 {
		fmt.Fprintf(&b, "- **%d** not currently supported (LLB conversion only)\n", unsupported)
	}
	fmt.Fprintf(&b, "- **%d** with auto-fixes (🔧)\n\n", countFixable(implementedDefs, capturedDefs, fixable))

	b.WriteString("### Implemented by tally\n\n")
	b.WriteString("These BuildKit checks run during LLB conversion in Docker/BuildKit. ")
	b.WriteString("tally reimplements them so they work as a pure static linter.\n\n")
	b.WriteString(renderImplementedBuildkitTable(implementedDefs, implementedMeta, fixable))
	b.WriteString("\n\n")

	b.WriteString("### Captured from BuildKit linter\n\n")
	b.WriteString("These checks are emitted by BuildKit during Dockerfile parsing and are captured directly by tally.\n\n")
	b.WriteString(renderCapturedBuildkitTable(capturedDefs, fixable))
	b.WriteString("\n\n")

	if len(unsupportedDefs) > 0 {
		b.WriteString("### Not currently supported\n\n")
		b.WriteString("These BuildKit checks only run during LLB conversion (a full build). tally does not run that conversion.\n\n")
		b.WriteString(renderUnsupportedTable(unsupportedDefs))
		b.WriteString("\n\n")
	}

	b.WriteString("See [Docker Build Checks](https://docs.docker.com/reference/build-checks/) for detailed documentation.\n")
	return b.String()
}

func countFixable(impl, captured []buildkitRuleDef, fixable map[string]bool) int {
	n := 0
	for _, d := range impl {
		if fixable[d.Name] {
			n++
		}
	}
	for _, d := range captured {
		if fixable[d.Name] {
			n++
		}
	}
	return n
}

func renderImplementedBuildkitTable(defs []buildkitRuleDef, meta map[string]rules.RuleMetadata, fixable map[string]bool) string {
	if len(defs) == 0 {
		return "_None._"
	}

	var b strings.Builder

	b.WriteString("| Rule | Description | Severity | Category | Default |\n")
	b.WriteString("|------|-------------|----------|----------|---------|\n")
	for _, d := range defs {
		m := meta[d.Name]
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			buildkitRuleLinkWithFix(d, fixable[d.Name]),
			escapePipes(d.Description),
			titleCase(m.DefaultSeverity.String()),
			titleCategory(m.Category),
			buildkitDefaultLabel(d),
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderCapturedBuildkitTable(defs []buildkitRuleDef, fixable map[string]bool) string {
	if len(defs) == 0 {
		return "_None._"
	}

	var b strings.Builder
	b.WriteString("| Rule | Description | Severity | Default | Status |\n")
	b.WriteString("|------|-------------|----------|---------|--------|\n")
	for _, d := range defs {
		status := "✅"
		if fixable[d.Name] {
			status = "✅🔧"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			buildkitRuleLinkWithFix(d, fixable[d.Name]),
			escapePipes(d.Description),
			"Warning",
			buildkitDefaultLabel(d),
			status,
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderUnsupportedTable(defs []buildkitRuleDef) string {
	var b strings.Builder
	b.WriteString("| Rule | Description | Notes |\n")
	b.WriteString("|------|-------------|-------|\n")
	for _, d := range defs {
		fmt.Fprintf(&b, "| %s | %s | %s |\n",
			buildkitRuleLinkWithFix(d, false),
			escapePipes(d.Description),
			"Requires LLB conversion (full build)",
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func buildkitRuleLink(d buildkitRuleDef) string {
	if href := buildkitDocsURL(d.URL); href != "" {
		return fmt.Sprintf("[`buildkit/%s`](%s)", d.Name, href)
	}
	return fmt.Sprintf("`buildkit/%s`", d.Name)
}

func buildkitRuleLinkWithFix(d buildkitRuleDef, fixable bool) string {
	link := buildkitRuleLink(d)
	if fixable {
		return link + " 🔧"
	}
	return link
}

func buildkitDocsURL(buildkitURL string) string {
	const prefix = "https://docs.docker.com/go/dockerfile/rule/"
	if !strings.HasPrefix(buildkitURL, prefix) {
		return ""
	}
	slug := strings.TrimPrefix(buildkitURL, prefix)
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" {
		return ""
	}
	return "https://docs.docker.com/reference/build-checks/" + slug + "/"
}

func buildkitDefaultLabel(d buildkitRuleDef) string {
	if d.Experimental {
		return "Off (experimental)"
	}
	return "Enabled"
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func titleCategory(cat string) string {
	switch cat {
	case "best-practice":
		return "Best Practice"
	case "best-practices":
		return "Best Practice"
	default:
		return titleCase(cat)
	}
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

type markerBounds struct {
	begin    int
	beginEnd int
	end      int
}

func findMarkerBounds(orig []byte, beginMarker, endMarker string) (markerBounds, error) {
	begin := bytes.Index(orig, []byte(beginMarker))
	if begin == -1 {
		return markerBounds{}, fmt.Errorf("begin marker not found: %s", beginMarker)
	}
	searchFrom := begin + len(beginMarker)
	endRel := bytes.Index(orig[searchFrom:], []byte(endMarker))
	if endRel == -1 {
		return markerBounds{}, fmt.Errorf("end marker not found: %s", endMarker)
	}
	end := searchFrom + endRel
	if end < begin {
		return markerBounds{}, errors.New("end marker occurs before begin marker")
	}
	return markerBounds{begin: begin, beginEnd: begin + len(beginMarker), end: end}, nil
}

func replaceBetweenMarkers(path, beginMarker, endMarker, newContent string) error {
	orig, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	bounds, err := findMarkerBounds(orig, beginMarker, endMarker)
	if err != nil {
		return err
	}

	// Keep the markers themselves and replace only the content between them.
	var out bytes.Buffer
	out.Write(orig[:bounds.beginEnd])
	out.WriteByte('\n')
	out.WriteString(strings.TrimRight(newContent, "\n"))
	out.WriteByte('\n')
	out.Write(orig[bounds.end:])

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, out.Bytes(), mode)
}

func checkBetweenMarkers(path, beginMarker, endMarker, newContent string) error {
	orig, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	bounds, err := findMarkerBounds(orig, beginMarker, endMarker)
	if err != nil {
		return err
	}
	existing := string(orig[bounds.beginEnd:bounds.end])

	normalize := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.TrimPrefix(s, "\n")
		s = strings.TrimSuffix(s, "\n")
		s = strings.TrimSuffix(s, "\n")
		return s
	}

	want := normalize(strings.TrimRight(newContent, "\n"))
	got := normalize(existing)
	if got != want {
		return errors.New("generated content differs")
	}
	return nil
}

func makeStringSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[it] = true
	}
	return set
}
