package hadolint

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3026Rule_Metadata(t *testing.T) {
	r := NewDL3026Rule()
	meta := r.Metadata()

	if meta.Code != "hadolint/DL3026" {
		t.Errorf("expected code hadolint/DL3026, got %s", meta.Code)
	}
	// Off by default, auto-enabled when trusted-registries configured
	if meta.DefaultSeverity != rules.SeverityOff {
		t.Errorf("expected DefaultSeverity=off, got %v", meta.DefaultSeverity)
	}
}

func TestDL3026Rule_NoConfigDisablesRule(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM python:3.9
RUN pip install flask
`)
	// No config means rule is disabled
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with no config, got %d", len(violations))
	}
}

func TestDL3026Rule_TrustedRegistry(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM docker.io/python:3.9
RUN pip install flask
`, DL3026Config{TrustedRegistries: []string{"docker.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for trusted registry, got %d", len(violations))
	}
}

func TestDL3026Rule_UntrustedRegistry(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM randomguy/python:3.9
RUN pip install flask
`, DL3026Config{TrustedRegistries: []string{"gcr.io"}})

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for untrusted registry, got %d", len(violations))
	}
	if violations[0].RuleCode != "hadolint/DL3026" {
		t.Errorf("expected rule code hadolint/DL3026, got %s", violations[0].RuleCode)
	}
}

func TestDL3026Rule_ImplicitDockerHub(t *testing.T) {
	r := NewDL3026Rule()

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
				DL3026Config{TrustedRegistries: tt.trusted})
			violations := r.Check(input)
			if len(violations) != tt.wantViol {
				t.Errorf("expected %d violations, got %d", tt.wantViol, len(violations))
			}
		})
	}
}

func TestDL3026Rule_CustomRegistry(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM my-registry.com/myimage:latest
RUN echo hello
`, DL3026Config{TrustedRegistries: []string{"my-registry.com"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for trusted custom registry, got %d", len(violations))
	}
}

func TestDL3026Rule_RegistryWithPort(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM localhost:5000/myimage:latest
RUN echo hello
`, DL3026Config{TrustedRegistries: []string{"localhost:5000"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for trusted registry with port, got %d", len(violations))
	}
}

func TestDL3026Rule_ScratchIsAlwaysAllowed(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM scratch
COPY binary /
`, DL3026Config{TrustedRegistries: []string{"gcr.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for scratch, got %d", len(violations))
	}
}

func TestDL3026Rule_StageReferenceIsAllowed(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM gcr.io/distroless/static AS base
RUN echo hello

FROM base
COPY --from=base /etc/passwd /etc/passwd
`, DL3026Config{TrustedRegistries: []string{"gcr.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when using stage reference, got %d", len(violations))
	}
}

func TestDL3026Rule_MultipleRegistries(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM gcr.io/distroless/static AS build
RUN echo build

FROM docker.io/alpine:3.18
RUN echo runtime
`, DL3026Config{TrustedRegistries: []string{"gcr.io", "docker.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for multiple trusted registries, got %d", len(violations))
	}
}

func TestDL3026Rule_DockerHubAliases(t *testing.T) {
	r := NewDL3026Rule()

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
				DL3026Config{TrustedRegistries: []string{tt.trusted}})
			violations := r.Check(input)
			if len(violations) != 0 {
				t.Errorf("expected 0 violations with %s as trusted, got %d", tt.trusted, len(violations))
			}
		})
	}
}

func TestDL3026Rule_ConfigFromMap(t *testing.T) {
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", "FROM python:3.9\n",
		map[string]any{
			"trusted-registries": []any{"docker.io"},
		})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with map config, got %d", len(violations))
	}
}

// Tests adapted from hadolint/hadolint test/Hadolint/Rule/DL3026Spec.hs

func TestDL3026Rule_WildcardAny(t *testing.T) {
	// "does not warn on * registry" - from hadolint
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM ubuntu:18.04 AS builder1
FROM zemanlx/ubuntu:18.04 AS builder2
FROM docker.io/zemanlx/ubuntu:18.04 AS builder3
`, DL3026Config{TrustedRegistries: []string{"*"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with * wildcard, got %d", len(violations))
		for _, v := range violations {
			t.Logf("  violation: %s", v.Message)
		}
	}
}

func TestDL3026Rule_WildcardSuffix(t *testing.T) {
	// "does not warn on allowed wildcard registries" - from hadolint
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM foo.random.com/debian
RUN echo hello
`, DL3026Config{TrustedRegistries: []string{"x.com", "*.random.com"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with *.random.com wildcard, got %d", len(violations))
	}
}

func TestDL3026Rule_WildcardSuffixNoMatch(t *testing.T) {
	// "warn on non-allowed wildcard registry" - from hadolint
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM x.com/debian
RUN echo hello
`, DL3026Config{TrustedRegistries: []string{"*.random.com"}})

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation when wildcard doesn't match, got %d", len(violations))
	}
}

func TestDL3026Rule_AllDockerHubForms(t *testing.T) {
	// "allows both all forms of docker.io" - from hadolint
	// Tests that bare image names, user/image, and explicit docker.io all work
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM ubuntu:18.04 AS builder1
FROM zemanlx/ubuntu:18.04 AS builder2
FROM docker.io/zemanlx/ubuntu:18.04 AS builder3
`, DL3026Config{TrustedRegistries: []string{"docker.io"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for all docker.io forms, got %d", len(violations))
		for _, v := range violations {
			t.Logf("  violation: %s", v.Message)
		}
	}
}

func TestDL3026Rule_StageReferenceFromUntrusted(t *testing.T) {
	// "allows using previous stages" - from hadolint
	// Even if first stage is from trusted registry, referencing it by name should work
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM random.com/foo AS builder1
FROM builder1 AS builder2
`, DL3026Config{TrustedRegistries: []string{"random.com"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when using stage reference, got %d", len(violations))
	}
}

func TestDL3026Rule_MultipleAllowedRegistries(t *testing.T) {
	// "does not warn on allowed registries" - from hadolint
	r := NewDL3026Rule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM random.com/debian
RUN echo hello
`, DL3026Config{TrustedRegistries: []string{"x.com", "random.com"}})

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for image from one of multiple allowed registries, got %d", len(violations))
	}
}

func TestDL3026Rule_MatchRegistry(t *testing.T) {
	tests := []struct {
		pattern  string
		registry string
		want     bool
	}{
		// Exact matches
		{"docker.io", "docker.io", true},
		{"gcr.io", "gcr.io", true},
		{"gcr.io", "docker.io", false},

		// Wildcard any
		{"*", "docker.io", true},
		{"*", "gcr.io", true},
		{"*", "anything.example.com", true},

		// Suffix wildcard
		{"*.example.com", "foo.example.com", true},
		{"*.example.com", "bar.example.com", true},
		{"*.example.com", "sub.foo.example.com", true},
		{"*.example.com", "example.com", false}, // No subdomain
		{"*.example.com", "notexample.com", false},
		{"*.example.com", "docker.io", false},

		// Prefix wildcard
		{"gcr*", "gcr.io", true},
		{"gcr*", "gcr.example.com", true},
		{"gcr*", "docker.io", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.registry, func(t *testing.T) {
			got := matchRegistry(tt.pattern, tt.registry)
			if got != tt.want {
				t.Errorf("matchRegistry(%q, %q) = %v, want %v", tt.pattern, tt.registry, got, tt.want)
			}
		})
	}
}
