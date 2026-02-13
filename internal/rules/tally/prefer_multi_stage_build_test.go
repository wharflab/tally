package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/tinovyatkin/tally/internal/ai/autofixdata"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestPreferMultiStageBuildRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferMultiStageBuildRule().Metadata())
}

func TestPreferMultiStageBuildRule_SingleStageBuildStep_Triggers(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o /out/app ./cmd/app
CMD ["app"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferMultiStageBuildRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.RuleCode != "tally/prefer-multi-stage-build" {
		t.Fatalf("unexpected rule code: %q", v.RuleCode)
	}
	if v.SuggestedFix == nil {
		t.Fatalf("expected SuggestedFix")
	}
	if v.SuggestedFix.Safety != rules.FixUnsafe {
		t.Fatalf("expected FixUnsafe safety, got %v", v.SuggestedFix.Safety)
	}
	if !v.SuggestedFix.NeedsResolve {
		t.Fatalf("expected NeedsResolve=true")
	}
	if v.SuggestedFix.ResolverID != autofixdata.ResolverID {
		t.Fatalf("unexpected ResolverID: %q", v.SuggestedFix.ResolverID)
	}
	data, ok := v.SuggestedFix.ResolverData.(*autofixdata.MultiStageResolveData)
	if !ok || data == nil {
		t.Fatalf("expected MultiStageResolveData resolver data, got %T", v.SuggestedFix.ResolverData)
	}
	if data.Score < 4 {
		t.Fatalf("expected score >= 4, got %d", data.Score)
	}
	foundBuild := false
	for _, s := range data.Signals {
		if s.Kind == autofixdata.SignalKindBuildStep {
			foundBuild = true
			break
		}
	}
	if !foundBuild {
		t.Fatalf("expected a build_step signal, got %+v", data.Signals)
	}
}

func TestPreferMultiStageBuildRule_MultiStage_NoViolation(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /out/app ./cmd/app

FROM alpine:3.20
COPY --from=builder /out/app /usr/local/bin/app
CMD ["app"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferMultiStageBuildRule()
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestPreferMultiStageBuildRule_MinScoreConfig_Disables(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.22-alpine
RUN go build -o /out/app ./cmd/app
`
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, map[string]any{"min-score": 10})
	r := NewPreferMultiStageBuildRule()
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}
