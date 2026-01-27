package rules

import (
	"testing"
)

// mockRule is a simple rule for testing.
type mockRule struct {
	code     string
	enabled  bool
	category string
	severity Severity
	expmt    bool
}

func (r *mockRule) Metadata() RuleMetadata {
	return RuleMetadata{
		Code:             r.code,
		Name:             "Mock Rule " + r.code,
		Description:      "A mock rule for testing",
		DefaultSeverity:  r.severity,
		Category:         r.category,
		EnabledByDefault: r.enabled,
		IsExperimental:   r.expmt,
	}
}

func (r *mockRule) Check(input LintInput) []Violation {
	return nil
}

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()

	rule := &mockRule{code: "test-001"}
	reg.Register(rule)

	if !reg.Has("test-001") {
		t.Error("Has() = false after registration")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	reg := NewRegistry()
	rule := &mockRule{code: "dup-001"}
	reg.Register(rule)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()

	reg.Register(rule) // Should panic
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	rule := &mockRule{code: "get-001"}
	reg.Register(rule)

	got := reg.Get("get-001")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Metadata().Code != "get-001" {
		t.Errorf("Get().Code = %q, want %q", got.Metadata().Code, "get-001")
	}

	if reg.Get("nonexistent") != nil {
		t.Error("Get() should return nil for nonexistent rule")
	}
}

func TestRegistry_All(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockRule{code: "c-rule"})
	reg.Register(&mockRule{code: "a-rule"})
	reg.Register(&mockRule{code: "b-rule"})

	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d rules, want 3", len(all))
	}

	// Should be sorted by code
	codes := []string{all[0].Metadata().Code, all[1].Metadata().Code, all[2].Metadata().Code}
	want := []string{"a-rule", "b-rule", "c-rule"}
	for i, c := range codes {
		if c != want[i] {
			t.Errorf("All()[%d].Code = %q, want %q", i, c, want[i])
		}
	}
}

func TestRegistry_Codes(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockRule{code: "z-rule"})
	reg.Register(&mockRule{code: "a-rule"})

	codes := reg.Codes()
	if len(codes) != 2 {
		t.Fatalf("Codes() returned %d, want 2", len(codes))
	}
	if codes[0] != "a-rule" || codes[1] != "z-rule" {
		t.Errorf("Codes() = %v, want [a-rule, z-rule]", codes)
	}
}

func TestRegistry_EnabledByDefault(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockRule{code: "enabled-1", enabled: true})
	reg.Register(&mockRule{code: "disabled-1", enabled: false})
	reg.Register(&mockRule{code: "enabled-2", enabled: true})

	enabled := reg.EnabledByDefault()
	if len(enabled) != 2 {
		t.Fatalf("EnabledByDefault() returned %d, want 2", len(enabled))
	}

	for _, r := range enabled {
		if !r.Metadata().EnabledByDefault {
			t.Errorf("rule %q should be enabled by default", r.Metadata().Code)
		}
	}
}

func TestRegistry_ByCategory(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockRule{code: "sec-1", category: "security"})
	reg.Register(&mockRule{code: "perf-1", category: "performance"})
	reg.Register(&mockRule{code: "sec-2", category: "security"})

	secRules := reg.ByCategory("security")
	if len(secRules) != 2 {
		t.Fatalf("ByCategory(security) returned %d, want 2", len(secRules))
	}

	perfRules := reg.ByCategory("performance")
	if len(perfRules) != 1 {
		t.Fatalf("ByCategory(performance) returned %d, want 1", len(perfRules))
	}
}

func TestRegistry_BySeverity(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockRule{code: "err-1", severity: SeverityError})
	reg.Register(&mockRule{code: "warn-1", severity: SeverityWarning})
	reg.Register(&mockRule{code: "err-2", severity: SeverityError})

	errorRules := reg.BySeverity(SeverityError)
	if len(errorRules) != 2 {
		t.Fatalf("BySeverity(error) returned %d, want 2", len(errorRules))
	}
}

func TestRegistry_Experimental(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockRule{code: "stable", expmt: false})
	reg.Register(&mockRule{code: "experimental", expmt: true})

	exp := reg.Experimental()
	if len(exp) != 1 {
		t.Fatalf("Experimental() returned %d, want 1", len(exp))
	}
	if exp[0].Metadata().Code != "experimental" {
		t.Errorf("Experimental()[0].Code = %q, want %q", exp[0].Metadata().Code, "experimental")
	}
}
