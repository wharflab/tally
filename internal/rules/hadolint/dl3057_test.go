package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3057Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3057Rule().Metadata())
}

func TestDL3057Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		// === Fast-path tests (static, no registry) ===
		{
			name: "no CMD or ENTRYPOINT delegates to parent — suppressed",
			dockerfile: `FROM scratch
`,
			wantCount: 0,
		},
		{
			name: "ok with one HEALTHCHECK CMD instruction",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/bla
`,
			wantCount: 0,
		},
		{
			name: "ok with inheriting HEALTHCHECK CMD instruction",
			dockerfile: `FROM scratch AS base
HEALTHCHECK CMD /bin/bla
FROM base
`,
			wantCount: 0,
		},
		{
			name: "multi-stage final stage without CMD — suppressed",
			dockerfile: `FROM alpine:3.18 AS builder
RUN echo "build"

FROM debian:bookworm
RUN echo "run"
`,
			wantCount: 0,
		},
		{
			name: "HEALTHCHECK NONE alone is explicit opt-out",
			dockerfile: `FROM scratch
HEALTHCHECK NONE
`,
			wantCount: 0, // NONE is a deliberate opt-out; DL3057 should not fire
		},
		{
			name: "HEALTHCHECK CMD in any stage suppresses all",
			dockerfile: `FROM alpine:3.18 AS builder
RUN echo "build"

FROM debian:bookworm
HEALTHCHECK CMD curl -f http://localhost/
`,
			wantCount: 0,
		},
		{
			name: "chain with HEALTHCHECK CMD in middle",
			dockerfile: `FROM scratch AS base
FROM base AS middle
HEALTHCHECK CMD /bin/check
FROM middle AS final
`,
			wantCount: 0,
		},
		{
			name: "parallel branches both with HEALTHCHECK CMD",
			dockerfile: `FROM scratch AS base
FROM base AS branch1
HEALTHCHECK CMD /bin/check1
FROM base AS branch2
HEALTHCHECK CMD /bin/check2
`,
			wantCount: 0,
		},
		{
			name: "HEALTHCHECK with interval options",
			dockerfile: `FROM alpine:3.18
HEALTHCHECK --interval=30s CMD curl -f http://localhost/ || exit 1
`,
			wantCount: 0,
		},
		{
			name: "HEALTHCHECK CMD followed by HEALTHCHECK NONE is explicit opt-out",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/check
HEALTHCHECK NONE
`,
			wantCount: 0, // HEALTHCHECK NONE is a deliberate opt-out
		},
		{
			name: "HEALTHCHECK NONE followed by HEALTHCHECK CMD uses last instruction",
			dockerfile: `FROM scratch
HEALTHCHECK NONE
HEALTHCHECK CMD /bin/check
`,
			wantCount: 0, // Last HEALTHCHECK is CMD, so no violation
		},
		{
			name: "ONBUILD HEALTHCHECK without CMD — suppressed (no CMD delegates to parent)",
			dockerfile: `FROM scratch
ONBUILD HEALTHCHECK CMD /bin/check
`,
			wantCount: 0,
		},

		// === Smart suppression: serverless base images ===
		{
			name: "suppressed for AWS Lambda base image (ECR)",
			dockerfile: `FROM public.ecr.aws/lambda/python:3.12
COPY app.py /var/task/
CMD ["app.handler"]
`,
			wantCount: 0,
		},
		{
			name: "suppressed for AWS Lambda base image (Docker Hub)",
			dockerfile: `FROM amazon/aws-lambda-python:3.12
COPY app.py /var/task/
CMD ["app.handler"]
`,
			wantCount: 0,
		},
		{
			name: "suppressed for AWS Lambda in multi-stage build",
			dockerfile: `FROM golang:1.21 AS builder
RUN echo "build"
FROM public.ecr.aws/lambda/go:latest
COPY --from=builder /app /var/task
`,
			wantCount: 0,
		},
		{
			name: "suppressed for Azure Functions base image",
			dockerfile: `FROM mcr.microsoft.com/azure-functions/dotnet:4
COPY . /home/site/wwwroot
`,
			wantCount: 0,
		},
		{
			name: "suppressed for OpenFaaS of-watchdog",
			dockerfile: `FROM ghcr.io/openfaas/of-watchdog:latest AS watchdog
FROM alpine:3.18
COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
`,
			wantCount: 0,
		},
		{
			name: "suppressed for OpenFaaS classic-watchdog",
			dockerfile: `FROM openfaas/classic-watchdog:latest AS watchdog
FROM alpine:3.18
COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
`,
			wantCount: 0,
		},

		// === Smart suppression: serverless entrypoints ===
		{
			name: "suppressed for functions-framework CMD (exec form)",
			dockerfile: `FROM python:3.12-slim
RUN pip install functions-framework
COPY main.py .
CMD ["functions-framework", "--target=hello"]
`,
			wantCount: 0,
		},
		{
			name: "suppressed for functions-framework CMD (shell form)",
			dockerfile: `FROM python:3.12-slim
RUN pip install functions-framework
COPY main.py .
CMD functions-framework --target=hello
`,
			wantCount: 0,
		},
		{
			name: "suppressed for functions-framework CMD with exec prefix",
			dockerfile: `FROM python:3.12-slim
RUN pip install functions-framework
COPY main.py .
CMD exec functions-framework --target=handle_dlq_message --source=/app/main.py --port=$PORT
`,
			wantCount: 0,
		},
		{
			name: "suppressed for functions-framework ENTRYPOINT",
			dockerfile: `FROM python:3.12-slim
RUN pip install functions-framework
COPY main.py .
ENTRYPOINT ["functions-framework"]
CMD ["--target=hello"]
`,
			wantCount: 0,
		},

		// === Smart suppression: shell-only CMD/ENTRYPOINT ===
		{
			name: "suppressed when CMD is bare bash",
			dockerfile: `FROM ubuntu:22.04
CMD ["bash"]
`,
			wantCount: 0,
		},
		{
			name: "suppressed when CMD is /bin/sh",
			dockerfile: `FROM alpine:3.18
CMD ["/bin/sh"]
`,
			wantCount: 0,
		},
		{
			name: "suppressed when CMD is shell form bash",
			dockerfile: `FROM ubuntu:22.04
CMD bash
`,
			wantCount: 0,
		},
		{
			name: "suppressed when ENTRYPOINT is bare shell",
			dockerfile: `FROM ubuntu:22.04
ENTRYPOINT ["/bin/bash"]
`,
			wantCount: 0,
		},
		{
			name: "suppressed when CMD is bash with login flag",
			dockerfile: `FROM ubuntu:22.04
CMD ["bash", "-l"]
`,
			wantCount: 0,
		},
		{
			name: "not suppressed when CMD is bash -c command",
			dockerfile: `FROM ubuntu:22.04
CMD ["bash", "-c", "my-app"]
`,
			wantCount: 1,
		},
		{
			name: "not suppressed when CMD is a real application",
			dockerfile: `FROM ubuntu:22.04
CMD ["nginx", "-g", "daemon off;"]
`,
			wantCount: 1,
		},
		{
			name: "suppressed for shell-only in final stage of multi-stage",
			dockerfile: `FROM golang:1.21 AS builder
RUN echo "build"
FROM alpine:3.18
CMD ["ash"]
`,
			wantCount: 0,
		},
		{
			name: "non-final stage has shell CMD but final has none — suppressed",
			dockerfile: `FROM ubuntu:22.04 AS base
CMD ["bash"]
FROM alpine:3.18
RUN echo "app"
`,
			wantCount: 0,
		},
		{
			name: "suppressed when ENTRYPOINT overrides CMD with shell",
			dockerfile: `FROM alpine:3.18
CMD ["my-app"]
ENTRYPOINT ["/bin/sh"]
`,
			wantCount: 0,
		},

		// === Smart suppression: no CMD/ENTRYPOINT (delegates to parent image) ===
		{
			name: "suppressed when final stage has no CMD or ENTRYPOINT",
			dockerfile: `FROM nginx:latest
RUN echo "custom config"
`,
			wantCount: 0,
		},
		{
			name: "suppressed when multi-stage final stage has no CMD or ENTRYPOINT",
			dockerfile: `FROM golang:1.21 AS builder
RUN echo "build"
CMD ["go", "test"]

FROM alpine:3.18
COPY --from=builder /app /app
`,
			wantCount: 0,
		},
		{
			name: "not suppressed when final stage has CMD",
			dockerfile: `FROM alpine:3.18
CMD ["my-app"]
`,
			wantCount: 1,
		},
		{
			name: "not suppressed when final stage has ENTRYPOINT",
			dockerfile: `FROM alpine:3.18
ENTRYPOINT ["my-app"]
`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3057Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s at line %d: %s", v.RuleCode, v.Location.Start.Line, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}

			// Verify file-level violation uses StageIndex=-1
			if tt.wantCount > 0 && len(violations) > 0 {
				if violations[0].StageIndex != -1 {
					t.Errorf("StageIndex = %d, want -1 (file-level)", violations[0].StageIndex)
				}
				if !violations[0].Location.IsFileLevel() {
					t.Error("expected file-level location")
				}
			}
		})
	}
}

