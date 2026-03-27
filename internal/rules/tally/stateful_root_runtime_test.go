package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestStatefulRootRuntimeMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewStatefulRootRuntimeRule().Metadata())
}

func TestStatefulRootRuntimeCheck(t *testing.T) {
	t.Parallel()

	rule := NewStatefulRootRuntimeRule()

	testutil.RunRuleTests(t, rule, []testutil.RuleTestCase{
		// === Core violations ===
		{
			Name: "explicit USER root + VOLUME",
			Content: `FROM ubuntu:22.04
USER root
VOLUME /data
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"runs as root"},
		},
		{
			Name: "implicit root (no USER) + VOLUME",
			Content: `FROM ubuntu:22.04
VOLUME /data
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"no USER instruction"},
		},
		{
			Name: "USER 0 (numeric root) + VOLUME",
			Content: `FROM ubuntu:22.04
USER 0
VOLUME /data
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"runs as root"},
		},
		{
			Name: "USER root:root with group + VOLUME",
			Content: `FROM ubuntu:22.04
USER root:root
VOLUME /var/lib/mysql
CMD ["mysqld"]
`,
			WantViolations: 1,
		},
		{
			Name: "root + WORKDIR state dir",
			Content: `FROM ubuntu:22.04
WORKDIR /var/lib/mysql
CMD ["mysqld"]
`,
			WantViolations: 1,
			WantMessages:   []string{"workdir /var/lib/mysql"},
		},
		{
			Name: "root + WORKDIR /data",
			Content: `FROM ubuntu:22.04
WORKDIR /data
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"workdir /data"},
		},
		{
			Name: "root + COPY to /var/log",
			Content: `FROM ubuntu:22.04
COPY config.conf /var/log/app/config.conf
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY destination"},
		},
		{
			Name: "root + ADD to /srv",
			Content: `FROM ubuntu:22.04
ADD data.tar.gz /srv/
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"ADD destination /srv"},
		},
		{
			Name: "root + RUN mkdir state dir",
			Content: `FROM ubuntu:22.04
RUN mkdir -p /var/lib/data
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"mkdir /var/lib/data"},
		},
		{
			Name: "root + multiple VOLUME paths",
			Content: `FROM ubuntu:22.04
VOLUME /data /var/log /var/lib/db
CMD ["app"]
`,
			WantViolations: 1,
		},
		{
			Name: "USER switches non-root then back to root + VOLUME",
			Content: `FROM ubuntu:22.04
USER appuser
USER root
VOLUME /data
CMD ["app"]
`,
			WantViolations: 1,
		},

		// === Clean cases (no violation) ===
		{
			Name: "non-root USER + VOLUME",
			Content: `FROM ubuntu:22.04
USER 1000
VOLUME /data
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "named non-root USER + VOLUME",
			Content: `FROM ubuntu:22.04
USER appuser
VOLUME /data
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "root + no stateful signal",
			Content: `FROM ubuntu:22.04
USER root
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "root + WORKDIR /app (not state path)",
			Content: `FROM ubuntu:22.04
WORKDIR /app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "root + COPY to /usr/bin (not state path)",
			Content: `FROM ubuntu:22.04
COPY app /usr/bin/app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "USER root then non-root + VOLUME",
			Content: `FROM ubuntu:22.04
USER root
RUN apt-get update
USER appuser
VOLUME /data
CMD ["app"]
`,
			WantViolations: 0,
		},

		// === Privilege-drop suppression ===
		{
			Name: "gosu in ENTRYPOINT suppresses",
			Content: `FROM ubuntu:22.04
VOLUME /var/lib/postgresql
ENTRYPOINT ["gosu", "postgres", "docker-entrypoint.sh"]
CMD ["postgres"]
`,
			WantViolations: 0,
		},
		{
			Name: "su-exec in ENTRYPOINT suppresses",
			Content: `FROM alpine:3.20
VOLUME /data
ENTRYPOINT ["su-exec", "redis", "redis-server"]
`,
			WantViolations: 0,
		},
		{
			Name: "docker-entrypoint.sh alone does not suppress",
			Content: `FROM ubuntu:22.04
VOLUME /var/lib/mysql
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["mysqld"]
`,
			WantViolations: 1,
		},
		{
			Name: "gosu in CMD suppresses",
			Content: `FROM ubuntu:22.04
VOLUME /data
CMD ["gosu", "nobody", "/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "shell-form entrypoint with gosu suppresses",
			Content: `FROM ubuntu:22.04
VOLUME /data
ENTRYPOINT exec gosu nobody /app
`,
			WantViolations: 0,
		},
		{
			Name: "setpriv in ENTRYPOINT suppresses",
			Content: `FROM ubuntu:22.04
VOLUME /data
ENTRYPOINT ["setpriv", "--reuid=1000", "--", "/app"]
`,
			WantViolations: 0,
		},

		{
			Name: "gosu in CMD with ENTRYPOINT does not suppress",
			Content: `FROM ubuntu:22.04
VOLUME /data
ENTRYPOINT ["/app"]
CMD ["gosu", "nobody"]
`,
			WantViolations: 1,
		},

		{
			Name: "inherited gosu ENTRYPOINT from parent stage suppresses",
			Content: `FROM ubuntu:22.04 AS base
ENTRYPOINT ["gosu", "postgres", "start"]

FROM base
VOLUME /var/lib/postgresql
CMD ["postgres"]
`,
			WantViolations: 0,
		},
		{
			Name: "child overrides inherited gosu ENTRYPOINT does not suppress",
			Content: `FROM ubuntu:22.04 AS base
ENTRYPOINT ["gosu", "postgres", "start"]

FROM base
ENTRYPOINT ["/app"]
VOLUME /data
CMD ["serve"]
`,
			WantViolations: 1,
		},

		// === Multi-stage builds ===
		{
			Name: "multi-stage: builder root + VOLUME, final non-root",
			Content: `FROM ubuntu:22.04 AS builder
USER root
VOLUME /build
RUN make

FROM alpine:3.20
USER 1000
COPY --from=builder /app /app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "multi-stage: final stage root + VOLUME",
			Content: `FROM golang:1.22 AS builder
RUN go build -o /app

FROM ubuntu:22.04
USER root
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 1,
		},

		// === Known non-root base images ===
		{
			Name: "distroless nonroot base + VOLUME",
			Content: `FROM gcr.io/distroless/static:nonroot
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "distroless debug-nonroot base + VOLUME",
			Content: `FROM gcr.io/distroless/base:debug-nonroot
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "chainguard base + VOLUME",
			Content: `FROM cgr.dev/chainguard/static:latest
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 0,
		},

		// === Stage-ref chain tests ===
		{
			Name: "chained stage-ref to distroless nonroot",
			Content: `FROM gcr.io/distroless/static:nonroot AS base

FROM base AS runtime

FROM runtime
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "chained stage-ref to chainguard",
			Content: `FROM cgr.dev/chainguard/static:latest AS base

FROM base
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "chained stage-ref with root USER in middle breaks chain",
			Content: `FROM gcr.io/distroless/static:nonroot AS base

FROM base AS middle
USER root

FROM middle
VOLUME /data
CMD ["/app"]
`,
			WantViolations: 1,
		},

		// === Relative path resolution ===
		{
			Name: "chained relative WORKDIR resolves to state path",
			Content: `FROM ubuntu:22.04
WORKDIR /var
WORKDIR lib/app
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"workdir /var/lib/app"},
		},
		{
			Name: "relative COPY destination resolves to state path",
			Content: `FROM ubuntu:22.04
WORKDIR /var
COPY config.conf lib/app/config.conf
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY destination /var/lib/app"},
		},
		{
			Name: "relative ADD destination resolves to state path",
			Content: `FROM ubuntu:22.04
WORKDIR /var
ADD data.tar.gz log/app/
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"ADD destination /var/log/app"},
		},
		{
			Name: "relative WORKDIR under non-state path is clean",
			Content: `FROM ubuntu:22.04
WORKDIR /app
WORKDIR config
CMD ["app"]
`,
			WantViolations: 0,
		},

		// === Inherited state from parent stages ===
		{
			Name: "inherited VOLUME from parent stage",
			Content: `FROM ubuntu:22.04 AS base
VOLUME /data

FROM base
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"inherited volume /data"},
		},
		{
			Name: "inherited WORKDIR state path from parent stage",
			Content: `FROM ubuntu:22.04 AS base
WORKDIR /var/lib/app

FROM base
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"inherited workdir /var/lib/app"},
		},
		{
			Name: "inherited VOLUME through chain of stages",
			Content: `FROM ubuntu:22.04 AS base
VOLUME /data

FROM base AS middle

FROM middle
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"inherited volume /data"},
		},
		{
			Name: "relative COPY resolves against inherited WORKDIR",
			Content: `FROM ubuntu:22.04 AS base
WORKDIR /var

FROM base
COPY app.conf lib/app/app.conf
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY destination /var/lib/app"},
		},

		// === Edge cases ===
		{
			Name: "minimal stage no stateful signals",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "WORKDIR with variable (skipped)",
			Content: `FROM ubuntu:22.04
WORKDIR $DATA_DIR
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "VOLUME /var/cache state path",
			Content: `FROM ubuntu:22.04
VOLUME /var/cache/apt
CMD ["app"]
`,
			WantViolations: 1,
		},
		{
			Name: "WORKDIR /var/run state path",
			Content: `FROM ubuntu:22.04
WORKDIR /var/run/app
CMD ["app"]
`,
			WantViolations: 1,
		},
		{
			Name: "inherits non-root from parent stage",
			Content: `FROM ubuntu:22.04 AS base
USER appuser

FROM base
VOLUME /data
CMD ["app"]
`,
			WantViolations: 0,
		},
	})
}

func TestIsStatePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"/data", true},
		{"/srv", true},
		{"/var/lib/mysql", true},
		{"/var/lib/postgresql/data", true},
		{"/var/log/app", true},
		{"/var/cache/apt", true},
		{"/var/run/app", true},
		{"/var/spool/mail", true},
		{"/app", false},
		{"/usr/bin", false},
		{"/home/appuser", false},
		{"/var/library", false}, // not /var/lib/
		{"/data/subfolder", false},
		{"/srv/data", false},
		{"$DATA_DIR", false}, // variable reference
		{"/var/lib", true},
		{"/var/log", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := isStatePath(tt.path); got != tt.want {
				t.Errorf("isStatePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestFindMkdirStatePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "simple mkdir state path",
			script: "mkdir -p /var/lib/data",
			want:   []string{"/var/lib/data"},
		},
		{
			name:   "mkdir non-state path",
			script: "mkdir -p /app/config",
			want:   nil,
		},
		{
			name:   "chained commands with mkdir",
			script: "apt-get update && mkdir -p /var/log/app && chmod 755 /var/log/app",
			want:   []string{"/var/log/app"},
		},
		{
			name:   "no mkdir",
			script: "echo hello",
			want:   nil,
		},
		{
			name:   "mkdir /data",
			script: "mkdir /data",
			want:   []string{"/data"},
		},
		{
			name:   "mkdir with long option --mode=755",
			script: "mkdir --mode=755 /var/lib/data",
			want:   []string{"/var/lib/data"},
		},
		{
			name:   "mkdir with -m option and separate argument",
			script: "mkdir -m 755 /var/lib/data",
			want:   []string{"/var/lib/data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findMkdirStatePaths(tt.script)
			if len(got) != len(tt.want) {
				t.Fatalf("findMkdirStatePaths(%q) = %v, want %v", tt.script, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("findMkdirStatePaths(%q)[%d] = %q, want %q", tt.script, i, got[i], tt.want[i])
				}
			}
		})
	}
}
