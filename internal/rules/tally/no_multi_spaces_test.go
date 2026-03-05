package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoMultiSpacesMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoMultiSpacesRule().Metadata())
}

func TestNoMultiSpacesSchema(t *testing.T) {
	t.Parallel()
	r := NewNoMultiSpacesRule()
	if r.Schema() == nil {
		t.Fatal("Schema() returned nil")
	}
	if r.DefaultConfig() != nil {
		t.Errorf("DefaultConfig() = %v, want nil", r.DefaultConfig())
	}
}

func TestNoMultiSpacesValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewNoMultiSpacesRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil config", config: nil, wantErr: false},
		{name: "empty object", config: map[string]any{}, wantErr: false},
		{name: "severity only", config: map[string]any{"severity": "style"}, wantErr: false},
		{name: "extra key", config: map[string]any{"unknown": true}, wantErr: true},
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

func TestNoMultiSpacesCheck(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoMultiSpacesRule(), []testutil.RuleTestCase{
		// === Clean files ===
		{
			Name:           "clean file - single spaces",
			Content:        "FROM alpine:3.20\nRUN echo hello\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "empty file",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},
		{
			Name:           "tabs in content are not flagged",
			Content:        "FROM alpine:3.20\nRUN\t\techo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "leading indentation only",
			Content:        "FROM alpine:3.20\n    RUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "tab indentation only",
			Content:        "FROM alpine:3.20\n\t\tRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "comment lines are skipped",
			Content:        "# this  has  double  spaces\nFROM alpine:3.20\n",
			WantViolations: 0,
		},
		{
			Name:           "blank lines are skipped",
			Content:        "FROM alpine:3.20\n\n\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "heredoc body is skipped",
			Content:        "FROM alpine:3.20\nRUN <<EOF\necho   hello    world\nEOF\n",
			WantViolations: 0,
		},
		{
			Name:           "heredoc with dash body is skipped",
			Content:        "FROM alpine:3.20\nRUN <<-EOF\n\techo   hello    world\nEOF\n",
			WantViolations: 0,
		},
		{
			Name:           "continuation line indentation is not flagged",
			Content:        "FROM alpine:3.20\nRUN echo hello \\\n    && echo world\n",
			WantViolations: 0,
		},
		{
			Name:           "single space everywhere",
			Content:        "FROM alpine:3.20\nRUN apk add --no-cache curl\nCOPY . /app\nCMD [\"sh\"]\n",
			WantViolations: 0,
		},

		// === Violations ===
		{
			Name:           "double space after FROM",
			Content:        "FROM  alpine:3.20\n",
			WantViolations: 1,
			WantMessages:   []string{"multiple consecutive spaces (1 extra)"},
		},
		{
			Name:           "triple space after RUN",
			Content:        "FROM alpine:3.20\nRUN   echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"multiple consecutive spaces (2 extra)"},
		},
		{
			Name:           "multiple runs on same line produces one violation",
			Content:        "FROM alpine:3.20\nRUN echo  hello   world\n",
			WantViolations: 1,
			WantMessages:   []string{"multiple consecutive spaces (3 extra)"},
		},
		{
			Name:           "violations on multiple lines",
			Content:        "FROM  alpine:3.20\nRUN  echo hello\nCOPY  . /app\n",
			WantViolations: 3,
		},
		{
			Name:           "indentation plus content violation",
			Content:        "FROM alpine:3.20\n    RUN  echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"multiple consecutive spaces (1 extra)"},
		},
		{
			Name:           "continuation line with multi-spaces in content",
			Content:        "FROM alpine:3.20\nRUN echo  hello \\\n    &&  echo world\n",
			WantViolations: 2,
		},
		{
			Name:           "heredoc instruction line still checked",
			Content:        "FROM alpine:3.20\nRUN  <<EOF\necho hello\nEOF\n",
			WantViolations: 1,
		},
		{
			Name:           "LABEL with multiple spaces",
			Content:        "FROM alpine:3.20\nLABEL maintainer=\"foo\"  version=\"1.0\"\n",
			WantViolations: 1,
		},
		{
			Name:           "ENV with multiple spaces",
			Content:        "FROM alpine:3.20\nENV FOO=bar  BAZ=qux\n",
			WantViolations: 1,
		},
		{
			Name:           "COPY with extra spaces - one violation per line",
			Content:        "FROM alpine:3.20\nCOPY  .  /app\n",
			WantViolations: 1,
			WantMessages:   []string{"multiple consecutive spaces (2 extra)"},
		},
		{
			Name:           "whitespace-only line is skipped",
			Content:        "FROM alpine:3.20\n   \nRUN echo hello\n",
			WantViolations: 0,
		},

		// === Quoted strings ===
		{
			Name:           "double-quoted spaces are skipped",
			Content:        "FROM alpine:3.20\nRUN echo \"hello    world\"\n",
			WantViolations: 0,
		},
		{
			Name:           "single-quoted spaces are skipped",
			Content:        "FROM alpine:3.20\nRUN echo 'hello    world'\n",
			WantViolations: 0,
		},
		{
			Name:           "spaces outside quotes still flagged",
			Content:        "FROM alpine:3.20\nRUN  echo \"hello    world\"\n",
			WantViolations: 1,
			WantMessages:   []string{"multiple consecutive spaces (1 extra)"},
		},
		{
			Name:           "StrictHostKeyChecking pattern preserved",
			Content:        "FROM alpine:3.20\nRUN echo \"    StrictHostKeyChecking no\" >> /etc/ssh/config\n",
			WantViolations: 0,
		},
	})
}

func TestNoMultiSpacesCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewNoMultiSpacesRule()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "single run of double spaces",
			content: "FROM  alpine:3.20\n",
			want:    "FROM alpine:3.20\n",
		},
		{
			name:    "single run of five spaces",
			content: "FROM     alpine:3.20\n",
			want:    "FROM alpine:3.20\n",
		},
		{
			name:    "two runs on one line",
			content: "FROM alpine:3.20\nRUN echo  hello   world\n",
			want:    "FROM alpine:3.20\nRUN echo hello world\n",
		},
		{
			name:    "multiple lines",
			content: "FROM  alpine:3.20\nRUN  echo hello\nCOPY  . /app\n",
			want:    "FROM alpine:3.20\nRUN echo hello\nCOPY . /app\n",
		},
		{
			name:    "heredoc body not fixed",
			content: "FROM alpine:3.20\nRUN <<EOF\necho   hello\nEOF\n",
			want:    "FROM alpine:3.20\nRUN <<EOF\necho   hello\nEOF\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := r.Check(input)

			got := []byte(tt.content)
			for _, v := range violations {
				if v.SuggestedFix == nil {
					t.Fatal("violation has no SuggestedFix")
				}
				if v.SuggestedFix.Safety != rules.FixSafe {
					t.Errorf("fix safety = %v, want FixSafe", v.SuggestedFix.Safety)
				}
				for i := len(v.SuggestedFix.Edits) - 1; i >= 0; i-- {
					got = fixpkg.ApplyEdit(got, v.SuggestedFix.Edits[i])
				}
			}

			if string(got) != tt.want {
				t.Errorf("after fix:\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}