func TestDL3057Rule_RequiresSemantic(t *testing.T) {
	t.Parallel()
	// Without semantic model, the rule should return no violations
	input := testutil.MakeLintInput(t, "Dockerfile", "FROM scratch\n")
	input.Semantic = nil // explicitly test nil-semantic fallback
	r := NewDL3057Rule()
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations without semantic model, got %d", len(violations))
	}
}

func TestDL3057Rule_PlanAsync(t *testing.T) {
	t.Parallel()

	t.Run("plans checks for external images with CMD", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nRUN echo hello\nCMD [\"my-app\"]\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) == 0 {
			t.Fatal("expected at least one async request")
		}
		if requests[0].RuleCode != rules.HadolintRulePrefix+"DL3057" {
			t.Errorf("RuleCode = %q, want hadolint/DL3057", requests[0].RuleCode)
		}
	})

	t.Run("no plans when final stage has no CMD or ENTRYPOINT", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nRUN echo hello\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests when no CMD/ENTRYPOINT, got %d", len(requests))
		}
	})

	t.Run("no plans when HEALTHCHECK NONE present", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nHEALTHCHECK NONE\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests when HEALTHCHECK NONE present, got %d", len(requests))
		}
	})

	t.Run("no plans when HEALTHCHECK CMD present", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nHEALTHCHECK CMD curl -f http://localhost/\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests when HEALTHCHECK CMD present, got %d", len(requests))
		}
	})

	t.Run("no plans for serverless base image", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM public.ecr.aws/lambda/python:3.12\nCOPY app.py /var/task/\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests for serverless image, got %d", len(requests))
		}
	})

	t.Run("no plans for shell-only CMD", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nCMD [\"sh\"]\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests for shell-only CMD, got %d", len(requests))
		}
	})

	t.Run("no plans for functions-framework CMD", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM python:3.12-slim\nCMD [\"functions-framework\", \"--target=hello\"]\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests for functions-framework CMD, got %d", len(requests))
		}
	})
}

