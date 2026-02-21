package ruleconfig

import "testing"

func TestCanonicalizeRuleOptions(t *testing.T) {
	t.Parallel()

	t.Run("max-lines integer shorthand", func(t *testing.T) {
		t.Parallel()

		got := CanonicalizeRuleOptions("tally/max-lines", 120)
		opts, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("got %T, want map[string]any", got)
		}
		if opts["max"] != 120 {
			t.Fatalf("opts[max] = %v, want 120", opts["max"])
		}
	})

	t.Run("max-lines map stays unchanged", func(t *testing.T) {
		t.Parallel()

		input := map[string]any{"max": 80}
		got := CanonicalizeRuleOptions("tally/max-lines", input)
		gotMap, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("got %T, want map[string]any", got)
		}
		if gotMap["max"] != 80 {
			t.Fatalf("got map max = %v, want 80", gotMap["max"])
		}
	})

	t.Run("max-lines string integer shorthand from env var", func(t *testing.T) {
		t.Parallel()

		got := CanonicalizeRuleOptions("tally/max-lines", "100")
		opts, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("got %T, want map[string]any", got)
		}
		if opts["max"] != "100" {
			t.Fatalf("opts[max] = %v, want \"100\"", opts["max"])
		}
	})

	t.Run("max-lines non-numeric string is not shorthand", func(t *testing.T) {
		t.Parallel()

		input := "abc"
		got := CanonicalizeRuleOptions("tally/max-lines", input)
		if got != input {
			t.Fatalf("expected non-numeric string unchanged, got %v", got)
		}
	})

	t.Run("max-lines float is not shorthand", func(t *testing.T) {
		t.Parallel()

		input := 120.0
		got := CanonicalizeRuleOptions("tally/max-lines", input)
		if got != input {
			t.Fatalf("expected float input unchanged, got %v", got)
		}
	})

	t.Run("newline mode shorthand", func(t *testing.T) {
		t.Parallel()

		got := CanonicalizeRuleOptions("tally/newline-between-instructions", "always")
		opts, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("got %T, want map[string]any", got)
		}
		if opts["mode"] != "always" {
			t.Fatalf("opts[mode] = %v, want always", opts["mode"])
		}
	})

	t.Run("unsupported rule unchanged", func(t *testing.T) {
		t.Parallel()

		input := "warning"
		got := CanonicalizeRuleOptions("tally/unknown", input)
		if got != input {
			t.Fatalf("expected unsupported rule unchanged, got %v", got)
		}
	})
}

func TestCanonicalizeRulesMap(t *testing.T) {
	t.Parallel()

	rules := map[string]any{
		"include": "tally/*",
		"tally": map[string]any{
			"max-lines":                    150,
			"newline-between-instructions": "grouped",
			"other-rule":                   map[string]any{"severity": "warning"},
		},
	}

	CanonicalizeRulesMap(rules)

	tallyRules, ok := rules["tally"].(map[string]any)
	if !ok {
		t.Fatalf("rules[tally] type = %T, want map[string]any", rules["tally"])
	}

	maxLines, ok := tallyRules["max-lines"].(map[string]any)
	if !ok {
		t.Fatalf("tally.max-lines type = %T, want map[string]any", tallyRules["max-lines"])
	}
	if maxLines["max"] != 150 {
		t.Fatalf("tally.max-lines.max = %v, want 150", maxLines["max"])
	}

	newline, ok := tallyRules["newline-between-instructions"].(map[string]any)
	if !ok {
		t.Fatalf(
			"tally.newline-between-instructions type = %T, want map[string]any",
			tallyRules["newline-between-instructions"],
		)
	}
	if newline["mode"] != "grouped" {
		t.Fatalf(
			"tally.newline-between-instructions.mode = %v, want grouped",
			newline["mode"],
		)
	}

	other, ok := tallyRules["other-rule"].(map[string]any)
	if !ok {
		t.Fatalf("tally.other-rule type = %T, want map[string]any", tallyRules["other-rule"])
	}
	if other["severity"] != "warning" {
		t.Fatalf("tally.other-rule.severity = %v, want warning", other["severity"])
	}
}
