package autofixdata

import (
	"bytes"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
)

func TestExtractFinalStageRuntime_CountsAndOccurrences(t *testing.T) {
	t.Parallel()

	// Two WORKDIRs, two USERs, two ENVs, one LABEL with two keys, two EXPOSEs,
	// one HEALTHCHECK, one ENTRYPOINT, one CMD, one SHELL, one STOPSIGNAL,
	// two VOLUMEs — all on the final stage.
	src := `FROM alpine:3.20 AS build
RUN echo build

FROM alpine:3.20
SHELL ["/bin/bash", "-lc"]
STOPSIGNAL SIGTERM
VOLUME /data
VOLUME /cache /logs
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
		{"ShellCount", rt.ShellCount, 1},
		{"StopSignalCount", rt.StopSignalCount, 1},
		{"VolumeCount", rt.VolumeCount, 2},
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
	if rt.Shell == nil || rt.StopSignal == nil {
		t.Errorf("expected SHELL and STOPSIGNAL pointers to be non-nil: %+v", rt)
	}
	wantVolumes := []string{"/data", "/cache", "/logs"}
	if !slices.Equal(rt.Volumes, wantVolumes) {
		t.Errorf("Volumes = %v, want %v", rt.Volumes, wantVolumes)
	}
	wantShell := []string{"/bin/bash", "-lc"}
	if !slices.Equal(rt.Shell.Shell, wantShell) {
		t.Errorf("Shell = %v, want %v", rt.Shell.Shell, wantShell)
	}
	if rt.StopSignal.Signal != "SIGTERM" {
		t.Errorf("StopSignal.Signal = %q, want SIGTERM", rt.StopSignal.Signal)
	}
}

func TestFinalStageRuntimeErrors_ShellStopSignalVolume(t *testing.T) {
	t.Parallel()

	origSrc := `FROM alpine:3.20
SHELL ["/bin/bash", "-lc"]
STOPSIGNAL SIGTERM
VOLUME /data
CMD ["sh"]
`
	orig := mustParseRuntime(t, origSrc)

	t.Run("SHELL dropped", func(t *testing.T) {
		t.Parallel()
		proposed := mustParseRuntime(t, `FROM alpine:3.20
STOPSIGNAL SIGTERM
VOLUME /data
CMD ["sh"]
`)
		assertRuntimeErrorContains(t, FinalStageRuntimeErrors(orig, proposed), "SHELL")
	})

	t.Run("SHELL changed", func(t *testing.T) {
		t.Parallel()
		proposed := mustParseRuntime(t, `FROM alpine:3.20
SHELL ["/bin/sh", "-c"]
STOPSIGNAL SIGTERM
VOLUME /data
CMD ["sh"]
`)
		assertRuntimeErrorContains(t, FinalStageRuntimeErrors(orig, proposed), "SHELL")
	})

	t.Run("STOPSIGNAL changed", func(t *testing.T) {
		t.Parallel()
		proposed := mustParseRuntime(t, `FROM alpine:3.20
SHELL ["/bin/bash", "-lc"]
STOPSIGNAL SIGKILL
VOLUME /data
CMD ["sh"]
`)
		assertRuntimeErrorContains(t, FinalStageRuntimeErrors(orig, proposed), "STOPSIGNAL")
	})

	t.Run("VOLUME dropped", func(t *testing.T) {
		t.Parallel()
		proposed := mustParseRuntime(t, `FROM alpine:3.20
SHELL ["/bin/bash", "-lc"]
STOPSIGNAL SIGTERM
CMD ["sh"]
`)
		assertRuntimeErrorContains(t, FinalStageRuntimeErrors(orig, proposed), "VOLUME")
	})

	t.Run("VOLUME changed", func(t *testing.T) {
		t.Parallel()
		proposed := mustParseRuntime(t, `FROM alpine:3.20
SHELL ["/bin/bash", "-lc"]
STOPSIGNAL SIGTERM
VOLUME /elsewhere
CMD ["sh"]
`)
		assertRuntimeErrorContains(t, FinalStageRuntimeErrors(orig, proposed), "VOLUME")
	})

	t.Run("identical runtime → no blocking", func(t *testing.T) {
		t.Parallel()
		proposed := mustParseRuntime(t, origSrc)
		errs := FinalStageRuntimeErrors(orig, proposed)
		if len(errs) != 0 {
			t.Errorf("expected no errors for identical runtime, got %v", errs)
		}
	})
}

func mustParseRuntime(t *testing.T, src string) *dockerfile.ParseResult {
	t.Helper()
	parsed, err := dockerfile.Parse(bytes.NewReader([]byte(src)), nil)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	return parsed
}

func assertRuntimeErrorContains(t *testing.T, errs []error, want string) {
	t.Helper()
	for _, err := range errs {
		if err != nil && strings.Contains(err.Error(), want) {
			return
		}
	}
	t.Errorf("expected error mentioning %q, got %v", want, errs)
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

func TestFactsInt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		facts   map[string]any
		key     string
		wantVal int
		wantOK  bool
	}{
		{name: "int", facts: map[string]any{"k": 12}, key: "k", wantVal: 12, wantOK: true},
		{name: "int64", facts: map[string]any{"k": int64(7)}, key: "k", wantVal: 7, wantOK: true},
		{name: "int32", facts: map[string]any{"k": int32(3)}, key: "k", wantVal: 3, wantOK: true},
		{name: "float64 whole (json round-trip)", facts: map[string]any{"k": float64(12)}, key: "k", wantVal: 12, wantOK: true},
		{name: "float32 whole", facts: map[string]any{"k": float32(4)}, key: "k", wantVal: 4, wantOK: true},
		{name: "float64 non-integral", facts: map[string]any{"k": float64(12.9)}, key: "k", wantVal: 0, wantOK: false},
		{name: "float32 non-integral", facts: map[string]any{"k": float32(1.5)}, key: "k", wantVal: 0, wantOK: false},
		{name: "float64 NaN", facts: map[string]any{"k": math.NaN()}, key: "k", wantVal: 0, wantOK: false},
		{name: "float64 +Inf", facts: map[string]any{"k": math.Inf(1)}, key: "k", wantVal: 0, wantOK: false},
		{name: "float64 -Inf", facts: map[string]any{"k": math.Inf(-1)}, key: "k", wantVal: 0, wantOK: false},
		{name: "missing key", facts: map[string]any{"other": 1}, key: "k", wantVal: 0, wantOK: false},
		{name: "nil map", facts: nil, key: "k", wantVal: 0, wantOK: false},
		{name: "non-numeric", facts: map[string]any{"k": "12"}, key: "k", wantVal: 0, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := FactsInt(tc.facts, tc.key)
			if ok != tc.wantOK {
				t.Fatalf("FactsInt ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.wantVal {
				t.Errorf("FactsInt value = %d, want %d", got, tc.wantVal)
			}
		})
	}
}
