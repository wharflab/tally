package buildkit

import (
	"slices"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

func TestRegistryHas22Rules(t *testing.T) {
	t.Parallel()
	if len(Registry) != 22 {
		t.Errorf("expected 22 BuildKit rules, got %d", len(Registry))
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"StageNameCasing", true},
		{"MaintainerDeprecated", true},
		{"UnknownRule", false},
	}

	for _, tt := range tests {
		info := Get(tt.name)
		if (info != nil) != tt.want {
			t.Errorf("Get(%q) exists = %v, want %v", tt.name, info != nil, tt.want)
		}
	}
}

func TestGetMetadata(t *testing.T) {
	t.Parallel()
	meta := GetMetadata("StageNameCasing")
	if meta == nil {
		t.Fatal("GetMetadata(StageNameCasing) returned nil")
	}
	snaps.MatchStandaloneJSON(t, meta)
}

func TestByCategory(t *testing.T) {
	t.Parallel()
	style := ByCategory("style")
	if len(style) < 4 {
		t.Errorf("expected at least 4 style rules, got %d", len(style))
	}

	security := ByCategory("security")
	if len(security) < 1 {
		t.Errorf("expected at least 1 security rule, got %d", len(security))
	}
}

func TestCategories(t *testing.T) {
	t.Parallel()
	cats := Categories()
	if len(cats) < 4 {
		t.Errorf("expected at least 4 categories, got %d", len(cats))
	}

	// Verify expected categories exist
	expected := map[string]bool{
		"style":         false,
		"correctness":   false,
		"best-practice": false,
		"security":      false,
	}

	for _, cat := range cats {
		if _, ok := expected[cat]; ok {
			expected[cat] = true
		}
	}

	for cat, found := range expected {
		if !found {
			t.Errorf("expected category %q not found", cat)
		}
	}
}

func TestAllRulesHaveDocURL(t *testing.T) {
	t.Parallel()
	// Count rules without DocURL (only InvalidBaseImagePlatform is expected)
	missingDocs := 0
	for name, info := range Registry {
		if info.DocURL == "" {
			missingDocs++
			// Only InvalidBaseImagePlatform should be missing docs
			if name != "InvalidBaseImagePlatform" {
				t.Errorf("rule %q is missing DocURL", name)
			}
		}
	}

	if missingDocs > 1 {
		t.Errorf("expected at most 1 rule without DocURL, got %d", missingDocs)
	}
}

func TestAll(t *testing.T) {
	t.Parallel()
	all := All()
	if len(all) != len(Registry) {
		t.Fatalf("len(All()) = %d, want %d", len(all), len(Registry))
	}

	// Basic sanity: a known rule should be present.
	if !slices.ContainsFunc(all, func(info RuleInfo) bool { return info.Name == "StageNameCasing" }) {
		t.Fatalf("All() missing StageNameCasing")
	}
}

func TestCaptured(t *testing.T) {
	t.Parallel()
	captured := Captured()
	if len(captured) != len(CapturedRuleNames) {
		t.Fatalf("len(Captured()) = %d, want %d", len(captured), len(CapturedRuleNames))
	}

	for i, name := range CapturedRuleNames {
		if captured[i].Name != name {
			t.Fatalf("Captured()[%d].Name = %q, want %q", i, captured[i].Name, name)
		}
	}
}
