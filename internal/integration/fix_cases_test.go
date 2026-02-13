package integration

import (
	"fmt"
	"testing"
)

func fixCases(t *testing.T) []fixCase {
	t.Helper()

	mustSelectRules := func(rules ...string) []string {
		t.Helper()
		args, err := selectRules(rules...)
		if err != nil {
			t.Fatalf("build rule-selection args: %v", err)
		}
		return args
	}

	return []fixCase{
		{
			name:        "stage-name-casing",
			input:       "FROM alpine:3.18 AS Builder\nRUN echo hello\nFROM alpine:3.18\nCOPY --from=Builder /app /app\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "from-as-casing",
			input:       "FROM alpine:3.18 as builder\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "combined-stage-and-as-casing",
			input:       "FROM alpine:3.18 as Builder\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 2, // Both FromAsCasing and StageNameCasing
		},
		// DL3027: apt -> apt-get (regression test for line number consistency)
		{
			name:        "dl3027-apt-to-apt-get",
			input:       "FROM ubuntu:22.04\nRUN apt update && apt install -y curl\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Single violation with multiple edits
		},
		// DL3046: useradd with high UID -> useradd -l
		{
			name:        "dl3046-useradd-high-uid",
			input:       "FROM debian:bookworm\nRUN useradd -u 123456 appuser\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		// DL3047: wget -> wget --progress=dot:giga
		{
			name:        "dl3047-wget-progress",
			input:       "FROM ubuntu:22.04\nRUN wget http://example.com/file.tar.gz\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		// DL3047 + DL4001 + prefer-add-unpack: all three cooperating rules fire.
		// prefer-add-unpack (priority 95) applies first and replaces the wget|tar
		// with ADD --unpack, making DL3047 (priority 96) moot on that line.
		// The standalone wget (no tar) only triggers DL3047 → --progress inserted.
		// DL4001 has no fix. --fail-level=none prevents unfixed DL4001 from failing.
		{
			name: "dl3047-cross-rules",
			input: "FROM ubuntu:22.04\n" +
				"RUN wget http://example.com/archive.tar.gz | tar -xz -C /opt\n" +
				"RUN wget http://example.com/config.json -O /etc/app/config.json\n" +
				"RUN curl -fsSL http://example.com/script.sh | sh\n",
			args:        []string{"--fix", "--fix-unsafe", "--fail-level", "none"},
			wantApplied: 4, // Current fixer reports four applied fixes for this combined scenario.
		},
		// DL3003: cd -> WORKDIR (regression test for line number consistency)
		{
			// DL3003 fix is FixSuggestion (not FixSafe) because WORKDIR creates
			// the directory if it doesn't exist, while RUN cd fails.
			// Requires both --fix and --fix-unsafe since FixSuggestion > FixSafe.
			name:        "dl3003-cd-to-workdir",
			input:       "FROM ubuntu:22.04\nRUN cd /app\n",
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 1,
		},
		// DL4005: ln /bin/sh -> SHELL instruction
		{
			// DL4005 fix is FixSuggestion: SHELL affects Docker RUN execution
			// while ln affects the container filesystem — different semantics.
			name:  "dl4005-ln-to-shell",
			input: "FROM ubuntu:22.04\nRUN ln -sf /bin/bash /bin/sh\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("hadolint/DL4005")...),
			wantApplied: 1,
		},
		{
			name:  "dl4005-ln-in-chain",
			input: "FROM ubuntu:22.04\nRUN apt-get update && ln -sf /bin/bash /bin/sh && echo done\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("hadolint/DL4005")...),
			wantApplied: 1,
		},
		// DL4006: Add SHELL with -o pipefail before RUN with pipe
		{
			name:  "dl4006-add-pipefail",
			input: "FROM ubuntu:22.04\nRUN wget -O - https://some.site | wc -l > /number\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("hadolint/DL4006")...),
			wantApplied: 1,
		},
		// DL4006 + prefer-run-heredoc cross-rule interaction:
		// The chained pipe RUN (heredoc candidate) gets converted to a heredoc
		// with "set -o pipefail" inside the body, avoiding a SHELL instruction.
		// The simple pipe RUN gets the standard DL4006 SHELL injection fix.
		// --fail-level=none prevents unfixed DL4006 violations from failing.
		{
			name: "dl4006-cross-heredoc",
			input: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y curl && curl -fsSL https://example.com/setup.sh | bash\n" +
				"RUN wget -O - https://some.site | wc -l > /number\n",
			args: append(
				[]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("hadolint/DL4006", "tally/prefer-run-heredoc")...),
			wantApplied: 2, // SHELL injection + heredoc with pipefail
		},
		// NoEmptyContinuation: Remove empty lines in continuations
		{
			name:        "no-empty-continuation-single",
			input:       "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add curl\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "no-empty-continuation-multiple",
			input:       "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add \\\n\n    curl\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Single violation covers all empty lines
		},
		// ConsistentInstructionCasing: Normalize instruction casing
		{
			name:        "consistent-instruction-casing-to-upper",
			input:       "FROM alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n",
			args:        []string{"--fix"},
			wantApplied: 2, // Two instructions need fixing
		},
		{
			name:        "consistent-instruction-casing-to-lower",
			input:       "from alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Only COPY needs fixing
		},
		// Multiple fixes with line shift: DL3003 splits one line into two,
		// then DL3027 fix on a later line must still apply correctly.
		// The fixer applies edits from end to start to handle position drift.
		{
			name: "multi-fix-line-shift",
			input: `FROM ubuntu:22.04
RUN cd /app && make build
RUN apt install curl
`,
			args:        []string{"--fix", "--fix-unsafe", "--ignore", "tally/prefer-run-heredoc"},
			wantApplied: 2, // DL3003 + DL3027
		},
		// LegacyKeyValueFormat: Replace legacy "ENV key value" with "ENV key=value"
		{
			name:        "legacy-key-value-format-simple",
			input:       "FROM alpine:3.18\nENV foo bar\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "legacy-key-value-format-multi-word",
			input:       "FROM alpine:3.18\nENV MY_VAR hello world\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "legacy-key-value-format-label",
			input:       "FROM alpine:3.18\nLABEL maintainer John Doe\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "legacy-key-value-format-multiple",
			input:       "FROM alpine:3.18\nENV foo bar\nLABEL version 1.0\n",
			args:        []string{"--fix"},
			wantApplied: 2,
		},
		// ExposeProtoCasing: Lowercase protocol in EXPOSE
		{
			name:        "expose-proto-casing-single",
			input:       "FROM alpine:3.18\nEXPOSE 8080/TCP\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "expose-proto-casing-multiple-ports",
			input:       "FROM alpine:3.18\nEXPOSE 80/TCP 443/UDP\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Current fixer reports one applied fix for the EXPOSE line.
		},
		// ExposeProtoCasing + ConsistentInstructionCasing overlap: both rules edit the same EXPOSE line
		{
			name:  "expose-proto-casing-with-instruction-casing",
			input: "FROM alpine:3.18\nexpose 8080/TCP\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/ExposeProtoCasing",
				"--select", "buildkit/ConsistentInstructionCasing",
			},
			wantApplied: 2, // instruction casing + protocol casing
		},
		// MaintainerDeprecated: Replace MAINTAINER with LABEL
		{
			name:        "maintainer-deprecated",
			input:       "FROM alpine:3.18\nMAINTAINER John Doe <john@example.com>\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "json-args-recommended",
			input:       "FROM alpine:3.18\nCMD echo hello\n",
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 1,
		},
		// InvalidDefinitionDescription: Add empty line between non-description comment and instruction
		// Multiple violations to verify fixes apply correctly with line shifts
		{
			name: "invalid-definition-description-multiple",
			input: `# check=experimental=InvalidDefinitionDescription
# bar this is the bar
ARG foo=bar
# BasE this is the BasE image
FROM scratch AS base
# definitely a bad comment
ARG version=latest
# definitely a bad comment
ARG baz=quux
`,
			args:        []string{"--fix", "--select", "buildkit/InvalidDefinitionDescription"},
			wantApplied: 4, // Four violations: lines 3, 5, 7, 9
		},
		// Consistent indentation: add indentation to multi-stage commands
		{
			name:        "consistent-indentation-multi-stage",
			input:       "FROM alpine:3.20 AS builder\nRUN echo build\nFROM scratch\nCOPY --from=builder /app /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2,
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation: remove indentation from single-stage
		{
			name:        "consistent-indentation-single-stage",
			input:       "FROM alpine:3.20\n\tRUN echo hello\n\tCOPY . /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2,
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation: remove space indentation from single-stage
		{
			name:        "consistent-indentation-single-stage-spaces",
			input:       "FROM alpine:3.20\n    RUN echo hello\n    COPY . /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2,
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation: multi-line continuation lines get aligned to 1 tab
		{
			name: "consistent-indentation-multi-line-continuation",
			input: "FROM ubuntu:22.04 AS builder\n" +
				"ARG LAMBDA_TASK_ROOT=/var/task\n" +
				"RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf \\\n" +
				"         --mount=type=cache,target=/root/.cache/pip \\\n" +
				"--mount=type=secret,id=uvtoml,target=/root/.config/uv/uv.toml \\\n" +
				"--mount=type=bind,source=requirements.txt,target=${LAMBDA_TASK_ROOT}/requirements.txt \\\n" +
				"     --mount=type=cache,target=/root/.cache/uv \\\n" +
				"  pip install uv==0.9.24 && \\\n" +
				"      uv pip install --system -r requirements.txt\n" +
				"FROM scratch\n" +
				"COPY --from=builder /app /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 3, // Current fixer reports three applied fixes in this fixture.
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation + ConsistentInstructionCasing: both fix the same line
		// Indentation adds a tab, casing fixes "run" -> "RUN" on the same line
		{
			name:  "consistent-indentation-with-casing-fix",
			input: "FROM alpine:3.20 AS builder\nrun echo build\nFROM scratch\ncopy --from=builder /app /app\n",
			args: []string{
				"--fix",
				"--select", "tally/consistent-indentation",
				"--select", "buildkit/ConsistentInstructionCasing",
			},
			wantApplied: 4, // Current fixer reports four applied fixes in this combined fixture.
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// InvalidDefinitionDescription enabled via config file instead of Dockerfile directive
		// Verifies that experimental rules can be enabled by setting severity in tally.toml
		{
			name: "invalid-definition-description-via-config",
			input: `# This comment doesn't match the ARG name
ARG foo=bar
# Another mismatched comment
FROM scratch AS base
`,
			args:        []string{"--fix"},
			wantApplied: 2,
			config: `[rules.buildkit.InvalidDefinitionDescription]
severity = "error"
`,
		},

		// MultipleInstructionsDisallowed: Comment out duplicate CMD/ENTRYPOINT
		{
			name:  "multiple-cmd-fix",
			input: "FROM alpine:3.21\nCMD echo \"first\"\nRUN echo hello\nCMD echo \"second\"\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 1,
		},
		{
			name:  "multiple-entrypoint-fix",
			input: "FROM alpine:3.21\nENTRYPOINT [\"/bin/bash\"]\nENTRYPOINT [\"/bin/sh\"]\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 1,
		},
		{
			name:  "multiple-cmd-three",
			input: "FROM alpine:3.21\nCMD echo first\nCMD echo second\nCMD echo third\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 2,
		},
		{
			name:  "multiple-healthcheck-fix",
			input: "FROM alpine:3.21\nHEALTHCHECK CMD curl -f http://localhost/\nHEALTHCHECK --interval=60s CMD wget -qO- http://localhost/\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 1,
		},
		// Cross-rule interaction: MultipleInstructionsDisallowed + ConsistentInstructionCasing + JSONArgsRecommended
		// all fire on the same duplicate CMD line. MultipleInstructionsDisallowed has priority -1 (applied
		// before cosmetic fixes at priority 0), so it comments out the earlier cmd on line 2 first.
		// Casing and JSON fixes on line 2 are then skipped (conflict with the whole-line edit).
		// Remaining non-conflicting fixes still apply on other lines.
		//   Line 2: commented out by MultipleInstructionsDisallowed (priority -1)
		//   Line 3: JSON fix (echo second→["echo","second"])
		//   Line 4: casing fix (entrypoint→ENTRYPOINT)
		//   Skipped: ConsistentInstructionCasing + JSONArgsRecommended on line 2 (conflict)
		{
			name: "multiple-instructions-cross-rules",
			input: "FROM alpine:3.21\n" +
				"cmd echo first\n" +
				"CMD echo second\n" +
				"entrypoint [\"/bin/sh\"]\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules(
					"buildkit/MultipleInstructionsDisallowed",
					"buildkit/ConsistentInstructionCasing",
					"buildkit/JSONArgsRecommended",
				)...),
			wantApplied: 3, // comment-out line 2 + JSON line 3 + casing line 4
		},

		// === Heredoc fix tests ===

		// prefer-copy-heredoc: single RUN echo redirect → COPY heredoc
		{
			name:  "prefer-copy-heredoc-single-echo",
			input: "FROM ubuntu:22.04\nRUN echo 'hello world' > /app/greeting.txt\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-copy-heredoc: consecutive RUNs writing to same file → single COPY heredoc
		{
			name: "prefer-copy-heredoc-consecutive-writes",
			input: "FROM ubuntu:22.04\n" +
				"RUN echo 'line1' > /app/data.txt\n" +
				"RUN echo 'line2' >> /app/data.txt\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-run-heredoc: 3 consecutive RUNs → heredoc RUN
		{
			name: "prefer-run-heredoc-consecutive",
			input: "FROM ubuntu:22.04\n" +
				"RUN apt-get update\n" +
				"RUN apt-get install -y curl\n" +
				"RUN apt-get install -y git\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-run-heredoc: chained commands → heredoc RUN
		{
			name:  "prefer-run-heredoc-chained",
			input: "FROM ubuntu:22.04\nRUN echo step1 && echo step2 && echo step3\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 1,
		},

		// Both heredoc rules enabled together: prefer-copy-heredoc takes priority (99) over prefer-run-heredoc (100).
		// The file-creation RUN is handled by prefer-copy-heredoc; the consecutive RUNs by prefer-run-heredoc.
		{
			name: "heredoc-both-rules-combined",
			input: "FROM ubuntu:22.04\n" +
				"RUN echo 'server {}' > /etc/nginx.conf\n" +
				"RUN apt-get update\n" +
				"RUN apt-get install -y curl\n" +
				"RUN apt-get install -y git\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 2,
		},

		// Heredoc + indentation: multi-stage with both heredoc rules + indentation.
		// The indentation fix (priority 50) applies first, then run-heredoc (100).
		// After indentation adds tabs, the heredoc resolver should preserve them.
		{
			name: "heredoc-with-indentation-multi-stage",
			input: "FROM ubuntu:22.04 AS builder\n" +
				"RUN apt-get update\n" +
				"RUN apt-get install -y curl\n" +
				"RUN apt-get install -y git\n" +
				"FROM alpine:3.20\n" +
				"COPY --from=builder /usr/bin/curl /usr/bin/curl\n" +
				"RUN echo 'done'\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--ignore", "*",
				"--select", "tally/consistent-indentation",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 6, // 5 indentation fixes + 1 heredoc
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},

		// prefer-copy-heredoc: echo with chmod → COPY --chmod heredoc
		{
			name:  "prefer-copy-heredoc-with-chmod",
			input: "FROM ubuntu:22.04\nRUN echo '#!/bin/sh' > /entrypoint.sh && chmod +x /entrypoint.sh\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},
		{
			name: "ai-autofix-prefer-multi-stage-build",
			input: `FROM golang:1.22-alpine
WORKDIR /src
COPY . .
RUN go build -o /out/app ./cmd/app
CMD ["app"]
`,
			args: append([]string{
				"--fix",
				"--fix-unsafe",
				"--fix-rule", "tally/prefer-multi-stage-build",
			}, mustSelectRules("tally/prefer-multi-stage-build", "tally/no-unreachable-stages")...),
			wantApplied: 1,
			config: fmt.Sprintf(`[ai]
enabled = true
timeout = "10s"
redact-secrets = false
command = ['%s', '-mode=multistage']

[rules.tally.prefer-multi-stage-build]
fix = "explicit"
`, acpAgentPath),
		},
	}
}
