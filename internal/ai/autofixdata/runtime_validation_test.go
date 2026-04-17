package autofixdata

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
)

func TestExtractFinalStageRuntime_CountsAndOccurrences(t *testing.T) {
	t.Parallel()

	// Two WORKDIRs, two USERs, two ENVs, one LABEL with two keys, two EXPOSEs,
	// one HEALTHCHECK, one ENTRYPOINT, one CMD — all on the final stage.
	src := `FROM alpine:3.20 AS build
RUN echo build

FROM alpine:3.20
WORKDIR /app
WORKDIR /app/sub
USER app
USER root
ENV FOO=1
ENV BAR=2 BAZ=3
LABEL org.example.a=a org.example.b=b
EXPOSE 80
EXPOSE 443
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
ENTRYPOINT ["/entry.sh"]
CMD ["python", "-m", "app"]
`
	parsed, err := dockerfile.Parse(bytes.NewReader([]byte(src)), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	rt := ExtractFinalStageRuntime(parsed)

	cases := []struct {
		name string
		got  int
		want int
	}{
		{"WorkdirCount", rt.WorkdirCount, 2},
		{"UserCount", rt.UserCount, 2},
		{"EnvCount", rt.EnvCount, 2},
		{"LabelCount", rt.LabelCount, 1},
		{"ExposeCount", rt.ExposeCount, 2},
		{"HealthCount", rt.HealthCount, 1},
		{"EntrypointCount", rt.EntrypointCount, 1},
		{"CmdCount", rt.CmdCount, 1},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	if len(rt.AllWorkdirs) != 2 {
		t.Errorf("AllWorkdirs len = %d, want 2", len(rt.AllWorkdirs))
	}
	if len(rt.AllUsers) != 2 {
		t.Errorf("AllUsers len = %d, want 2", len(rt.AllUsers))
	}

	wantEnvKeys := []string{"FOO", "BAR", "BAZ"}
	if !slices.Equal(rt.EnvKeys(), wantEnvKeys) {
		t.Errorf("EnvKeys() = %v, want %v", rt.EnvKeys(), wantEnvKeys)
	}
	wantLabelKeys := []string{"org.example.a", "org.example.b"}
	if !slices.Equal(rt.LabelKeys(), wantLabelKeys) {
		t.Errorf("LabelKeys() = %v, want %v", rt.LabelKeys(), wantLabelKeys)
	}
	wantPorts := []string{"80", "443"}
	if !slices.Equal(rt.Expose, wantPorts) {
		t.Errorf("Expose = %v, want %v", rt.Expose, wantPorts)
	}

	if rt.Cmd == nil || rt.Entrypoint == nil || rt.Workdir == nil || rt.User == nil || rt.Health == nil {
		t.Errorf("expected all last-occurrence pointers to be non-nil: %+v", rt)
	}
}

func TestExtractFinalStageRuntime_EmptyParse(t *testing.T) {
	t.Parallel()

	rt := ExtractFinalStageRuntime(nil)
	if rt.CmdCount != 0 || rt.EnvCount != 0 {
		t.Errorf("nil parse should return zero snapshot, got %+v", rt)
	}

	rt = ExtractFinalStageRuntime(&dockerfile.ParseResult{})
	if rt.CmdCount != 0 || rt.EnvCount != 0 {
		t.Errorf("empty stages should return zero snapshot, got %+v", rt)
	}
}
