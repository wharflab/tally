package customlint

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestPlugin_BuildAnalyzers(t *testing.T) {
	t.Parallel()

	p := &plugin{}
	analyzers, err := p.BuildAnalyzers()

	if err != nil {
		t.Fatalf("BuildAnalyzers() returned error: %v", err)
	}

	if analyzers == nil {
		t.Fatal("BuildAnalyzers() returned nil")
	}

	// Check that we have the expected analyzers
	expectedCount := 3
	if len(analyzers) != expectedCount {
		t.Errorf("BuildAnalyzers() returned %d analyzers, want %d", len(analyzers), expectedCount)
	}

	// Verify analyzer names are present
	names := make(map[string]bool)
	for _, a := range analyzers {
		if a == nil {
			t.Error("BuildAnalyzers() contains nil analyzer")
			continue
		}
		names[a.Name] = true
	}

	expectedNames := []string{"rulestruct", "lspliteral", "docurl"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("BuildAnalyzers() missing expected analyzer: %s", name)
		}
	}
}

func TestPlugin_BuildAnalyzers_AnalyzerProperties(t *testing.T) {
	t.Parallel()

	p := &plugin{}
	analyzers, err := p.BuildAnalyzers()
	if err != nil {
		t.Fatalf("BuildAnalyzers() returned error: %v", err)
	}

	// Verify each analyzer has required properties
	for _, a := range analyzers {
		if a.Name == "" {
			t.Error("analyzer has empty Name")
		}

		if a.Doc == "" {
			t.Error("analyzer has empty Doc")
		}

		if a.Run == nil {
			t.Errorf("analyzer %s has nil Run function", a.Name)
		}

		// Verify analyzers have inspect.Analyzer as dependency
		// (this is a common pattern for AST-based analyzers)
		hasInspect := false
		for _, req := range a.Requires {
			if req != nil && req.Name == "inspect" {
				hasInspect = true
				break
			}
		}
		if !hasInspect {
			t.Errorf("analyzer %s does not require inspect.Analyzer", a.Name)
		}
	}
}

func TestPlugin_GetLoadMode(t *testing.T) {
	t.Parallel()

	p := &plugin{}
	mode := p.GetLoadMode()

	// LoadModeSyntax is a constant that equals "syntax"
	expectedMode := "syntax"
	if mode != expectedMode {
		t.Errorf("GetLoadMode() = %q, want %q", mode, expectedMode)
	}
}

func TestAnalyzersAreIndependent(t *testing.T) {
	t.Parallel()

	p := &plugin{}
	analyzers1, err1 := p.BuildAnalyzers()
	analyzers2, err2 := p.BuildAnalyzers()

	if err1 != nil || err2 != nil {
		t.Fatalf("BuildAnalyzers() returned errors: %v, %v", err1, err2)
	}

	// Multiple calls should return the same set of analyzers
	if len(analyzers1) != len(analyzers2) {
		t.Errorf("BuildAnalyzers() returned different counts: %d vs %d", len(analyzers1), len(analyzers2))
	}

	// Check that analyzer instances are the same references
	for i, a1 := range analyzers1 {
		if i >= len(analyzers2) {
			break
		}
		a2 := analyzers2[i]

		if a1 != a2 {
			t.Errorf("BuildAnalyzers() returned different analyzer instances at index %d", i)
		}
	}
}

func TestAnalyzerOrderIsStable(t *testing.T) {
	t.Parallel()

	p := &plugin{}
	analyzers, err := p.BuildAnalyzers()
	if err != nil {
		t.Fatalf("BuildAnalyzers() returned error: %v", err)
	}

	// Verify the order matches the expected registration order
	expectedOrder := []string{"rulestruct", "lspliteral", "docurl"}
	for i, expected := range expectedOrder {
		if i >= len(analyzers) {
			t.Errorf("Expected analyzer %s at index %d, but only %d analyzers returned", expected, i, len(analyzers))
			continue
		}
		if analyzers[i].Name != expected {
			t.Errorf("Analyzer at index %d: got %s, want %s", i, analyzers[i].Name, expected)
		}
	}
}

func TestAllAnalyzersReturnNonNil(t *testing.T) {
	t.Parallel()

	// Verify individual analyzer constructors don't panic
	testCases := []struct {
		name     string
		analyzer *analysis.Analyzer
	}{
		{"ruleStructAnalyzer", ruleStructAnalyzer},
		{"lspLiteralAnalyzer", lspLiteralAnalyzer},
		{"docURLAnalyzer", docURLAnalyzer},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.analyzer == nil {
				t.Errorf("%s is nil", tc.name)
			}
		})
	}
}