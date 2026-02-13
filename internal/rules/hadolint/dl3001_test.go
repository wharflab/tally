package hadolint

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3001Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3001Rule().Metadata())
}

func TestDL3001Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		// From Hadolint spec: ruleCatches "DL3001" "RUN top"
		{
			name: "invalid cmd top",
			dockerfile: `FROM ubuntu:22.04
RUN top
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3001",
		},
		// From Hadolint spec: ruleCatchesNot "DL3001" "RUN apt-get install ssh"
		{
			name: "install ssh is not a violation",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install ssh
`,
			wantCount: 0,
		},
		// Additional test cases for all invalid commands
		{
			name: "free command",
			dockerfile: `FROM ubuntu:22.04
RUN free -m
`,
			wantCount: 1,
		},
		{
			name: "kill command",
			dockerfile: `FROM ubuntu:22.04
RUN kill -9 1234
`,
			wantCount: 1,
		},
		{
			name: "mount command",
			dockerfile: `FROM ubuntu:22.04
RUN mount /dev/sda1 /mnt
`,
			wantCount: 1,
		},
		{
			name: "ps command",
			dockerfile: `FROM ubuntu:22.04
RUN ps aux
`,
			wantCount: 1,
		},
		{
			name: "service command",
			dockerfile: `FROM ubuntu:22.04
RUN service nginx start
`,
			wantCount: 1,
		},
		{
			name: "shutdown command",
			dockerfile: `FROM ubuntu:22.04
RUN shutdown -h now
`,
			wantCount: 1,
		},
		{
			name: "ssh command",
			dockerfile: `FROM ubuntu:22.04
RUN ssh user@host
`,
			wantCount: 1,
		},
		{
			name: "vim command",
			dockerfile: `FROM ubuntu:22.04
RUN vim /etc/config
`,
			wantCount: 1,
		},
		// Valid commands - should NOT trigger
		{
			name: "normal apt-get command",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
`,
			wantCount: 0,
		},
		{
			name: "echo command",
			dockerfile: `FROM ubuntu:22.04
RUN echo "hello"
`,
			wantCount: 0,
		},
		// Edge cases
		{
			name: "invalid command as argument",
			dockerfile: `FROM ubuntu:22.04
RUN echo top
`,
			wantCount: 0,
		},
		{
			name: "invalid command in pipeline",
			dockerfile: `FROM ubuntu:22.04
RUN echo "data" | ssh user@host
`,
			wantCount: 1,
		},
		{
			name: "invalid command with &&",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && top
`,
			wantCount: 1,
		},
		{
			name: "multiple invalid commands",
			dockerfile: `FROM ubuntu:22.04
RUN top && ps aux
`,
			wantCount: 1, // one violation per RUN instruction
		},
		{
			name: "repeated invalid command is deduplicated in message",
			dockerfile: `FROM ubuntu:22.04
RUN top && ps aux && top
`,
			wantCount: 1,
		},
		{
			name: "multi-stage no violation",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN apt-get update

FROM alpine:3.18
RUN apk add curl
`,
			wantCount: 0,
		},
		{
			name: "exec form with invalid command",
			dockerfile: `FROM ubuntu:22.04
RUN ["top"]
`,
			wantCount: 1,
		},
		{
			name: "exec form with valid command",
			dockerfile: `FROM ubuntu:22.04
RUN ["apt-get", "update"]
`,
			wantCount: 0,
		},
		{
			name: "invalid command via env wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN env top
`,
			wantCount: 1,
		},
		{
			name: "invalid command via sh -c wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN sh -c 'top'
`,
			wantCount: 1,
		},
		{
			name: "install package named like invalid command",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y vim
`,
			wantCount: 0,
		},
		{
			name: "onbuild with invalid command",
			dockerfile: `FROM ubuntu:22.04
ONBUILD RUN top
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3001",
		},
		{
			name: "onbuild with valid command",
			dockerfile: `FROM ubuntu:22.04
ONBUILD RUN apt-get install ssh
`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL3001Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

func TestDL3001Rule_SuggestedFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantFix    bool
		wantText   string
	}{
		{
			name: "single invalid command gets fix",
			dockerfile: `FROM ubuntu:22.04
RUN top
`,
			wantFix:  true,
			wantText: "# [commented out by tally - command has no purpose in a container]: RUN top",
		},
		{
			name: "all invalid commands on one line get fix",
			dockerfile: `FROM ubuntu:22.04
RUN top && ps
`,
			wantFix:  true,
			wantText: "# [commented out by tally - command has no purpose in a container]: RUN top && ps",
		},
		{
			name: "mixed valid and invalid commands get no fix",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && top
`,
			wantFix: false,
		},
		{
			name: "multi-line RUN gets no fix",
			dockerfile: `FROM ubuntu:22.04
RUN top \
    && ps
`,
			wantFix: false,
		},
		{
			name: "fix has suggestion safety level",
			dockerfile: `FROM ubuntu:22.04
RUN ssh user@host
`,
			wantFix: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3001Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			if tt.wantFix {
				if v.SuggestedFix == nil {
					t.Fatal("expected SuggestedFix, got nil")
				}
				if v.SuggestedFix.Safety != rules.FixSuggestion {
					t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
				}
				if tt.wantText != "" && len(v.SuggestedFix.Edits) > 0 {
					if v.SuggestedFix.Edits[0].NewText != tt.wantText {
						t.Errorf("NewText = %q, want %q",
							v.SuggestedFix.Edits[0].NewText, tt.wantText)
					}
				}
			} else if v.SuggestedFix != nil {
				t.Errorf("expected no SuggestedFix, got %+v", v.SuggestedFix)
			}
		})
	}
}

