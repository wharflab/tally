package fixes

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestFindDockerfileInlineCommentStart(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantIdx   int
		wantFound bool
	}{
		{
			name:      "no-comment",
			line:      "CMD echo hi",
			wantIdx:   len("CMD echo hi"),
			wantFound: false,
		},
		{
			name:      "inline-comment",
			line:      "CMD echo hi # comment",
			wantIdx:   len("CMD echo hi "),
			wantFound: true,
		},
		{
			name:      "hash-not-comment",
			line:      "CMD echo hi#not-a-comment",
			wantIdx:   len("CMD echo hi#not-a-comment"),
			wantFound: false,
		},
		{
			name:      "hash-in-single-quotes",
			line:      "CMD echo '# not a comment'",
			wantIdx:   len("CMD echo '# not a comment'"),
			wantFound: false,
		},
		{
			name:      "hash-in-double-quotes",
			line:      `CMD echo "# not a comment"`,
			wantIdx:   len(`CMD echo "# not a comment"`),
			wantFound: false,
		},
		{
			name:      "escaped-quote-in-double-quotes",
			line:      `CMD echo "a\\\"b" # comment`,
			wantIdx:   len(`CMD echo "a\\\"b" `),
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIdx, gotFound := findDockerfileInlineCommentStart([]byte(tt.line))
			if gotFound != tt.wantFound {
				t.Fatalf("found = %v, want %v (idx=%d)", gotFound, tt.wantFound, gotIdx)
			}
			if gotIdx != tt.wantIdx {
				t.Fatalf("idx = %d, want %d", gotIdx, tt.wantIdx)
			}
		})
	}
}

func TestEnrichJSONArgsRecommendedFix_SkipsComplexShell(t *testing.T) {
	df := "FROM alpine\nCMD echo *.txt\n"
	source := []byte(df)

	v := rules.NewViolation(
		rules.NewLineLocation("Dockerfile", 2),
		"buildkit/JSONArgsRecommended",
		"msg",
		rules.SeverityInfo,
	)
	// Make the location look like an instruction location (line range), not a point.
	v.Location = rules.NewRangeLocation("Dockerfile", 2, 0, 2, len("CMD echo *.txt"))

	enrichJSONArgsRecommendedFix(&v, source)
	if v.SuggestedFix != nil {
		t.Fatalf("expected no fix for globbing shell command, got %+v", v.SuggestedFix)
	}
}

func TestEnrichJSONArgsRecommendedFix_EarlyReturns(t *testing.T) {
	t.Run("line-number-missing", func(t *testing.T) {
		source := []byte("FROM alpine\nCMD echo hi\n")
		v := rules.NewViolation(rules.NewFileLocation("Dockerfile"), "buildkit/JSONArgsRecommended", "msg", rules.SeverityInfo)
		v.Location = rules.NewRangeLocation("Dockerfile", 0, 0, 0, 0)

		enrichJSONArgsRecommendedFix(&v, source)
		if v.SuggestedFix != nil {
			t.Fatalf("expected no fix, got %+v", v.SuggestedFix)
		}
	})

	t.Run("line-out-of-bounds", func(t *testing.T) {
		source := []byte("FROM alpine\nCMD echo hi\n")
		v := rules.NewViolation(rules.NewFileLocation("Dockerfile"), "buildkit/JSONArgsRecommended", "msg", rules.SeverityInfo)
		v.Location = rules.NewRangeLocation("Dockerfile", 99, 0, 99, 0)

		enrichJSONArgsRecommendedFix(&v, source)
		if v.SuggestedFix != nil {
			t.Fatalf("expected no fix, got %+v", v.SuggestedFix)
		}
	})

	t.Run("not-a-cmd-or-entrypoint", func(t *testing.T) {
		source := []byte("FROM alpine\nRUN echo hi\n")
		v := rules.NewViolation(rules.NewFileLocation("Dockerfile"), "buildkit/JSONArgsRecommended", "msg", rules.SeverityInfo)
		v.Location = rules.NewRangeLocation("Dockerfile", 2, 0, 2, len("RUN echo hi"))

		enrichJSONArgsRecommendedFix(&v, source)
		if v.SuggestedFix != nil {
			t.Fatalf("expected no fix, got %+v", v.SuggestedFix)
		}
	})

	t.Run("missing-args", func(t *testing.T) {
		source := []byte("FROM alpine\nCMD\n")
		v := rules.NewViolation(rules.NewFileLocation("Dockerfile"), "buildkit/JSONArgsRecommended", "msg", rules.SeverityInfo)
		v.Location = rules.NewRangeLocation("Dockerfile", 2, 0, 2, len("CMD"))

		enrichJSONArgsRecommendedFix(&v, source)
		if v.SuggestedFix != nil {
			t.Fatalf("expected no fix, got %+v", v.SuggestedFix)
		}
	})
}

func TestEnrichJSONArgsRecommendedFix_Success(t *testing.T) {
	source := []byte("FROM alpine\nCMD echo hi # comment\n")
	v := rules.NewViolation(rules.NewFileLocation("Dockerfile"), "buildkit/JSONArgsRecommended", "msg", rules.SeverityInfo)
	v.Location = rules.NewRangeLocation("Dockerfile", 2, 0, 2, len("CMD echo hi # comment"))

	enrichJSONArgsRecommendedFix(&v, source)
	if v.SuggestedFix == nil {
		t.Fatalf("expected fix, got nil")
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}
	if got := v.SuggestedFix.Edits[0].NewText; got != "[\"echo\",\"hi\"] " {
		t.Fatalf("NewText = %q, want %q", got, "[\"echo\",\"hi\"] ")
	}
}
