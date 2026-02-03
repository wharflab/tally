package buildkit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type upstreamRuleDef struct {
	VarName      string
	Name         string
	URL          string
	Deprecated   bool
	Experimental bool
}

type upstreamRuleUsage struct {
	ParsePhase bool
	LLBPhase   bool
}

func TestRegistryMatchesUpstreamBuildkitRules(t *testing.T) {
	defs := mustUpstreamRuleDefinitions(t)
	want := make([]string, 0, len(defs))
	for _, d := range defs {
		if d.Deprecated {
			continue
		}
		want = append(want, d.Name)
	}
	slices.Sort(want)

	got := make([]string, 0, len(Registry))
	for name := range Registry {
		got = append(got, name)
	}
	slices.Sort(got)

	missing, extra := diffSortedStrings(want, got)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf(
			"BuildKit rule registry out of sync with upstream moby/buildkit.\nMissing: %v\nExtra: %v",
			missing,
			extra,
		)
	}
}

func TestCapturedRuleNamesMatchUpstreamParsePhaseRules(t *testing.T) {
	defs := mustUpstreamRuleDefinitions(t)
	usages := mustUpstreamRuleUsages(t)
	parserWarningURLs := mustUpstreamParserWarningURLs(t)

	expectedSet := make(map[string]struct{})
	for _, d := range defs {
		if d.Deprecated {
			continue
		}
		u := usages[d.VarName]
		if u.ParsePhase || parserWarningURLs[d.URL] {
			expectedSet[d.Name] = struct{}{}
		}
	}

	want := make([]string, 0, len(expectedSet))
	for name := range expectedSet {
		want = append(want, name)
	}
	slices.Sort(want)

	got := slices.Clone(CapturedRuleNames)
	slices.Sort(got)

	missing, extra := diffSortedStrings(want, got)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf(
			"CapturedRuleNames is out of sync with upstream BuildKit parse-time rules.\nMissing: %v\nExtra: %v",
			missing,
			extra,
		)
	}
}

func diffSortedStrings(want, got []string) ([]string, []string) {
	var missing, extra []string
	i, j := 0, 0
	for i < len(want) && j < len(got) {
		switch {
		case want[i] == got[j]:
			i++
			j++
		case want[i] < got[j]:
			missing = append(missing, want[i])
			i++
		default:
			extra = append(extra, got[j])
			j++
		}
	}
	for ; i < len(want); i++ {
		missing = append(missing, want[i])
	}
	for ; j < len(got); j++ {
		extra = append(extra, got[j])
	}
	return missing, extra
}

func mustBuildkitModuleDir(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/moby/buildkit")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list buildkit module dir: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("empty buildkit module dir from go list")
	}
	return dir
}

func mustUpstreamRuleDefinitions(t *testing.T) []upstreamRuleDef {
	t.Helper()

	buildkitDir := mustBuildkitModuleDir(t)
	linterDir := filepath.Join(buildkitDir, "frontend", "dockerfile", "linter")

	entries, err := readDirNames(linterDir)
	if err != nil {
		t.Fatalf("read linter dir: %v", err)
	}

	byVar := make(map[string]upstreamRuleDef)
	for _, name := range entries {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filePath := filepath.Join(linterDir, name)
		defs, err := parseRuleDefsFromFile(filePath)
		if err != nil {
			t.Fatalf("parse upstream rules from %s: %v", filePath, err)
		}
		maps.Copy(byVar, defs)
	}

	out := make([]upstreamRuleDef, 0, len(byVar))
	for _, d := range byVar {
		if d.Name == "" {
			t.Fatalf("upstream rule %s has empty Name", d.VarName)
		}
		out = append(out, d)
	}
	return out
}

func parseRuleDefsFromFile(path string) (map[string]upstreamRuleDef, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	defs := make(map[string]upstreamRuleDef)
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
				defs[varName] = parseRuleCompositeLit(varName, cl)
			}
		}
	}
	return defs, nil
}

func parseRuleCompositeLit(varName string, cl *ast.CompositeLit) upstreamRuleDef {
	def := upstreamRuleDef{VarName: varName}
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
		case "URL":
			def.URL = mustStringLiteral(kv.Value)
		case "Deprecated":
			def.Deprecated = mustBoolLiteral(kv.Value)
		case "Experimental":
			def.Experimental = mustBoolLiteral(kv.Value)
		}
	}
	return def
}

func mustStringLiteral(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
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

func mustUpstreamRuleUsages(t *testing.T) map[string]upstreamRuleUsage {
	t.Helper()

	buildkitDir := mustBuildkitModuleDir(t)
	root := filepath.Join(buildkitDir, "frontend", "dockerfile")

	fset := token.NewFileSet()
	usages := make(map[string]upstreamRuleUsage)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
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
		t.Fatalf("walk buildkit dockerfile dir: %v", err)
	}

	return usages
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

func mustUpstreamParserWarningURLs(t *testing.T) map[string]bool {
	t.Helper()

	buildkitDir := mustBuildkitModuleDir(t)
	parserDir := filepath.Join(buildkitDir, "frontend", "dockerfile", "parser")

	entries, err := readDirNames(parserDir)
	if err != nil {
		t.Fatalf("read parser dir: %v", err)
	}

	const urlPrefix = "https://docs.docker.com/go/dockerfile/rule/"

	fset := token.NewFileSet()
	urls := make(map[string]bool)
	for _, name := range entries {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(parserDir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
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
	return urls
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