func TestDL3001Rule_DeduplicatesRepeatedCommands(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04
RUN top && ps aux && top
`)
	r := NewDL3001Rule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	// "top" appears twice in the RUN but should be listed only once in the message.
	msg := violations[0].Message
	if !strings.Contains(msg, "command ps, top") {
		t.Errorf("expected deduplicated 'ps, top' in message, got: %s", msg)
	}
}

func TestDL3001Rule_CheckWithConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		config     DL3001Config
		wantCount  int
	}{
		{
			name: "custom list catches custom command",
			dockerfile: `FROM ubuntu:22.04
RUN htop
`,
			config:    DL3001Config{InvalidCommands: []string{"htop", "nano"}},
			wantCount: 1,
		},
		{
			name: "custom list does not catch default command",
			dockerfile: `FROM ubuntu:22.04
RUN top
`,
			config:    DL3001Config{InvalidCommands: []string{"htop", "nano"}},
			wantCount: 0,
		},
		{
			name: "custom list with subset of defaults",
			dockerfile: `FROM ubuntu:22.04
RUN vim /etc/config
`,
			config:    DL3001Config{InvalidCommands: []string{"ssh", "top"}},
			wantCount: 0,
		},
		{
			name: "custom list extends with new commands",
			dockerfile: `FROM ubuntu:22.04
RUN nano /etc/config
`,
			config:    DL3001Config{InvalidCommands: append(defaultInvalidCommands, "nano", "htop")},
			wantCount: 1,
		},
		{
			name: "empty list disables the rule",
			dockerfile: `FROM ubuntu:22.04
RUN top
`,
			config:    DL3001Config{InvalidCommands: []string{}},
			wantCount: 0,
		},
		{
			name: "nil config uses defaults",
			dockerfile: `FROM ubuntu:22.04
RUN top
`,
			config:    DefaultDL3001Config(),
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.dockerfile, tt.config)

			r := NewDL3001Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}
		})
	}
}

func TestDL3001Rule_CheckWithMapConfig(t *testing.T) {
	t.Parallel()
	// Test that map[string]any config (as it comes from TOML parsing) works correctly.
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM ubuntu:22.04
RUN nano /etc/hosts
`, map[string]any{
		"invalid-commands": []any{"nano", "htop"},
	})

	r := NewDL3001Rule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Errorf("got %d violations, want 1", len(violations))
	}
}

func TestDL3001Rule_ValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewDL3001Rule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"invalid-commands": []any{"top", "vim"}},
			wantErr: false,
		},
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "empty object is valid",
			config:  map[string]any{},
			wantErr: false,
		},
		{
			name:    "unknown property is invalid",
			config:  map[string]any{"unknown": "value"},
			wantErr: true,
		},
		{
			name:    "wrong type for invalid-commands",
			config:  map[string]any{"invalid-commands": "not-an-array"},
			wantErr: true,
		},
		{
			name:    "empty string in array is invalid",
			config:  map[string]any{"invalid-commands": []any{""}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := r.ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDL3001Rule_Schema(t *testing.T) {
	t.Parallel()
	r := NewDL3001Rule()
	schema := r.Schema()

	if schema == nil {
		t.Fatal("Schema() returned nil")
	}
	if schema["type"] != "object" {
		t.Errorf("Schema type = %v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema properties is not a map")
	}
	if _, ok := props["invalid-commands"]; !ok {
		t.Error("Schema missing invalid-commands property")
	}
}
