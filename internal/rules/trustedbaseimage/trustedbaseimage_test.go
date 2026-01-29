package trustedbaseimage

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestMetadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != "hadolint/DL3026" {
		t.Errorf("expected code hadolint/DL3026, got %s", meta.Code)
	}
	// Off by default, auto-enabled when trusted-registries configured
	if meta.DefaultSeverity != rules.SeverityOff {
		t.Errorf("expected DefaultSeverity=off, got %v", meta.DefaultSeverity)
	}
}

func TestNoConfigDisablesRule(t *testing.T) {
	r := New()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM python:3.9
RUN pip install flask
`)
	// No config means rule is disabled
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with no config, got %d", len(violations))
	}
}

func TestTrustedRegistry(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM docker.io/python:3.9
RUN pip install flask
`, Config{TrustedRegistries: []string{"docker.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for trusted registry, got %d", len(violations))
	}
}

func TestUntrustedRegistry(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM randomguy/python:3.9
RUN pip install flask
`, Config{TrustedRegistries: []string{"gcr.io"}})

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for untrusted registry, got %d", len(violations))
	}
	if violations[0].RuleCode != "hadolint/DL3026" {
		t.Errorf("expected rule code hadolint/DL3026, got %s", violations[0].RuleCode)
	}
}

func TestImplicitDockerHub(t *testing.T) {
	r := New()

	tests := []struct {
		name       string
		dockerfile string
		trusted    []string
		wantViol   int
	}{
		{
			name:       "implicit docker.io trusted",
			dockerfile: "FROM python:3.9\n",
			trusted:    []string{"docker.io"},
			wantViol:   0,
		},
		{
			name:       "implicit docker.io untrusted",
			dockerfile: "FROM python:3.9\n",
			trusted:    []string{"gcr.io"},
			wantViol:   1,
		},
		{
			name:       "library prefix trusted",
			dockerfile: "FROM library/python:3.9\n",
			trusted:    []string{"docker.io"},
			wantViol:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.dockerfile,
				Config{TrustedRegistries: tt.trusted})
			violations := r.Check(input)
			if len(violations) != tt.wantViol {
				t.Errorf("expected %d violations, got %d", tt.wantViol, len(violations))
			}
		})
	}
}

func TestCustomRegistry(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM my-registry.com/myimage:latest
RUN echo hello
`, Config{TrustedRegistries: []string{"my-registry.com"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for trusted custom registry, got %d", len(violations))
	}
}

func TestRegistryWithPort(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM localhost:5000/myimage:latest
RUN echo hello
`, Config{TrustedRegistries: []string{"localhost:5000"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for trusted registry with port, got %d", len(violations))
	}
}

func TestScratchIsAlwaysAllowed(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM scratch
COPY binary /
`, Config{TrustedRegistries: []string{"gcr.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for scratch, got %d", len(violations))
	}
}

func TestStageReferenceIsAllowed(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM gcr.io/distroless/static AS base
RUN echo hello

FROM base
COPY --from=base /etc/passwd /etc/passwd
`, Config{TrustedRegistries: []string{"gcr.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when using stage reference, got %d", len(violations))
	}
}

func TestMultipleRegistries(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM gcr.io/distroless/static AS build
RUN echo build

FROM docker.io/alpine:3.18
RUN echo runtime
`, Config{TrustedRegistries: []string{"gcr.io", "docker.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for multiple trusted registries, got %d", len(violations))
	}
}

func TestDockerHubAliases(t *testing.T) {
	r := New()

	// All these should be treated as docker.io
	tests := []struct {
		name    string
		trusted string
	}{
		{"docker.io", "docker.io"},
		{"index.docker.io", "index.docker.io"},
		{"registry-1.docker.io", "registry-1.docker.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", "FROM python:3.9\n",
				Config{TrustedRegistries: []string{tt.trusted}})
			violations := r.Check(input)
			if len(violations) != 0 {
				t.Errorf("expected 0 violations with %s as trusted, got %d", tt.trusted, len(violations))
			}
		})
	}
}

func TestConfigFromMap(t *testing.T) {
	r := New()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", "FROM python:3.9\n",
		map[string]any{
			"trusted-registries": []any{"docker.io"},
		})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with map config, got %d", len(violations))
	}
}
