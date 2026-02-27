package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
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

func TestPreferMultiStageBuildRule_WindowsBuildTools_Triggers(t *testing.T) {
	t.Parallel()
	content := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN choco install microsoft-build-tools -y
COPY . c:\build
RUN nuget restore
RUN C:\Windows\Microsoft.NET\Framework64\v4.0.30319\MSBuild.exe /p:Configuration=Release MyApp.sln
ENTRYPOINT ["MyApp.exe"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferMultiStageBuildRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.MultiStageResolveData)
	if !ok {
		t.Fatalf("expected MultiStageResolveData, got %T", violations[0].SuggestedFix.ResolverData)
	}
	// choco install (4) + build-tools bonus (2) + msbuild (4) + nuget restore (2) = 12
	if data.Score < 8 {
		t.Errorf("expected score >= 8 for Windows build, got %d", data.Score)
	}
	t.Logf("signals: %+v, score: %d", data.Signals, data.Score)
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
