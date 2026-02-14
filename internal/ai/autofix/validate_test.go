package autofix

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/dockerfile"
)

func mustParseDockerfile(t *testing.T, content string) *dockerfile.ParseResult {
	t.Helper()
	cfg := config.Default()
	parsed, err := parseDockerfile([]byte(content), cfg)
	if err != nil {
		t.Fatalf("parse Dockerfile: %v", err)
	}
	return parsed
}

func TestValidateStageCount_SingleStageInputRequiresMultiStageProposal(t *testing.T) {
	t.Parallel()

	orig := mustParseDockerfile(t, "FROM alpine:3.20\nRUN echo hi\n")
	proposedSingle := mustParseDockerfile(t, "FROM alpine:3.20\nRUN echo hi\n")
	if err := validateStageCount(orig, proposedSingle); err == nil {
		t.Fatalf("expected stage-count error, got nil")
	}

	proposedMulti := mustParseDockerfile(t, "FROM alpine:3.20 AS builder\nRUN echo build\nFROM alpine:3.20\nRUN echo runtime\n")
	if err := validateStageCount(orig, proposedMulti); err != nil {
		t.Fatalf("expected no stage-count error, got %v", err)
	}
}

func TestValidateStageCount_RejectsProposalWithoutFrom(t *testing.T) {
	t.Parallel()

	orig := mustParseDockerfile(t, "FROM alpine:3.20\nRUN echo hi\n")
	proposed := mustParseDockerfile(t, "RUN echo hi\n")
	if err := validateStageCount(orig, proposed); err == nil {
		t.Fatalf("expected stage-count error, got nil")
	}
}

func TestValidateRuntimeSettings_PreservesFinalStageSettings(t *testing.T) {
	t.Parallel()

	orig := mustParseDockerfile(t, `FROM alpine:3.20
WORKDIR /app
ENV FOO=bar
ENV BAZ=qux
LABEL foo=bar
LABEL baz=qux
HEALTHCHECK CMD ["sh","-c","echo ok"]
EXPOSE 8080 9090
USER 1000
ENTRYPOINT ["app"]
CMD ["--help"]
`)

	proposed := mustParseDockerfile(t, `FROM alpine:3.20 AS builder
RUN echo build

FROM alpine:3.20
WORKDIR /app
ENV FOO=bar
ENV BAZ=qux
LABEL foo=bar
LABEL baz=qux
HEALTHCHECK CMD ["sh","-c","echo ok"]
EXPOSE 8080 9090
USER 1000
ENTRYPOINT ["app"]
CMD ["--help"]
COPY --from=builder /bin/sh /bin/sh
`)

	if err := validateRuntimeSettings(orig, proposed); err != nil {
		t.Fatalf("expected no runtime-settings error, got %v", err)
	}
}

func TestValidateRuntimeSettings_FailsOnAddedCMD(t *testing.T) {
	t.Parallel()

	orig := mustParseDockerfile(t, `FROM alpine:3.20
ENTRYPOINT ["app"]
`)
	proposed := mustParseDockerfile(t, `FROM alpine:3.20 AS builder
RUN echo build

FROM alpine:3.20
ENTRYPOINT ["app"]
CMD ["--help"]
COPY --from=builder /bin/sh /bin/sh
`)

	if err := validateRuntimeSettings(orig, proposed); err == nil {
		t.Fatalf("expected runtime-settings error, got nil")
	}
}

func TestValidateRuntimeSettings_FailsOnChangedWorkdir(t *testing.T) {
	t.Parallel()

	orig := mustParseDockerfile(t, "FROM alpine:3.20\nWORKDIR /app\nCMD [\"app\"]\n")
	proposed := mustParseDockerfile(t, `FROM alpine:3.20 AS builder
RUN echo build
FROM alpine:3.20
WORKDIR /srv
CMD ["app"]
COPY --from=builder /bin/sh /bin/sh
`)

	if err := validateRuntimeSettings(orig, proposed); err == nil {
		t.Fatalf("expected runtime-settings error, got nil")
	}
}

func TestValidateRuntimeSettings_FailsOnChangedEnv(t *testing.T) {
	t.Parallel()

	orig := mustParseDockerfile(t, "FROM alpine:3.20\nENV FOO=bar\nCMD [\"app\"]\n")
	proposed := mustParseDockerfile(t, `FROM alpine:3.20 AS builder
RUN echo build
FROM alpine:3.20
ENV FOO=baz
CMD ["app"]
COPY --from=builder /bin/sh /bin/sh
`)

	if err := validateRuntimeSettings(orig, proposed); err == nil {
		t.Fatalf("expected runtime-settings error, got nil")
	}
}
