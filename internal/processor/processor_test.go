package processor

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/rules"
)

func TestChain(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("a.txt", 1), "rule1", "message1", rules.SeverityWarning),
		rules.NewViolation(rules.NewLineLocation("b.txt", 2), "rule2", "message2", rules.SeverityError),
	}

	// Chain that filters out all violations
	chain := NewChain(&mockProcessor{name: "filter-all", filter: func(v rules.Violation) bool { return false }})
	ctx := NewContext(nil, config.Default(), nil)

	result := chain.Process(violations, ctx)
	if len(result) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result))
	}
}

func TestPathNormalization(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("path\\to\\file.txt", 1), "rule1", "msg", rules.SeverityWarning),
	}

	p := NewPathNormalization()
	ctx := NewContext(nil, config.Default(), nil)

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].Location.File != "path/to/file.txt" {
		t.Errorf("expected path/to/file.txt, got %s", result[0].Location.File)
	}
}

func TestDeduplication(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 1), "rule1", "msg1", rules.SeverityWarning),
		// duplicate
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 1), "rule1", "msg2", rules.SeverityWarning),
		// different line
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 2), "rule1", "msg3", rules.SeverityWarning),
		// different rule
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 1), "rule2", "msg4", rules.SeverityWarning),
	}

	p := NewDeduplication()
	ctx := NewContext(nil, config.Default(), nil)

	result := p.Process(violations, ctx)
	if len(result) != 3 {
		t.Errorf("expected 3 unique violations, got %d", len(result))
	}
}

func TestSorting(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("b.txt", 2), "rule2", "msg", rules.SeverityWarning),
		rules.NewViolation(rules.NewLineLocation("a.txt", 1), "rule1", "msg", rules.SeverityWarning),
		rules.NewViolation(rules.NewLineLocation("b.txt", 1), "rule1", "msg", rules.SeverityWarning),
	}

	p := NewSorting()
	ctx := NewContext(nil, config.Default(), nil)

	result := p.Process(violations, ctx)
	if len(result) != 3 {
		t.Fatalf("expected 3 violations, got %d", len(result))
	}

	// Should be sorted by file, then line
	if result[0].Location.File != "a.txt" {
		t.Errorf("first violation should be in a.txt, got %s", result[0].Location.File)
	}
	if result[1].Location.File != "b.txt" || result[1].Location.Start.Line != 1 {
		t.Errorf(
			"second violation should be b.txt:1, got %s:%d",
			result[1].Location.File, result[1].Location.Start.Line)
	}
	if result[2].Location.File != "b.txt" || result[2].Location.Start.Line != 2 {
		t.Errorf(
			"third violation should be b.txt:2, got %s:%d",
			result[2].Location.File, result[2].Location.Start.Line)
	}
}

func TestEnableFilter(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 1), "tally/max-lines", "msg", rules.SeverityWarning),
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 2),
			"buildkit/StageNameCasing", "msg", rules.SeverityWarning),
	}

	cfg := config.Default()
	// Disable tally/max-lines via exclude
	cfg.Rules.Exclude = append(cfg.Rules.Exclude, "tally/max-lines")

	p := NewEnableFilter()
	ctx := NewContext(nil, cfg, nil)

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation (disabled rule filtered), got %d", len(result))
	}
	if result[0].RuleCode != "buildkit/StageNameCasing" {
		t.Errorf("expected buildkit/StageNameCasing, got %s", result[0].RuleCode)
	}
}

func TestSeverityOverride(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 1), "tally/max-lines", "msg", rules.SeverityWarning),
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 2), "buildkit/StageNameCasing", "msg", rules.SeverityWarning),
	}

	cfg := config.Default()
	// Override tally/max-lines severity to info
	cfg.Rules.Set("tally/max-lines", config.RuleConfig{Severity: "info"})

	p := NewSeverityOverride()
	ctx := NewContext(nil, cfg, nil)

	result := p.Process(violations, ctx)
	if len(result) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(result))
	}
	if result[0].Severity != rules.SeverityInfo {
		t.Errorf("expected severity info for tally/max-lines, got %s", result[0].Severity)
	}
	if result[1].Severity != rules.SeverityWarning {
		t.Errorf("expected severity warning for buildkit/StageNameCasing, got %s", result[1].Severity)
	}
}

