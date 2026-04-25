package invocation

import (
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func TestComposePublishedPortsReturnsParseError(t *testing.T) {
	t.Parallel()

	_, err := composePublishedPorts([]composetypes.ServicePortConfig{{
		Target:    8080,
		Published: "bad",
	}})
	if err == nil {
		t.Fatal("expected invalid published port error")
	}
	if !strings.Contains(err.Error(), `published port "bad"`) {
		t.Fatalf("error = %q, want published port context", err)
	}
}

func TestComposeExposedPortsReturnsParseError(t *testing.T) {
	t.Parallel()

	_, err := composeExposedPorts(composetypes.StringOrNumberList{"bad/tcp"})
	if err == nil {
		t.Fatal("expected invalid exposed port error")
	}
	if !strings.Contains(err.Error(), `expose "bad/tcp"`) {
		t.Fatalf("error = %q, want expose context", err)
	}
}

func TestComposeSecretRefBuildDefaultTarget(t *testing.T) {
	t.Parallel()

	ref := composeSecretRef(
		t.TempDir(),
		SecretScopeBuild,
		composetypes.ServiceSecretConfig(composetypes.FileReferenceConfig{Source: "token"}),
		nil,
	)
	if ref.Target != "/run/secrets/token" {
		t.Fatalf("Target = %q, want /run/secrets/token", ref.Target)
	}
}

func TestComposeSecretRefDiscriminatesEnvSource(t *testing.T) {
	t.Parallel()

	ref := composeSecretRef(
		t.TempDir(),
		SecretScopeBuild,
		composetypes.ServiceSecretConfig(composetypes.FileReferenceConfig{Source: "token"}),
		composetypes.Secrets{
			"token": composetypes.SecretConfig(composetypes.FileObjectConfig{Environment: "TOKEN"}),
		},
	)
	if ref.Source != "env:TOKEN" {
		t.Fatalf("Source = %q, want env:TOKEN", ref.Source)
	}
}

func TestComposeCommandOverrideNilEmptyAndClone(t *testing.T) {
	t.Parallel()

	if got := composeCommandOverride(nil); got != nil {
		t.Fatalf("nil command override = %#v, want nil", got)
	}

	empty := composetypes.ShellCommand{}
	got := composeCommandOverride(empty)
	if got == nil {
		t.Fatal("empty command override = nil, want explicit override")
	}
	if got.Args == nil || len(got.Args) != 0 {
		t.Fatalf("empty command Args = %#v, want non-nil empty slice", got.Args)
	}
	if !got.ClearsImageValue {
		t.Fatal("empty command ClearsImageValue = false, want true")
	}

	command := composetypes.ShellCommand{"echo", "ok"}
	got = composeCommandOverride(command)
	if got == nil {
		t.Fatal("non-empty command override = nil")
	}
	command[0] = "mutated"
	if !slices.Equal(got.Args, []string{"echo", "ok"}) {
		t.Fatalf("command Args = %#v, want cloned args", got.Args)
	}
	if got.ClearsImageValue {
		t.Fatal("non-empty command ClearsImageValue = true, want false")
	}
}

func TestComposeHealthcheckParsesDurationCompanions(t *testing.T) {
	t.Parallel()

	interval := composetypes.Duration(5 * time.Second)
	timeout := composetypes.Duration(2 * time.Second)
	spec := composeHealthcheck(&composetypes.HealthCheckConfig{
		Interval: &interval,
		Timeout:  &timeout,
	})
	if spec == nil {
		t.Fatal("composeHealthcheck() = nil")
	}
	if spec.Interval != "5s" || spec.IntervalDur != 5*time.Second {
		t.Fatalf("Interval = %q/%s, want 5s/5s", spec.Interval, spec.IntervalDur)
	}
	if spec.Timeout != "2s" || spec.TimeoutDur != 2*time.Second {
		t.Fatalf("Timeout = %q/%s, want 2s/2s", spec.Timeout, spec.TimeoutDur)
	}
}

func TestHealthcheckSpecUnmarshalParsesDurationCompanions(t *testing.T) {
	t.Parallel()

	var spec HealthcheckSpec
	if err := jsonv2.Unmarshal([]byte(`{"interval":"3s","timeout":"1s"}`), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if spec.IntervalDur != 3*time.Second {
		t.Fatalf("IntervalDur = %s, want 3s", spec.IntervalDur)
	}
	if spec.TimeoutDur != time.Second {
		t.Fatalf("TimeoutDur = %s, want 1s", spec.TimeoutDur)
	}
}

func TestHealthcheckSpecParseDurationsReportsAllErrors(t *testing.T) {
	t.Parallel()

	spec := &HealthcheckSpec{
		Interval: "bad-interval",
		Timeout:  "bad-timeout",
	}
	err := spec.parseDurations()
	if err == nil {
		t.Fatal("parseDurations() error = nil, want joined duration errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "interval") || !strings.Contains(msg, "timeout") {
		t.Fatalf("parseDurations() error = %q, want interval and timeout context", msg)
	}
}
