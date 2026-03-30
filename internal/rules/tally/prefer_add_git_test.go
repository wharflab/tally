package tally

import (
	"context"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferAddGitRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferAddGitRule().Metadata())
}

func TestPreferAddGitRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferAddGitRule(), []testutil.RuleTestCase{
		{
			Name: "pure clone",
			Content: `FROM alpine
RUN git clone https://github.com/NVIDIA/apex
`,
			WantViolations: 1,
		},
		{
			Name: "extract from middle of chain",
			Content: `FROM alpine
RUN echo foo && git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo zoo
`,
			WantViolations: 1,
		},
		{
			Name: "gitlab http ref variable",
			Content: `FROM alpine
RUN git clone https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git -b ${GHC_WASM_META_COMMIT}
`,
			WantViolations: 1,
		},
		{
			Name: "branch ref with variable",
			Content: `FROM alpine
RUN git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}
`,
			WantViolations: 1,
		},
		{
			Name: "exec form ignored",
			Content: `FROM alpine
RUN ["git", "clone", "https://github.com/NVIDIA/apex"]
`,
			WantViolations: 0,
		},
		{
			Name: "non git run ignored",
			Content: `FROM alpine
RUN echo hello
`,
			WantViolations: 0,
		},
	})
}

func TestPreferAddGitRule_CheckWithFixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantFixed   string
		wantHasFix  bool
		wantContain string
	}{
		{
			name: "pure clone becomes add",
			content: `FROM alpine
RUN git clone https://github.com/NVIDIA/apex
`,
			wantHasFix: true,
			wantFixed: `FROM alpine
ADD --link https://github.com/NVIDIA/apex.git /apex
`,
		},
		{
			name: "middle extraction keeps surrounding commands",
			content: `FROM alpine
RUN echo foo && git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo zoo
`,
			wantHasFix: true,
			wantFixed: "FROM alpine\n" +
				"RUN echo foo\n" +
				"ADD --link --checksum=0123456789abcdef0123456789abcdef01234567 " +
				"https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567 /apex\n" +
				"RUN cd /apex && echo zoo\n",
		},
		{
			name: "abbreviated checkout commit reports without fix",
			content: `FROM alpine
RUN git clone https://github.com/NVIDIA/apex && cd apex && git checkout aa756ce
`,
			wantHasFix: false,
		},
		{
			name: "variable refs stay unescaped",
			content: `FROM alpine
RUN git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}
`,
			wantHasFix:  true,
			wantContain: `ADD --link https://github.com/aws/aws-ofi-nccl.git?ref=v${BRANCH_OFI}`,
		},
		{
			name: "gitlab http remotes keep ref query",
			content: `FROM alpine
RUN git clone https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git -b ${GHC_WASM_META_COMMIT}
`,
			wantHasFix:  true,
			wantContain: `ADD --link https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git?ref=${GHC_WASM_META_COMMIT}`,
		},
		{
			name: "git commands keep git dir via add flag",
			content: `FROM alpine
RUN git clone https://github.com/NVIDIA/apex && cd apex && git describe --tags
`,
			wantHasFix:  true,
			wantContain: `ADD --link --keep-git-dir=true https://github.com/NVIDIA/apex.git /apex`,
		},
		{
			name: "network flag reports without fix",
			content: `FROM alpine
RUN --network=host git clone https://github.com/NVIDIA/apex
`,
			wantHasFix: false,
		},
		{
			name: "mount reports without fix",
			content: `FROM alpine
RUN --mount=type=ssh git clone git@github.com:NVIDIA/apex.git
`,
			wantHasFix: false,
		},
	}

	rule := NewPreferAddGitRule()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := rule.Check(input)
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}

			fix := violations[0].SuggestedFix
			if (fix != nil) != tt.wantHasFix {
				t.Fatalf("has fix = %v, want %v", fix != nil, tt.wantHasFix)
			}
			if !tt.wantHasFix {
				return
			}
			if fix.Safety != rules.FixSuggestion {
				t.Fatalf("fix safety = %v, want %v", fix.Safety, rules.FixSuggestion)
			}

			result, err := (&fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}).Apply(
				context.Background(),
				violations,
				map[string][]byte{"Dockerfile": []byte(tt.content)},
			)
			if err != nil {
				t.Fatalf("apply fixes: %v", err)
			}

			got := string(result.Changes["Dockerfile"].ModifiedContent)
			if tt.wantFixed != "" && got != tt.wantFixed {
				t.Fatalf("fixed content =\n%s\nwant:\n%s", got, tt.wantFixed)
			}
			if tt.wantContain != "" && !strings.Contains(got, tt.wantContain) {
				t.Fatalf("fixed content = %q, want substring %q", got, tt.wantContain)
			}
		})
	}
}