func TestPathExclusionFilter(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("src/main.go", 1), "tally/test-rule", "msg", rules.SeverityWarning),
		rules.NewViolation(
			rules.NewLineLocation("test/main_test.go", 1), "tally/test-rule", "msg", rules.SeverityWarning),
		rules.NewViolation(
			rules.NewLineLocation("vendor/lib.go", 1), "tally/test-rule", "msg", rules.SeverityWarning),
	}

	cfg := config.Default()
	cfg.Rules.Set("tally/test-rule", config.RuleConfig{
		Exclude: config.ExcludeConfig{
			Paths: []string{"test/**", "vendor/**"},
		},
	})

	p := NewPathExclusionFilter()
	ctx := NewContext(nil, cfg, nil)

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation (test and vendor excluded), got %d", len(result))
	}
	if result[0].Location.File != "src/main.go" {
		t.Errorf("expected src/main.go, got %s", result[0].Location.File)
	}
}

func TestSnippetAttachment(t *testing.T) {
	source := []byte("line 1\nline 2\nline 3\n")
	violations := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("file.txt", 2), "rule1", "msg", rules.SeverityWarning),
	}

	p := NewSnippetAttachment()
	ctx := NewContext(nil, config.Default(), map[string][]byte{"file.txt": source})

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].SourceCode != "line 2" {
		t.Errorf("expected 'line 2', got %q", result[0].SourceCode)
	}
}

func TestEnableFilter_BuildKitRules(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 1), "buildkit/DuplicateStageName", "msg", rules.SeverityError),
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 2), "tally/max-lines", "msg", rules.SeverityWarning),
	}

	cfg := config.Default()
	// Disable buildkit/DuplicateStageName via exclude
	cfg.Rules.Exclude = append(cfg.Rules.Exclude, "buildkit/DuplicateStageName")

	p := NewEnableFilter()
	ctx := NewContext(nil, cfg, nil)

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation (buildkit rule disabled), got %d", len(result))
	}
	if result[0].RuleCode != "tally/max-lines" {
		t.Errorf("expected tally/max-lines, got %s", result[0].RuleCode)
	}
}

func TestSeverityOverride_BuildKitRules(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 1), "buildkit/DuplicateStageName", "msg", rules.SeverityError),
	}

	cfg := config.Default()
	// Change buildkit/DuplicateStageName severity to warning
	cfg.Rules.Set("buildkit/DuplicateStageName", config.RuleConfig{Severity: "warning"})

	p := NewSeverityOverride()
	ctx := NewContext(nil, cfg, nil)

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].Severity != rules.SeverityWarning {
		t.Errorf("expected severity warning, got %s", result[0].Severity)
	}
}

// mockProcessor is a test helper for custom processor behavior.
type mockProcessor struct {
	name   string
	filter func(v rules.Violation) bool
}

func (m *mockProcessor) Name() string { return m.name }

func (m *mockProcessor) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	if m.filter == nil {
		return violations
	}
	return filterViolations(violations, m.filter)
}

func TestSeverityOverride_AutoEnableOffRules(t *testing.T) {
	testCases := []struct {
		name    string
		message string
	}{
		{"basic auto-enable", "test violation"},
		{"with trusted registries", "untrusted registry"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			registry := rules.NewRegistry()
			mockRule := &mockRuleWithMetadata{
				code:            "hadolint/DL3026",
				defaultSeverity: rules.SeverityOff,
			}
			registry.Register(mockRule)

			violations := []rules.Violation{
				rules.NewViolation(
					rules.NewLineLocation("file.txt", 1),
					"hadolint/DL3026",
					tc.message,
					rules.SeverityOff,
				),
			}

			cfg := config.Default()
			cfg.Rules.Set("hadolint/DL3026", config.RuleConfig{
				Options: map[string]any{
					"trusted-registries": []string{"docker.io"},
				},
			})

			p := NewSeverityOverrideWithRegistry(registry)
			ctx := NewContext(nil, cfg, nil)

			result := p.Process(violations, ctx)
			if len(result) != 1 {
				t.Fatalf("expected 1 violation, got %d", len(result))
			}
			if result[0].Severity != rules.SeverityWarning {
				t.Errorf("expected severity=warning (auto-enabled), got %v", result[0].Severity)
			}
		})
	}
}

// mockRuleWithMetadata is a mock rule for testing
type mockRuleWithMetadata struct {
	code            string
	defaultSeverity rules.Severity
}

func (m *mockRuleWithMetadata) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            m.code,
		DefaultSeverity: m.defaultSeverity,
	}
}

func (m *mockRuleWithMetadata) Check(_ rules.LintInput) []rules.Violation {
	return nil
}
