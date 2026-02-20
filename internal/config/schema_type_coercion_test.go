package config

import "testing"

func TestDecodeConfig_CoercesStringTypesUsingSchema(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"rules": map[string]any{
			"include": "tally/*,buildkit/*",
			"max-lines": map[string]any{
				"max":           "500",
				"skip-comments": "false",
			},
		},
		"output": map[string]any{
			"show-source": "false",
		},
		"ai": map[string]any{
			"enabled":         "true",
			"max-input-bytes": "1234",
			"command":         `["acp-agent","--model","foo"]`,
		},
	}

	cfg, err := decodeConfig(raw)
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	if got := cfg.Output.ShowSource; got != false {
		t.Fatalf("cfg.Output.ShowSource = %v, want false", got)
	}

	if got := cfg.AI.Enabled; got != true {
		t.Fatalf("cfg.AI.Enabled = %v, want true", got)
	}

	if got := cfg.AI.MaxInputBytes; got != 1234 {
		t.Fatalf("cfg.AI.MaxInputBytes = %d, want 1234", got)
	}

	if got := cfg.AI.Command; len(got) != 3 || got[0] != "acp-agent" || got[1] != "--model" || got[2] != "foo" {
		t.Fatalf("cfg.AI.Command = %#v, want [acp-agent --model foo]", got)
	}

	if got := cfg.Rules.Include; len(got) != 2 || got[0] != "tally/*" || got[1] != "buildkit/*" {
		t.Fatalf("cfg.Rules.Include = %#v, want [tally/* buildkit/*]", got)
	}

	opts := cfg.Rules.GetOptions("tally/max-lines")
	if opts == nil {
		t.Fatal("cfg.Rules.GetOptions(tally/max-lines) = nil, want map")
	}

	maxAny, ok := opts["max"]
	if !ok {
		t.Fatal("max-lines opts missing \"max\"")
	}
	var maxLines int64
	switch v := maxAny.(type) {
	case int:
		maxLines = int64(v)
	case int64:
		maxLines = v
	default:
		t.Fatalf("max-lines opts[\"max\"] type = %T, want int/int64", maxAny)
	}
	if maxLines != 500 {
		t.Fatalf("max-lines opts[\"max\"] = %d, want 500", maxLines)
	}

	skipCommentsAny, ok := opts["skip-comments"]
	if !ok {
		t.Fatal("max-lines opts missing \"skip-comments\"")
	}
	skipComments, ok := skipCommentsAny.(bool)
	if !ok {
		t.Fatalf("max-lines opts[\"skip-comments\"] type = %T, want bool", skipCommentsAny)
	}
	if skipComments != false {
		t.Fatalf("max-lines opts[\"skip-comments\"] = %v, want false", skipComments)
	}
}

func TestDecodeConfig_PreservesGenericRuleConfigs(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"rules": map[string]any{
			"hadolint": map[string]any{
				"DL4000": map[string]any{
					"severity": "warning",
				},
			},
		},
	}

	cfg, err := decodeConfig(raw)
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	if sev := cfg.Rules.GetSeverity("hadolint/DL4000"); sev != "warning" {
		t.Fatalf("cfg.Rules.GetSeverity(hadolint/DL4000) = %q, want warning", sev)
	}
}
