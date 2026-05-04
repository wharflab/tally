package ruledeprecation

import (
	jsonv2 "encoding/json/v2"
	"maps"
	"os"
	"slices"
	"testing"
)

func TestLookupSupersededAlias(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"DL3000": "buildkit/WorkdirRelativePath",
		"DL3012": "buildkit/MultipleInstructionsDisallowed",
		"DL3024": "buildkit/DuplicateStageName",
		"DL3025": "buildkit/JSONArgsRecommended",
		"DL3029": "buildkit/FromPlatformFlagConstDisallowed",
		"DL3044": "buildkit/UndefinedVar",
		"DL3063": "buildkit/ReservedStageName",
		"DL4000": "buildkit/MaintainerDeprecated",
		"DL4003": "buildkit/MultipleInstructionsDisallowed",
		"DL4004": "buildkit/MultipleInstructionsDisallowed",
	}
	for _, code := range slices.Sorted(maps.Keys(tests)) {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			entry, ok := Lookup(code)
			if !ok {
				t.Fatalf("expected %s to be known", code)
			}
			if entry.Code != "hadolint/"+code {
				t.Fatalf("Code = %q, want hadolint/%s", entry.Code, code)
			}
			if entry.Kind != KindSuperseded {
				t.Fatalf("Kind = %q, want %q", entry.Kind, KindSuperseded)
			}
			if entry.Replacement != tests[code] {
				t.Fatalf("Replacement = %q, want %q", entry.Replacement, tests[code])
			}
			namespacedEntry, ok := Lookup("hadolint/" + code)
			if !ok {
				t.Fatalf("expected hadolint/%s to be known", code)
			}
			if namespacedEntry.Code != entry.Code || namespacedEntry.Kind != entry.Kind ||
				namespacedEntry.Replacement != entry.Replacement {
				t.Fatalf("Lookup(hadolint/%s) = %#v, want %#v", code, namespacedEntry, entry)
			}
		})
	}
}

func TestCollectorDeduplicatesAliases(t *testing.T) {
	t.Parallel()

	collector := NewCollector()
	collector.AddCode("DL3063")
	collector.AddCode("hadolint/DL3063")

	notices := collector.Notices()
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want 1", len(notices))
	}
	if notices[0].Entry.Code != "hadolint/DL3063" {
		t.Fatalf("notice code = %q, want hadolint/DL3063", notices[0].Entry.Code)
	}
}

func TestBuildKitCoveredHadolintRulesAreDeprecated(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../rules/hadolint-status.json")
	if err != nil {
		t.Fatalf("read hadolint status: %v", err)
	}

	var status struct {
		Rules map[string]struct {
			Status       string `json:"status"`
			BuildKitRule string `json:"buildkit_rule"`
		} `json:"rules"`
	}
	if err := jsonv2.Unmarshal(data, &status); err != nil {
		t.Fatalf("decode hadolint status: %v", err)
	}

	for _, code := range slices.Sorted(maps.Keys(status.Rules)) {
		ruleStatus := status.Rules[code]
		if ruleStatus.Status != "covered_by_buildkit" {
			continue
		}

		entry, ok := Lookup(code)
		if !ok {
			t.Fatalf("%s is covered_by_buildkit but has no deprecation entry", code)
		}
		if entry.Code != "hadolint/"+code {
			t.Fatalf("%s deprecation code = %q, want hadolint/%s", code, entry.Code, code)
		}
		if entry.Kind != KindSuperseded {
			t.Fatalf("%s deprecation kind = %q, want %q", code, entry.Kind, KindSuperseded)
		}
		wantReplacement := "buildkit/" + ruleStatus.BuildKitRule
		if entry.Replacement != wantReplacement {
			t.Fatalf("%s replacement = %q, want %q", code, entry.Replacement, wantReplacement)
		}
	}
}