func makeHandler(t *testing.T, dockerfile string) *healthcheckHandler {
	t.Helper()
	r := NewDL3057Rule()
	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	return &healthcheckHandler{
		meta: r.Metadata(),
		file: input.File,
	}
}

func TestDL3057Rule_Handler_BaseHasHealthcheck(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\nRUN echo hello\n")
	result := h.OnSuccess(&registry.ImageConfig{HasHealthcheck: true})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should contain a CompletedCheck with StageIndex=-1
	if !hasCompletedCheckAtStage(result, -1) {
		t.Error("expected CompletedCheck with StageIndex=-1")
	}
	// Should have no violations
	if hasAnyViolation(result) {
		t.Error("expected no violations when base has healthcheck")
	}
}

func TestDL3057Rule_Handler_BaseNoHealthcheck(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\nRUN echo hello\n")
	result := h.OnSuccess(&registry.ImageConfig{HasHealthcheck: false})
	// No HEALTHCHECK in base and no explicit opt-out → fast-path violation should remain.
	if result != nil {
		t.Errorf("expected nil result when base has no healthcheck, got %v", result)
	}
}

func TestDL3057Rule_Handler_NilConfig(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\n")
	if result := h.OnSuccess((*registry.ImageConfig)(nil)); result != nil {
		t.Errorf("expected nil for nil config, got %v", result)
	}
}

func TestDL3057Rule_Handler_WrongType(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\n")
	if result := h.OnSuccess("not an ImageConfig"); result != nil {
		t.Errorf("expected nil for wrong type, got %v", result)
	}
}

func TestIsServerlessImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		image string
		want  bool
	}{
		// AWS Lambda
		{"public.ecr.aws/lambda/python:3.12", true},
		{"gallery.ecr.aws/lambda/nodejs:18", true},
		{"public.ecr.aws/lambda/go:latest", true},
		{"amazon/aws-lambda-python:3.12", true},
		{"amazon/aws-lambda-java:17", true},
		// Azure Functions
		{"mcr.microsoft.com/azure-functions/dotnet:4", true},
		{"mcr.microsoft.com/azure-functions/node:18", true},
		// OpenFaaS
		{"ghcr.io/openfaas/of-watchdog:latest", true},
		{"openfaas/classic-watchdog:latest", true},
		// Case insensitive
		{"Public.ECR.AWS/Lambda/Python:3.12", true},
		// Not serverless (image-level)
		{"ubuntu:22.04", false},
		{"nginx:latest", false},
		{"alpine:3.18", false},
		{"scratch", false},
		{"gcr.io/my-project/my-app:latest", false},
		{"mcr.microsoft.com/dotnet/aspnet:8.0", false},
		{"gcr.io/google-appengine/python", false}, // App Engine can run long-lived services
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			t.Parallel()
			if got := isServerlessImage(tt.image); got != tt.want {
				t.Errorf("isServerlessImage(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}

func TestIsServerlessEntrypoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		cmdLine      []string
		prependShell bool
		want         bool
	}{
		// Google Cloud Functions framework
		{"exec functions-framework", []string{"functions-framework", "--target=hello"}, false, true},
		{"shell functions-framework", []string{"functions-framework --target=hello"}, true, true},
		{"exec bare functions-framework", []string{"functions-framework"}, false, true},
		{"shell exec functions-framework", []string{"exec functions-framework --target=hello --port=$PORT"}, true, true},
		{"shell env functions-framework", []string{"env FOO=bar functions-framework --target=hello"}, true, true},
		// Not serverless entrypoints
		{"exec nginx", []string{"nginx"}, false, false},
		{"exec python", []string{"python", "app.py"}, false, false},
		{"exec bash", []string{"bash"}, false, false},
		{"empty", []string{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isServerlessEntrypoint(tt.cmdLine, tt.prependShell, shell.VariantBash); got != tt.want {
				t.Errorf("isServerlessEntrypoint(%v, %v) = %v, want %v", tt.cmdLine, tt.prependShell, got, tt.want)
			}
		})
	}
}

func TestIsShellOnlyArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		cmdLine      []string
		prependShell bool
		want         bool
	}{
		// Exec form shells
		{"exec bash", []string{"bash"}, false, true},
		{"exec /bin/sh", []string{"/bin/sh"}, false, true},
		{"exec /usr/bin/zsh", []string{"/usr/bin/zsh"}, false, true},
		{"exec ash", []string{"ash"}, false, true},
		{"exec fish", []string{"fish"}, false, true},
		{"exec dash", []string{"dash"}, false, true},
		// Exec form with flags
		{"exec bash -l", []string{"bash", "-l"}, false, true},
		{"exec bash --login", []string{"bash", "--login"}, false, true},
		// Exec form with -c (not shell-only)
		{"exec bash -c cmd", []string{"bash", "-c", "echo hello"}, false, false},
		{"exec sh -e", []string{"sh", "-e"}, false, false},
		// Shell form — parsed by mvdan.cc/sh via shell.CommandNamesWithVariant
		{"shell bash", []string{"bash"}, true, true},
		{"shell /bin/bash", []string{"/bin/bash"}, true, true},
		{"shell bash -l", []string{"bash -l"}, true, true},
		{"shell exec bash", []string{"exec bash"}, true, true},
		{"shell exec /usr/bin/bash", []string{"exec /usr/bin/bash"}, true, true},
		{"shell bash -c cmd", []string{"bash -c 'echo hello'"}, true, false},
		{"shell exec bash -c cmd", []string{"exec bash -c 'my-app'"}, true, false},
		// Not shells
		{"exec nginx", []string{"nginx"}, false, false},
		{"exec my-app", []string{"my-app"}, false, false},
		{"empty", []string{}, false, false},
		{"shell empty", []string{""}, true, false},
		{"shell nginx", []string{"nginx -g 'daemon off;'"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isShellOnlyArgs(tt.cmdLine, tt.prependShell, shell.VariantBash); got != tt.want {
				t.Errorf("isShellOnlyArgs(%v, %v) = %v, want %v", tt.cmdLine, tt.prependShell, got, tt.want)
			}
		})
	}
}

// hasCompletedCheckAtStage checks if any item in the result is a CompletedCheck
// with the given StageIndex.
func hasCompletedCheckAtStage(result []any, stageIdx int) bool {
	for _, item := range result {
		if cc, ok := item.(async.CompletedCheck); ok && cc.StageIndex == stageIdx {
			return true
		}
	}
	return false
}

// hasAnyViolation checks if any item in the result is a Violation.
func hasAnyViolation(result []any) bool {
	for _, item := range result {
		if _, ok := item.(rules.Violation); ok {
			return true
		}
	}
	return false
}
