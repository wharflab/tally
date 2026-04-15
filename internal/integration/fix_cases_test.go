package integration

import (
	"fmt"
	"testing"
)

//nolint:funlen // test table
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
		// DL3020: ADD -> COPY for local files
		{
			name:        "dl3020-add-to-copy",
			input:       "FROM ubuntu:22.04\nADD file.txt /app/\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "dl3020-add-to-copy-multiple",
			input:       "FROM ubuntu:22.04\nADD file1.txt /app/\nADD file2.txt /app/\n",
			args:        []string{"--fix"},
			wantApplied: 2,
		},
		{
			name:        "dl3020-add-to-copy-with-flags",
			input:       "FROM ubuntu:22.04\nADD --chown=app:app src/ /app/\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		// DL3027: apt -> apt-get (regression test for line number consistency)
		{
			name:        "dl3027-apt-to-apt-get",
			input:       "FROM ubuntu:22.04\nRUN apt update && apt install -y curl\n",
			args:        []string{"--fix"},
			wantApplied: 2, // One fix per apt occurrence
		},
		// ShellCheck SC2086: echo $var -> echo "$var"
		{
			name:  "shellcheck-sc2086-quote",
			input: "FROM alpine:3.20\nRUN echo $1\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("shellcheck/SC2086")...),
			wantApplied: 1,
		},
		{
			name: "shellcheck-sc1040-tabs-only-terminator",
			input: "FROM alpine:3.20\n" +
				"\n" +
				"RUN <<SCRIPT\n" +
				"cat <<-EOF\n" +
				"hello\n" +
				"  EOF\n" +
				"EOF\n" +
				"SCRIPT\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("shellcheck/SC1040")...),
			wantApplied: 1,
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
		// DL3047 + DL4001 + prefer-add-unpack + prefer-wget-config all cooperate.
		// prefer-add-unpack (priority 95) applies first and replaces the wget|tar
		// with ADD --unpack, making DL3047 (priority 96) moot on that line.
		// The standalone wget (no tar) still triggers DL3047 and prefer-wget-config.
		// DL4001 has no fix. --fail-level=none prevents unfixed DL4001 from failing.
		{
			name: "dl3047-cross-rules",
			input: "FROM ubuntu:22.04\n" +
				"RUN wget http://example.com/archive.tar.gz | tar -xz -C /opt\n" +
				"RUN wget http://example.com/config.json -O /etc/app/config.json\n" +
				"RUN curl -fsSL http://example.com/script.sh | sh\n",
			args:        []string{"--fix", "--fix-unsafe", "--fail-level", "none"},
			wantApplied: 5, // prefer-add-unpack + DL3047 --progress + DL4006 SHELL + curl/wget config blocks.
		},
		// curl-should-follow-redirects: insert --location after curl
		// Also triggers prefer-curl-config (all rules enabled) → 2 fixes.
		{
			name:        "curl-should-follow-redirects",
			input:       "FROM ubuntu:22.04\nRUN curl -fsSo /tmp/file https://example.com/file\n",
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 2,
		},
		// Cross-rule: curl-should-follow-redirects + prefer-add-unpack on the same RUN.
		// Both rules fire (both elevated to error), but curl-should-follow-redirects
		// suppresses its fix when tar extraction is present (prefer-add-unpack
		// territory). Only the prefer-add-unpack fix applies (RUN → ADD --unpack).
		{
			name: "curl-should-follow-redirects-cross-prefer-add-unpack",
			input: "FROM ubuntu:22.04\n" +
				"RUN curl -fsSo /tmp/go.tar.gz https://go.dev/dl/go1.22.0.linux-amd64.tar.gz && " +
				"tar -xzf /tmp/go.tar.gz -C /usr/local\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/curl-should-follow-redirects", "tally/prefer-add-unpack")...),
			wantApplied: 1,
			config: `[rules.tally.curl-should-follow-redirects]
severity = "error"

[rules.tally.prefer-add-unpack]
severity = "error"
`,
		},
		// Cross-rule: curl-should-follow-redirects + newline-per-chained-call on the same RUN.
		// The --location insert (priority 0) shifts columns on the same line;
		// the newline split (priority 97) inserts \\\n which recordColumnShift
		// skips. Both fixes should compose correctly via column shift tracking.
		{
			name: "curl-should-follow-redirects-cross-newline-per-chained-call",
			input: "FROM ubuntu:22.04\n" +
				"RUN curl -fsSo /tmp/file https://example.com/file && chmod +x /tmp/file\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/curl-should-follow-redirects", "tally/newline-per-chained-call")...),
			wantApplied: 2,
			config: `[rules.tally.curl-should-follow-redirects]
severity = "error"

[rules.tally.newline-per-chained-call]
severity = "error"
`,
		},
		// prefer-curl-config: insert COPY heredoc + ENV before first curl usage
		{
			name:  "prefer-curl-config",
			input: "FROM ubuntu:22.04\nRUN curl -fsSL https://example.com/install.sh | bash\n",
			args: append([]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/prefer-curl-config")...),
			wantApplied: 1,
		},
		{
			name:  "prefer-wget-config",
			input: "FROM ubuntu:22.04\nRUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			args: append([]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/prefer-wget-config")...),
			wantApplied: 1,
		},
		// Cross-rule: prefer-curl-config (93) + curl-should-follow-redirects (0)
		// on the same stage. Both should apply: curl-config inserts COPY/ENV
		// before the RUN, curl-follow-redirects inserts --location inside the RUN.
		{
			name: "prefer-curl-config-cross-curl-redirects",
			input: "FROM ubuntu:22.04\n" +
				"RUN curl -fsSo /tmp/file https://example.com/file\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/prefer-curl-config", "tally/curl-should-follow-redirects")...),
			wantApplied: 2,
		},
		// Cross-rule: prefer-curl-config should defer on pure download+extract
		// territory when prefer-add-unpack is enabled, so only ADD --unpack applies.
		{
			name: "prefer-curl-config-cross-prefer-add-unpack",
			input: "FROM ubuntu:22.04\n" +
				"RUN curl -fsSL https://example.com/archive.tar.gz | tar -xz -C /opt\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/prefer-curl-config", "tally/prefer-add-unpack")...),
			wantApplied: 1,
		},
		// Cross-rule: prefer-curl-config (93) + prefer-wget-config (94)
		// insert at the same location before the install RUN. Zero-width line
		// inserts must compose deterministically without conflict.
		{
			name: "prefer-wget-config-cross-curl-config",
			input: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y ca-certificates curl wget\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n" +
				"RUN wget https://example.com/config.json -O /etc/app/config.json\n",
			args: append([]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/prefer-curl-config", "tally/prefer-wget-config")...),
			wantApplied: 2,
		},
		// Cross-rule: prefer-wget-config should defer on pure download+extract
		// territory when prefer-add-unpack is enabled, so only ADD --unpack applies.
		{
			name: "prefer-wget-config-cross-prefer-add-unpack",
			input: "FROM ubuntu:22.04\n" +
				"RUN wget http://example.com/archive.tar.gz | tar -xz -C /opt\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/prefer-wget-config", "tally/prefer-add-unpack")...),
			wantApplied: 1,
		},
		{
			name:  "prefer-telemetry-opt-out",
			input: "FROM node:22\nRUN bun install && next build\n",
			args: append([]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/prefer-telemetry-opt-out")...),
			wantApplied: 1,
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
			args: append([]string{"--fix", "--fix-unsafe"},
				mustSelectRules("hadolint/DL3003", "hadolint/DL3027", "tally/prefer-curl-config")...),
			wantApplied: 3, // DL3003 + DL3027 + prefer-curl-config
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

		// prefer-copy-heredoc: literal ~/ target resolves against the effective USER and stays unsafe
		{
			name: "prefer-copy-heredoc-tilde-home-root",
			input: "FROM ubuntu:22.04\n" +
				"RUN echo '#!/bin/bash' > ~/.bashrc\n",
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

		// prefer-copy-heredoc: BuildKit heredoc piped to cat → COPY heredoc
		{
			name:  "prefer-copy-heredoc-buildkit-heredoc-cat",
			input: "FROM ubuntu:22.04\nRUN <<EOF cat > /aria2/aria2.conf\ndir=/downloads\nmax-concurrent-downloads=16\nEOF\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-copy-heredoc: BuildKit heredoc piped to tee → COPY heredoc
		{
			name:  "prefer-copy-heredoc-buildkit-heredoc-tee",
			input: "FROM ubuntu:22.04\nRUN <<EOF tee /etc/app.conf\n[app]\nkey=value\nEOF\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-copy-heredoc: printf with escape sequences → COPY heredoc
		{
			name:  "prefer-copy-heredoc-printf-escapes",
			input: "FROM ubuntu:22.04\nRUN printf '#ifndef H\\n#define H\\nint f(void);\\n#endif\\n' > /usr/include/stub.h\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-copy-heredoc: printf with chmod → COPY heredoc with --chmod
		{
			name:  "prefer-copy-heredoc-printf-chmod",
			input: "FROM ubuntu:22.04\nRUN printf '#!/bin/sh\\nexec app\\n' > /app/run.sh && chmod +x /app/run.sh\n",
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
			wantApplied: 4, // cache mounts (3) + curl config (1); heredoc skipped (curl config breaks consecutiveness)
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
		{
			name: "prefer-package-cache-mounts",
			input: "FROM ubuntu:24.04\n" +
				"RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf apt-get update && apt-get install -y gcc && apt-get clean\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-with-prefer-run-heredoc",
			input: "FROM node:20\n" +
				"RUN npm install && npm cache clean --force\n" +
				"RUN npm ci && npm cache clean --force\n" +
				"RUN npm install left-pad && npm cache clean --force\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 4,
		},
		{
			name: "prefer-package-cache-mounts-no-cache-flags",
			input: "FROM python:3.13\n" +
				"RUN pip install --no-cache-dir -r requirements.txt && pip cache purge\n" +
				"RUN uv sync --no-cache --frozen && uv cache clean\n" +
				"RUN bun install --no-cache && bun pm cache rm\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 4,
		},
		{
			name: "prefer-package-cache-mounts-uv-no-cache-env",
			input: "FROM python:3.13\n" +
				"ENV UV_NO_CACHE=1\n" +
				"RUN uv sync --frozen\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-uv-python-install",
			input: "FROM python:3.13\n" +
				"RUN uv python install 3.12\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-pip-no-cache-dir-env",
			input: "FROM python:3.13\n" +
				"ENV PIP_NO_CACHE_DIR=1\n" +
				"RUN pip install -r requirements.txt\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			// Cross-rule interaction: prefer-package-cache-mounts (priority 90) deletes
			// "ENV UV_NO_CACHE 1" and adds cache mount before LegacyKeyValueFormat (priority 91)
			// tries to reformat it. The LegacyKeyValueFormat fix is then skipped (range deleted).
			name:  "prefer-package-cache-mounts-uv-no-cache-legacy-with-legacy-key-value",
			input: "FROM python:3.13\nENV UV_NO_CACHE 1\nRUN uv sync --frozen\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
				"--select", "buildkit/LegacyKeyValueFormat",
			},
			wantApplied: 1, // cache-mounts wins; LegacyKeyValueFormat skipped (ENV already deleted)
		},
		{
			name: "prefer-package-cache-mounts-multiline-env-removal",
			input: "FROM python:3.13\n" +
				"ENV \\\n" +
				"    UV_NO_CACHE=1\n" +
				"RUN uv sync --frozen\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-npm-config-cache-env",
			input: "FROM node:20\n" +
				"ENV npm_config_cache=/tmp/npm-cache\n" +
				"RUN npm install\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-dnf-heredoc-cleanup",
			input: "FROM amazonlinux:2023\n" +
				"RUN --mount=type=cache,target=/var/cache/dnf,id=dnf <<'EOF'\n" +
				"dnf -y update\n" +
				"dnf -y install java-21-amazon-corretto-headless\n" +
				"dnf clean all\n" +
				"EOF\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-dnf-heredoc-tab-strip-mid-cleanup",
			input: "FROM amazonlinux:2023\n" +
				"RUN --mount=type=cache,target=/var/cache/dnf,id=dnf <<-EOF\n" +
				"\tdnf -y update\n" +
				"\tdnf clean all\n" +
				"\tdnf -y install java-21-amazon-corretto-headless\n" +
				"EOF\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-apt-heredoc-mutated-plus-new-mount",
			input: "FROM ubuntu:24.04\n" +
				"RUN --mount=type=cache,target=/var/cache/apt,id=apt <<EOF\n" +
				"apt-get update && apt-get install -y gcc && apt-get clean\n" +
				"EOF\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 1,
		},
		{
			name: "prefer-package-cache-mounts-bun-install-cache-dir-env",
			input: "FROM oven/bun:1.2\n" +
				"ENV BUN_INSTALL_CACHE_DIR=/tmp/bun-cache\n" +
				"RUN bun install\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 2,
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
			wantApplied: 5, // copy heredoc (1) + cache mounts (3) + curl config (1); run-heredoc skipped (curl config breaks consecutiveness)
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

		// Newline between instructions: grouped mode (default) - insert blank lines.
		// The async resolver generates all edits in one pass, so only 1 fix is recorded.
		{
			name:  "newline-between-instructions-grouped",
			input: "FROM alpine:3.20\nRUN echo hello\nENV FOO=bar\nENV BAZ=qux\n\nCOPY . /app\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-between-instructions")...),
			wantApplied: 1,
		},
		// Newline between instructions: grouped mode - excess blanks between different types
		// Regression: resolver must trim to exactly 1 blank line, not remove all.
		{
			name:  "newline-between-instructions-grouped-excess-blanks",
			input: "FROM alpine:3.20\n\n\n\nRUN echo hello\n\nENV FOO=bar\nENV BAZ=qux\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-between-instructions")...),
			wantApplied: 1,
		},
		// Newline between instructions: always mode
		{
			name:  "newline-between-instructions-always",
			input: "FROM alpine:3.20\nRUN echo hello\nENV FOO=bar\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-between-instructions")...),
			wantApplied: 1,
			config: `[rules.tally.newline-between-instructions]
mode = "always"
`,
		},
		// Newline between instructions: never mode
		{
			name:  "newline-between-instructions-never",
			input: "FROM alpine:3.20\n\nRUN echo hello\n\nENV FOO=bar\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-between-instructions")...),
			wantApplied: 1,
			config: `[rules.tally.newline-between-instructions]
mode = "never"
`,
		},
		// Newline between instructions: grouped mode - indented comment between different-type
		// instructions with correct spacing should not trigger a fix.
		// Regression: the comment must not be deleted when the gap is already correct.
		{
			name: "newline-between-instructions-grouped-indented-comment",
			input: "FROM dhi.io/debian-base:trixie-dev AS builder\n\n" +
				"ENV DEBIAN_FRONTEND=noninteractive\n\n" +
				"RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked \\\n" +
				"    --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked \\\n" +
				"    apt-get update \\\n" +
				"    && apt-get install -y build-essential curl git jq unzip xz-utils zstd\n\n" +
				"    # Haskell dependencies\n\n" +
				"ARG GHC_WASM_META_COMMIT\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-between-instructions")...),
			wantApplied: 0,
		},

		// Newline between instructions: grouped mode - same-type instructions
		// separated by a comment should not trigger a fix.
		// Regression: the resolver must skip same-type pairs with PrevComment.
		{
			name: "newline-between-instructions-grouped-comment-separator",
			input: "FROM alpine:3.20\n\n# Add Julia to PATH\nENV PATH=/usr/local/julia/bin:$PATH \\\n" +
				"    LD_LIBRARY_PATH=/usr/local/julia/lib/julia\n\n" +
				"# Target x86_64\nENV JULIA_CPU_TARGET=\"haswell\"\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-between-instructions")...),
			wantApplied: 0,
		},

		// No trailing spaces: remove trailing whitespace from multiple lines
		{
			name:  "no-trailing-spaces",
			input: "FROM alpine:3.20   \nRUN echo hello  \n# comment \nCOPY . /app\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-trailing-spaces")...),
			wantApplied: 3,
		},
		// No trailing spaces: ignore-comments skips # lines
		{
			name:  "no-trailing-spaces-ignore-comments",
			input: "FROM alpine:3.20   \nRUN echo hello  \n# comment \nCOPY . /app\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-trailing-spaces")...),
			wantApplied: 2, // comment line skipped
			config: `[rules.tally.no-trailing-spaces]
ignore-comments = true
`,
		},
		// No trailing spaces: skip-blank-lines skips whitespace-only lines
		{
			name:  "no-trailing-spaces-skip-blank-lines",
			input: "FROM alpine:3.20   \n   \nRUN echo hello\nCOPY . /app\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-trailing-spaces")...),
			wantApplied: 1, // blank line skipped, only FROM fixed
			config: `[rules.tally.no-trailing-spaces]
skip-blank-lines = true
`,
		},

		// No multi spaces: replace runs of multiple spaces with single space
		{
			name:  "no-multi-spaces",
			input: "FROM  alpine:3.20\nRUN  echo  hello\nCOPY  . /app\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-multi-spaces")...),
			wantApplied: 3,
		},
		{
			name:  "no-multi-spaces-heredoc-skipped",
			input: "FROM alpine:3.20\nRUN <<EOF\necho   hello\nEOF\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-multi-spaces")...),
			wantApplied: 0,
		},
		// No multi spaces: backslash continuations preserved, only extra spaces removed
		{
			name: "no-multi-spaces-continuation-lines",
			input: "FROM alpine:3.20\n" +
				"RUN apt-get  update \\\n" +
				"    &&  apt-get install -y  curl \\\n" +
				"    && rm -rf  /var/lib/apt/lists/*\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-multi-spaces")...),
			wantApplied: 3, // one violation per line (L2, L3, L4)
		},

		// EOL last: add missing final newline
		{
			name:  "eol-last-always",
			input: "FROM alpine:3.20\nRUN echo hello",
			args: append([]string{"--fix"},
				mustSelectRules("tally/eol-last")...),
			wantApplied: 1,
		},
		// EOL last: file already ends with newline — no fix
		{
			name:  "eol-last-already-ok",
			input: "FROM alpine:3.20\nRUN echo hello\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/eol-last")...),
			wantApplied: 0,
		},
		// EOL last: "never" mode removes trailing newline
		{
			name:  "eol-last-never",
			input: "FROM alpine:3.20\nRUN echo hello\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/eol-last")...),
			wantApplied: 1,
			config:      "[rules.tally.eol-last]\nmode = \"never\"\n",
		},
		// EOL last cross no-multiple-empty-lines: only no-multiple-empty-lines applies in default "always" mode
		{
			name:  "eol-last-cross-no-multiple-empty-lines",
			input: "FROM alpine:3.20\nRUN echo hello\n\n\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/eol-last", "tally/no-multiple-empty-lines")...),
			wantApplied: 1, // only no-multiple-empty-lines fires (file ends with \n, so eol-last "always" is satisfied)
		},
		// EOL last "never" standalone: removes all trailing newlines in one pass
		{
			name:  "eol-last-never-multiple-trailing",
			input: "FROM alpine:3.20\nRUN echo hello\n\n\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/eol-last")...),
			wantApplied: 1, // single violation with 3 edits (one per trailing \n)
			config:      "[rules.tally.eol-last]\nmode = \"never\"\n",
		},

		// No multiple empty lines: remove excess blank lines
		{
			name:  "no-multiple-empty-lines",
			input: "FROM alpine:3.20\n\n\nRUN echo hello\n\n\n\nCOPY . /app\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-multiple-empty-lines")...),
			wantApplied: 2, // two groups of excess blank lines
		},
		// No multiple empty lines: remove blank lines at BOF
		{
			name:  "no-multiple-empty-lines-bof",
			input: "\n\nFROM alpine:3.20\nRUN echo hello\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-multiple-empty-lines")...),
			wantApplied: 1,
		},
		// No multiple empty lines: remove blank lines at EOF
		{
			name:  "no-multiple-empty-lines-eof",
			input: "FROM alpine:3.20\nRUN echo hello\n\n\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/no-multiple-empty-lines")...),
			wantApplied: 1,
		},

		// Cross-rule: no-multiple-empty-lines fix makes max-lines violation stale.
		// The input has 8 lines (with skip-blank-lines=false). After fixing the
		// excess blank lines (3→1 between instructions), the file drops to 6 lines,
		// below the max of 7. The max-lines violation should be suppressed via
		// PostFixRevalidator rather than reported as stale.
		{
			name: "no-multiple-empty-lines-revalidate-max-lines",
			input: "FROM alpine:3.20\n" +
				"RUN echo hello\n" +
				"\n\n\n" + // 3 blank lines (2 excess)
				"COPY . /app\n" +
				"CMD [\"./app\"]\n",
			args: append([]string{"--fix"},
				mustSelectRules(
					"tally/no-multiple-empty-lines",
					"tally/max-lines",
				)...),
			config:      "[rules.tally.max-lines]\nmax = 7\nskip-blank-lines = false\n",
			wantApplied: 1, // only no-multiple-empty-lines fix; max-lines has no fix
		},

		// Epilogue order: reorder CMD and ENTRYPOINT
		{
			name: "epilogue-order-basic",
			input: "FROM alpine:3.20\n" +
				"RUN echo hello\n" +
				"CMD [\"serve\"]\n" +
				"ENTRYPOINT [\"/app\"]\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/epilogue-order")...),
			wantApplied: 1,
		},
		// Epilogue order: scattered epilogue instructions
		{
			name: "epilogue-order-scattered",
			input: "FROM alpine:3.20\n" +
				"CMD [\"serve\"]\n" +
				"RUN echo hello\n" +
				"HEALTHCHECK CMD curl -f http://localhost/\n" +
				"ENTRYPOINT [\"/app\"]\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/epilogue-order")...),
			wantApplied: 1,
		},
		// Epilogue order: multi-stage, only final stage fixed
		{
			name: "epilogue-order-multi-stage",
			input: "FROM golang:1.21 AS builder\n" +
				"RUN go build -o /app\n" +
				"FROM alpine:3.20\n" +
				"COPY --from=builder /app /app\n" +
				"CMD [\"serve\"]\n" +
				"ENTRYPOINT [\"/app\"]\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/epilogue-order")...),
			wantApplied: 1,
		},
		// Epilogue order: all-epilogue stage with blank lines after FROM
		{
			name: "epilogue-order-all-epilogue",
			input: "FROM alpine:3.20\n" +
				"\n" +
				"CMD [\"serve\"]\n" +
				"ENTRYPOINT [\"/app\"]\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/epilogue-order")...),
			wantApplied: 1,
		},
		// Cross-rule: epilogue-order + newline-between-instructions compose correctly.
		// epilogue-order (priority 175) moves instructions to end adjacent,
		// then newline-between-instructions (priority 200) normalizes blank lines.
		// Result is stable with no fix-loop.
		{
			name: "epilogue-order-with-newlines",
			input: "FROM alpine:3.20\n" +
				"CMD [\"serve\"]\n" +
				"RUN echo hello\n" +
				"ENTRYPOINT [\"/app\"]\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/epilogue-order", "tally/newline-between-instructions")...),
			wantApplied: 2, // epilogue-order + newline-between-instructions
		},

		// === Newline per chained call fix tests ===

		// RUN chain splitting: two chained commands
		{
			name:  "newline-per-chained-call-run-chain",
			input: "FROM alpine:3.20\nRUN apt-get update && apt-get install -y curl\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call")...),
			wantApplied: 1,
		},
		// RUN chain splitting: three chained commands
		{
			name:  "newline-per-chained-call-run-chain-three",
			input: "FROM alpine:3.20\nRUN cmd1 && cmd2 && cmd3\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call")...),
			wantApplied: 1,
		},
		// RUN mount splitting: two mounts
		{
			name: "newline-per-chained-call-run-mounts",
			input: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt " +
				"--mount=type=bind,source=go.sum,target=go.sum " +
				"apt-get update\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call")...),
			wantApplied: 1,
		},
		// RUN mount + chain combined
		{
			name: "newline-per-chained-call-run-mounts-and-chains",
			input: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt " +
				"--mount=type=bind,source=go.sum,target=go.sum " +
				"apt-get update && apt-get install -y curl\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call")...),
			wantApplied: 1,
		},
		// LABEL pair splitting
		{
			name: "newline-per-chained-call-label",
			input: "FROM alpine:3.20\n" +
				"LABEL org.opencontainers.image.title=myapp " +
				"org.opencontainers.image.version=1.0 " +
				"org.opencontainers.image.vendor=acme\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call")...),
			wantApplied: 1,
		},
		// HEALTHCHECK CMD chain splitting
		{
			name:  "newline-per-chained-call-healthcheck",
			input: "FROM alpine:3.20\nHEALTHCHECK CMD curl -f http://localhost/ && wget -qO- http://localhost/health || exit 1\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call")...),
			wantApplied: 1,
		},
		// Cross-rule: DL3027 (apt→apt-get) + chain split on same RUN
		{
			name:  "newline-per-chained-call-cross-dl3027",
			input: "FROM ubuntu:24.04\nRUN apt update && apt install -y curl\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/newline-per-chained-call", "hadolint/DL3027")...),
			wantApplied: 3, // DL3027 (×2, one per apt) + chain split
		},
		// Cross-rule: DL3047 (wget --progress) + chain split on same RUN
		{
			name: "newline-per-chained-call-cross-dl3047",
			input: "FROM ubuntu:24.04\n" +
				"RUN wget https://example.com/file.tar.gz " +
				"&& tar -xzf file.tar.gz\n",
			args: append([]string{"--fix"},
				mustSelectRules(
					"tally/newline-per-chained-call",
					"hadolint/DL3047",
				)...),
			wantApplied: 2, // DL3047 + chain split
		},
		// Cross-rule: consistent-indentation + newline-per-chained-call on a
		// multi-stage Dockerfile. consistent-indentation (priority 50) adds tab
		// indent to second-stage instructions; newline-per-chained-call (priority 97)
		// splits chains using instrIndent computed from the original (pre-fix) source.
		{
			name: "newline-per-chained-call-with-consistent-indentation",
			input: "FROM alpine:3.20 AS builder\n" +
				"RUN apt-get update && apt-get install -y curl\n" +
				"FROM scratch\n" +
				"COPY --from=builder /usr/bin/curl /usr/bin/curl\n",
			args: append([]string{"--fix"},
				mustSelectRules(
					"tally/consistent-indentation",
					"tally/newline-per-chained-call",
				)...),
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
			wantApplied: 3, // 2 indentation (RUN in stage 1 + COPY in stage 2) + 1 chain split
		},
		// tally/invalid-onbuild-trigger: COPPY → COPY via FixSuggestion (requires --fix-unsafe)
		{
			name:        "invalid-onbuild-trigger",
			input:       "FROM alpine:3.19 AS base\nONBUILD COPPY . /app\nONBUILD RUN echo hello\n",
			args:        append([]string{"--fix", "--fix-unsafe"}, mustSelectRules("tally/invalid-onbuild-trigger")...),
			wantApplied: 1,
		},
		// tally/invalid-json-form: unquoted → quoted via FixSuggestion (requires --fix-unsafe)
		{
			name:        "invalid-json-form-unquoted",
			input:       "FROM alpine:3.20\nCMD [bash, -lc, \"echo hi\"]\n",
			args:        append([]string{"--fix", "--fix-unsafe"}, mustSelectRules("tally/invalid-json-form")...),
			wantApplied: 1,
		},
		{
			name:        "invalid-json-form-single-quotes",
			input:       "FROM alpine:3.20\nENTRYPOINT ['/app', '--serve']\n",
			args:        append([]string{"--fix", "--fix-unsafe"}, mustSelectRules("tally/invalid-json-form")...),
			wantApplied: 1,
		},
		{
			name:        "invalid-json-form-trailing-comma",
			input:       "FROM alpine:3.20\nRUN [\"echo\", \"hello\",]\n",
			args:        append([]string{"--fix", "--fix-unsafe"}, mustSelectRules("tally/invalid-json-form")...),
			wantApplied: 1,
		},
		// Cross-rule fix: invalid-json-form fix satisfies both rules.
		// JSONArgsRecommended can't fix this (SplitSimpleCommand fails on "[bash, ...]"),
		// but invalid-json-form repairs the JSON, which resolves both violations.
		{
			name:  "invalid-json-form-cross-rules",
			input: "FROM alpine:3.20\nCMD [bash, -lc, \"echo hello\"]\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/invalid-json-form", "buildkit/JSONArgsRecommended")...,
			),
			wantApplied: 1,
		},
		// tally/require-secret-mounts: add missing secret mount
		{
			name: "require-secret-mounts",
			input: "FROM python:3.12-slim\n" +
				"RUN pip install -r requirements.txt\n",
			args: []string{
				"--fix",
				"--select", "tally/require-secret-mounts",
			},
			wantApplied: 1,
			config: `[rules.tally.require-secret-mounts]
severity = "warning"

[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"
`,
		},
		// tally/require-secret-mounts: preserves existing cache mount
		{
			name: "require-secret-mounts-with-cache-mount",
			input: "FROM python:3.12-slim\n" +
				"RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt\n",
			args: []string{
				"--fix",
				"--select", "tally/require-secret-mounts",
			},
			wantApplied: 1,
			config: `[rules.tally.require-secret-mounts]
severity = "warning"

[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"
`,
		},
		// Combined: require-secret-mounts + prefer-package-cache-mounts on same RUN.
		// The secret mount fix (security, priority 85) wins conflict resolution and
		// its rewrite includes both the secret mount AND cache mounts, so a single
		// --fix produces the fully-mounted RUN.
		{
			name: "require-secret-mounts-with-cache-mounts-combined",
			input: "FROM python:3.12-slim\n" +
				"RUN pip install -r requirements.txt\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/require-secret-mounts",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 2, // both mount insertions compose (zero-length edits don't conflict)
			config: `[rules.tally.require-secret-mounts]
severity = "error"

[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"

[rules.tally.prefer-package-cache-mounts]
severity = "error"
`,
		},
		// Combined: composer-no-dev-in-production (88) inserts --no-dev after
		// install, prefer-package-cache-mounts (90) adds the composer cache mount,
		// no-multi-spaces (10) collapses repeated spaces, newline-per-chained-call
		// (97) splits the && boundary, and consistent-indentation (50) indents
		// both RUN lines in the multi-stage file. Cache-mounts stays at warning
		// severity here so processor supersession does not hide the overlapping
		// warning/style violations on the same line.
		{
			name: "php-composer-no-dev-with-cache-mounts-and-formatting",
			input: "FROM alpine AS base\n" +
				"RUN echo base\n\n" +
				"FROM php:8.4-cli AS app\n" +
				"RUN composer  install && echo done\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules(
					"tally/no-multi-spaces",
					"tally/consistent-indentation",
					"tally/php/composer-no-dev-in-production",
					"tally/prefer-package-cache-mounts",
					"tally/newline-per-chained-call",
				)...),
			wantApplied: 6,
			config: `[rules.tally.consistent-indentation]
severity = "style"

[rules.tally.prefer-package-cache-mounts]
severity = "warning"
`,
		},
		// PHP: no-xdebug-in-final-image comments out a standalone xdebug
		// installation. The pecl install is the only command in the RUN, so the
		// preferred comment-out fix (FixSuggestion, priority 88) applies. The
		// builder stage is non-final so no fix there.
		{
			name: "php-no-xdebug-in-final-image-comment-out",
			input: "FROM php:8.4-cli AS builder\n" +
				"RUN docker-php-ext-install xdebug\n\n" +
				"FROM php:8.4-fpm AS app\n" +
				"RUN pecl install xdebug\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/php/no-xdebug-in-final-image")...),
			wantApplied: 1,
		},
		// Full composition: two secret mounts (insertion) + two cache mounts
		// (insertion) + cleanup removal (npm cache clean, --no-cache-dir)
		// on the same RUN in a single --fix pass.
		{
			name: "require-secret-mounts-multi-command",
			input: "FROM node:20\n" +
				"RUN npm install && npm cache clean --force && pip install --no-cache-dir pandas\n",
			args: []string{
				"--fix",
				"--fix-unsafe",
				"--select", "tally/require-secret-mounts",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 2, // secret mount insertion + cache mount insertion (with cleanup)
			config: `[rules.tally.require-secret-mounts]
severity = "error"

[rules.tally.require-secret-mounts.commands.npm]
id = "npmrc"
target = "/root/.npmrc"

[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"

[rules.tally.prefer-package-cache-mounts]
severity = "error"
`,
		},
		// Sort packages: single-line unsorted
		{
			name:  "sort-packages-single-line",
			input: "FROM alpine:3.20\nRUN apt-get install -y wget curl\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Sort packages: multi-line unsorted
		{
			name:  "sort-packages-multi-line",
			input: "FROM alpine:3.20\nRUN apt-get install -y \\\n    zip \\\n    curl \\\n    git\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Sort packages: npm
		{
			name:  "sort-packages-npm",
			input: "FROM node:20\nRUN npm install express axios\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Sort packages: choco install on Windows base image
		{
			name: "sort-packages-choco",
			input: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN choco install -y python3 nodejs git\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Sort packages: choco install with backtick escape, multi-line
		{
			name: "sort-packages-choco-backtick-escape",
			input: "# escape=`\n" +
				"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN choco install -y `\n" +
				"    python3 `\n" +
				"    nodejs `\n" +
				"    git\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Sort packages: pip with versions
		{
			name:  "sort-packages-pip",
			input: "FROM python:3.12\nRUN pip install flask==2.0 django==4.0\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Sort packages: mixed literals and variables — literals sorted first, variables at tail
		{
			name: "sort-packages-mixed-vars",
			input: "FROM python:3.12\n" +
				"RUN uv pip install $CDK_DEPS otel aws-otel $RUNTIME_DEPS polars==1.2.3\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages")...),
			wantApplied: 1,
		},
		// Cross-rule: sort-packages (priority 15) + newline-per-chained-call (priority 97)
		// on a chained RUN with unsorted packages. sort-packages rewrites package names
		// within the install command; newline-per-chained-call splits the && boundary.
		// Edits target different regions and should compose without conflict.
		{
			name: "sort-packages-cross-newline-per-chained-call",
			input: "FROM alpine:3.20\n" +
				"RUN apt-get update && apt-get install -y wget curl git\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages", "tally/newline-per-chained-call")...),
			wantApplied: 2, // sort-packages + newline-per-chained-call
		},

		// Cross-rule: sort-packages + no-multi-spaces on the same line.
		// sort-packages (priority 9) wins over no-multi-spaces (priority 10)
		// when their edits overlap on double-space positions.
		// sort-packages produces clean output on its own.
		{
			name: "sort-packages-cross-no-multi-spaces",
			input: "FROM alpine:3.20\n" +
				"RUN apt-get install -y  zoo  foo  bar\n",
			args: append([]string{"--fix"},
				mustSelectRules("tally/sort-packages", "tally/no-multi-spaces")...),
			wantApplied: 1, // sort-packages wins; no-multi-spaces skipped due to overlap
		},

		// Cross-rule: sort-packages (priority 15) + shellcheck SC2086 (quotes vars)
		// on a single-line RUN with interleaved variables. sort-packages moves
		// literals to the front via insert+delete (never touching variable tokens),
		// SC2086 wraps $EXTRA_PKGS in quotes. Both compose without conflict.
		{
			name: "sort-packages-cross-shellcheck-sc2086",
			input: "FROM alpine:3.20\n" +
				"RUN apk add --no-cache zoo foo $EXTRA_PKGS bar\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/sort-packages", "shellcheck/SC2086")...),
			wantApplied: 2, // sort-packages + SC2086
		},

		// Three rules on the same RUN: secret mount insertion + cache mount
		// insertion + DL3030 (-y flag insertion) + cache cleanup deletion.
		// All edits are targeted and non-overlapping.
		{
			name: "three-rules-same-line",
			input: "FROM centos:7\n" +
				"RUN yum update && yum install curl && yum clean all && curl http://127.0.0.1:8080\n",
			args: []string{
				"--fix",
				"--fix-unsafe",
				"--ignore", "tally/prefer-run-heredoc",
				"--select", "tally/require-secret-mounts",
				"--select", "hadolint/DL3030",
				"--select", "tally/prefer-package-cache-mounts",
			},
			wantApplied: 4, // secret mount + cache mount (with cleanup) + DL3030 -y + prefer-curl-config
			config: `[rules.tally.require-secret-mounts]
severity = "warning"

[rules.tally.require-secret-mounts.commands.yum]
id = "YUM_CONF"
target = "/etc/yum.conf"

[rules.tally.prefer-package-cache-mounts]
severity = "warning"

[rules.hadolint.DL3030]
severity = "warning"
`,
		},

		// prefer-copy-chmod: COPY + RUN chmod → COPY --chmod
		{
			name:  "prefer-copy-chmod-basic",
			input: "FROM alpine\nCOPY entrypoint.sh /app/entrypoint.sh\nRUN chmod +x /app/entrypoint.sh\n",
			args: []string{
				"--fix",
				"--select", "tally/prefer-copy-chmod",
			},
			wantApplied: 1,
		},

		// prefer-copy-chmod: preserves existing --chown flag
		{
			name:  "prefer-copy-chmod-with-chown",
			input: "FROM alpine\nCOPY --chown=appuser:appuser entrypoint.sh /app/entrypoint.sh\nRUN chmod 755 /app/entrypoint.sh\n",
			args: []string{
				"--fix",
				"--select", "tally/prefer-copy-chmod",
			},
			wantApplied: 1,
		},

		// prefer-copy-chmod: directory destination
		{
			name:  "prefer-copy-chmod-dir-dest",
			input: "FROM alpine\nCOPY start.sh /usr/local/bin/\nRUN chmod 0755 /usr/local/bin/start.sh\n",
			args: []string{
				"--fix",
				"--select", "tally/prefer-copy-chmod",
			},
			wantApplied: 1,
		},

		// GPU: no-hardcoded-visible-devices FixSafe (redundant all on nvidia/cuda)
		{
			name:  "gpu-visible-devices-redundant-all",
			input: "FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04\nENV NVIDIA_VISIBLE_DEVICES=all\nRUN echo hello\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/gpu/no-hardcoded-visible-devices")...),
			wantApplied: 1,
		},
		// GPU: no-hardcoded-visible-devices FixSuggestion (hardcoded index, requires --fix-unsafe)
		{
			name:  "gpu-visible-devices-hardcoded-index",
			input: "FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04\nENV NVIDIA_VISIBLE_DEVICES=0\nRUN echo hello\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/gpu/no-hardcoded-visible-devices")...),
			wantApplied: 1,
		},
		// GPU: no-hardcoded-visible-devices FixSafe multi-key partial removal
		{
			name:  "gpu-visible-devices-multi-key-partial",
			input: "FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04\nENV NVIDIA_VISIBLE_DEVICES=all CUDA_HOME=/usr/local/cuda\nRUN echo hello\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/gpu/no-hardcoded-visible-devices")...),
			wantApplied: 1,
		},
		// GPU: no-hardcoded-visible-devices no-fire (all on non-CUDA base)
		{
			name:  "gpu-visible-devices-all-non-cuda-no-fire",
			input: "FROM ubuntu:22.04\nENV NVIDIA_VISIBLE_DEVICES=all\nRUN echo hello\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/gpu/no-hardcoded-visible-devices")...),
			wantApplied: 0,
		},

		// GPU: prefer-minimal-driver-capabilities FixSuggestion (requires --fix-unsafe)
		{
			name:  "gpu-driver-caps-all-suggestion",
			input: "FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04\nENV NVIDIA_DRIVER_CAPABILITIES=all\nRUN echo hello\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/gpu/prefer-minimal-driver-capabilities")...),
			wantApplied: 1,
		},
		// GPU: prefer-minimal-driver-capabilities not applied without --fix-unsafe
		{
			name:  "gpu-driver-caps-all-no-unsafe",
			input: "FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04\nENV NVIDIA_DRIVER_CAPABILITIES=all\nRUN echo hello\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/gpu/prefer-minimal-driver-capabilities")...),
			wantApplied: 0,
		},
		// GPU: prefer-minimal-driver-capabilities multi-key partial replacement
		{
			name:  "gpu-driver-caps-multi-key-replace",
			input: "FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04\nENV NVIDIA_DRIVER_CAPABILITIES=all CUDA_HOME=/usr/local/cuda\nRUN echo hello\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/gpu/prefer-minimal-driver-capabilities")...),
			wantApplied: 1,
		},
		// GPU: prefer-minimal-driver-capabilities no-fire (variable ref)
		{
			name:  "gpu-driver-caps-variable-no-fire",
			input: "FROM ubuntu:22.04\nARG CAPS=all\nENV NVIDIA_DRIVER_CAPABILITIES=${CAPS}\nRUN echo hello\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/gpu/prefer-minimal-driver-capabilities")...),
			wantApplied: 0,
		},

		// No ungraceful STOPSIGNAL: SIGKILL → SIGTERM via FixSuggestion (requires --fix-unsafe)
		{
			name:  "no-ungraceful-stopsignal-sigkill",
			input: "FROM alpine:3.20\nSTOPSIGNAL SIGKILL\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/no-ungraceful-stopsignal")...),
			wantApplied: 1,
		},
		// No ungraceful STOPSIGNAL: SIGSTOP → SIGTERM
		{
			name:  "no-ungraceful-stopsignal-sigstop",
			input: "FROM alpine:3.20\nSTOPSIGNAL SIGSTOP\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/no-ungraceful-stopsignal")...),
			wantApplied: 1,
		},
		// No ungraceful STOPSIGNAL: numeric 9 → SIGTERM
		{
			name:  "no-ungraceful-stopsignal-numeric-9",
			input: "FROM alpine:3.20\nSTOPSIGNAL 9\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/no-ungraceful-stopsignal")...),
			wantApplied: 1,
		},
		// No ungraceful STOPSIGNAL: SIGTERM — no fix needed
		{
			name:  "no-ungraceful-stopsignal-sigterm-no-fix",
			input: "FROM alpine:3.20\nSTOPSIGNAL SIGTERM\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/no-ungraceful-stopsignal")...),
			wantApplied: 0,
		},

		// Windows no-stopsignal: comment out STOPSIGNAL on Windows stage (FixSafe)
		{
			name:  "windows-no-stopsignal",
			input: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nSTOPSIGNAL SIGTERM\nCMD [\"cmd\", \"/C\", \"echo\", \"hi\"]\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/windows/no-stopsignal")...),
			wantApplied: 1,
		},
		// Windows no-stopsignal: Linux stage STOPSIGNAL — no fix
		{
			name:  "windows-no-stopsignal-linux-no-fix",
			input: "FROM alpine:3.20\nSTOPSIGNAL SIGTERM\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/windows/no-stopsignal")...),
			wantApplied: 0,
		},
		// Cross-rule: both windows/no-stopsignal and no-ungraceful-stopsignal enabled on a
		// Windows stage with SIGKILL. Only windows/no-stopsignal should fire (comment-out);
		// no-ungraceful-stopsignal skips Windows stages entirely.
		{
			name:  "windows-no-stopsignal-cross-no-ungraceful",
			input: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nSTOPSIGNAL SIGKILL\n",
			args: append(
				[]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/windows/no-stopsignal", "tally/no-ungraceful-stopsignal")...),
			wantApplied: 1, // only windows/no-stopsignal should apply
		},

		// Windows no-chown-flag: remove --chown from COPY on Windows stage (FixSafe)
		{
			name:  "windows-no-chown-flag",
			input: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nCOPY --chown=app:app src/ C:/app/\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/windows/no-chown-flag")...),
			wantApplied: 1,
		},
		// Cross-rule: windows/no-chown-flag + copy-after-user-without-chown on a
		// Windows stage with USER ContainerUser then COPY without --chown.
		// copy-after-user-without-chown still reports on Windows but suppresses
		// the --chown fix. No move-USER fix is possible here (no RUN/WORKDIR
		// after COPY), and windows/no-chown-flag has nothing to remove. Zero fixes.
		{
			name:  "windows-no-chown-flag-cross-copy-after-user",
			input: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nUSER ContainerUser\nCOPY src/ C:/app/\n",
			args: append(
				[]string{"--fix", "--fail-level", "none"},
				mustSelectRules("tally/windows/no-chown-flag", "tally/copy-after-user-without-chown")...),
			wantApplied: 0, // no fixable violations on this Windows stage
		},
		// Cross-rule: windows/no-chown-flag removes --chown that was already present
		// on a Windows stage with USER, while copy-after-user-without-chown does not
		// fire because --chown is already set (mutually exclusive conditions).
		{
			name:  "windows-no-chown-flag-cross-copy-after-user-existing-chown",
			input: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nUSER ContainerUser\nCOPY --chown=ContainerUser src/ C:/app/\n",
			args: append(
				[]string{"--fix", "--fail-level", "none"},
				mustSelectRules("tally/windows/no-chown-flag", "tally/copy-after-user-without-chown")...),
			wantApplied: 1, // only windows/no-chown-flag removes the dead --chown
		},
		// Windows no-chown-flag: Linux stage — no fix
		{
			name:  "windows-no-chown-flag-linux-no-fix",
			input: "FROM alpine:3.20\nCOPY --chown=app:app src/ /app/\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/windows/no-chown-flag")...),
			wantApplied: 0,
		},

		// PowerShell error-action-preference: SHELL missing prelude → insert prelude (FixSuggestion)
		{
			name: "powershell-error-action-preference-missing-both",
			input: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				"SHELL [\"pwsh\", \"-Command\"]\n" +
				"RUN Install-Module PSReadLine -Force; Write-Host done\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/powershell/error-action-preference")...),
			wantApplied: 1,
		},
		// PowerShell error-action-preference: single-command RUN — no fix (below threshold)
		{
			name: "powershell-error-action-preference-single-cmd-no-fix",
			input: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				"SHELL [\"pwsh\", \"-Command\"]\n" +
				"RUN Install-Module PSReadLine -Force\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/powershell/error-action-preference")...),
			wantApplied: 0,
		},
		// PowerShell error-action-preference: backtick escape, multiline RUN,
		// SHELL already has Stop — only PSNativeCommandUseErrorActionPreference missing.
		{
			name: "powershell-error-action-preference-backtick-multiline-native-only",
			input: "# escape=`\n" +
				"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"SHELL [\"powershell\", \"-Command\", \"$ErrorActionPreference = 'Stop';\"]\n" +
				"RUN Invoke-WebRequest -Uri https://example.com/setup.exe `\n" +
				"      -OutFile C:\\setup.exe; `\n" +
				"    Start-Process C:\\setup.exe -Wait; `\n" +
				"    Remove-Item C:\\setup.exe -Force\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/powershell/error-action-preference")...),
			wantApplied: 1,
		},
		// PowerShell error-action-preference: explicit powershell -Command wrapper
		// with backtick-continued multiline body. Both preludes missing; fix
		// prepends them to the inner script.
		{
			name: "powershell-error-action-preference-explicit-wrapper-backtick",
			input: "# escape=`\n" +
				"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN powershell -Command `\n" +
				"    Invoke-WebRequest -Uri https://example.com/setup.exe " +
				"-OutFile C:\\setup.exe; `\n" +
				"    Start-Process C:\\setup.exe -Wait; `\n" +
				"    Remove-Item C:\\setup.exe -Force\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/powershell/error-action-preference")...),
			wantApplied: 1,
		},

		// PowerShell error-action-preference: PowerShell base image with no SHELL
		// instruction → inserts a new SHELL after FROM with the prelude.
		{
			name: "powershell-error-action-preference-insert-shell-after-from",
			input: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				"RUN Install-Module PSReadLine -Force; Write-Host done\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/powershell/error-action-preference")...),
			wantApplied: 1,
		},

		// Cross-rule: error-action-preference + prefer-shell-instruction both
		// enabled on repeated pwsh wrappers. prefer-shell-instruction (95) runs
		// first and inserts SHELL with the full prelude; error-action-preference
		// (96) fix is skipped because its SHELL edit overlaps.
		{
			name: "powershell-error-action-preference-cross-prefer-shell-instruction",
			input: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				"RUN pwsh -Command Install-Module PSReadLine -Force\n" +
				"RUN pwsh -Command Invoke-WebRequest https://example.com -OutFile /tmp/f.zip\n",
			args: append(
				[]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules(
					"tally/powershell/error-action-preference",
					"tally/powershell/prefer-shell-instruction",
				)...),
			wantApplied: 1, // only prefer-shell-instruction applies
		},

		// Prefer canonical STOPSIGNAL: missing SIG prefix → SIGTERM (FixSafe)
		{
			name:  "prefer-canonical-stopsignal-prefix",
			input: "FROM alpine:3.20\nSTOPSIGNAL TERM\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-canonical-stopsignal")...),
			wantApplied: 1,
		},
		// Prefer canonical STOPSIGNAL: quoted → unquoted (FixSafe)
		{
			name:  "prefer-canonical-stopsignal-quoted",
			input: "FROM alpine:3.20\nSTOPSIGNAL \"SIGINT\"\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-canonical-stopsignal")...),
			wantApplied: 1,
		},
		// Prefer canonical STOPSIGNAL: numeric → named (FixSafe)
		{
			name:  "prefer-canonical-stopsignal-numeric",
			input: "FROM alpine:3.20\nSTOPSIGNAL 15\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-canonical-stopsignal")...),
			wantApplied: 1,
		},
		// Prefer canonical STOPSIGNAL: lowercase → uppercase (FixSafe)
		{
			name:  "prefer-canonical-stopsignal-lowercase",
			input: "FROM alpine:3.20\nSTOPSIGNAL sigquit\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-canonical-stopsignal")...),
			wantApplied: 1,
		},
		// Prefer canonical STOPSIGNAL: RTMIN+3 → SIGRTMIN+3 (FixSafe)
		{
			name:  "prefer-canonical-stopsignal-rtmin",
			input: "FROM alpine:3.20\nSTOPSIGNAL RTMIN+3\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-canonical-stopsignal")...),
			wantApplied: 1,
		},
		// Prefer canonical STOPSIGNAL: already canonical — no fix
		{
			name:  "prefer-canonical-stopsignal-no-fix",
			input: "FROM alpine:3.20\nSTOPSIGNAL SIGTERM\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-canonical-stopsignal")...),
			wantApplied: 0,
		},
		// Cross-rule: both prefer-canonical-stopsignal and no-ungraceful-stopsignal on
		// "SIGKILL" (quoted + ungraceful). The ungraceful fix (SIGTERM) has higher severity
		// and wins; the canonical fix is skipped due to overlapping edit range.
		{
			name:  "prefer-canonical-stopsignal-cross-no-ungraceful",
			input: "FROM alpine:3.20\nSTOPSIGNAL \"SIGKILL\"\n",
			args: append(
				[]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/prefer-canonical-stopsignal", "tally/no-ungraceful-stopsignal")...),
			wantApplied: 1, // only no-ungraceful-stopsignal fix should apply (SIGTERM)
		},

		// Prefer systemd SIGRTMIN+3: wrong signal → SIGRTMIN+3 (FixSafe)
		{
			name:  "prefer-systemd-sigrtmin-plus-3-wrong-signal",
			input: "FROM fedora:40\nSTOPSIGNAL SIGTERM\nENTRYPOINT [\"/sbin/init\"]\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-systemd-sigrtmin-plus-3")...),
			wantApplied: 1,
		},
		// Prefer systemd SIGRTMIN+3: missing → insert STOPSIGNAL (FixSafe)
		{
			name:  "prefer-systemd-sigrtmin-plus-3-missing",
			input: "FROM fedora:40\nENTRYPOINT [\"/sbin/init\"]\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-systemd-sigrtmin-plus-3")...),
			wantApplied: 1,
		},
		// Prefer systemd SIGRTMIN+3: correct signal — no fix
		{
			name:  "prefer-systemd-sigrtmin-plus-3-no-fix",
			input: "FROM fedora:40\nSTOPSIGNAL SIGRTMIN+3\nENTRYPOINT [\"/sbin/init\"]\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-systemd-sigrtmin-plus-3")...),
			wantApplied: 0,
		},
		// Prefer systemd SIGRTMIN+3: non-systemd — no fix
		{
			name:  "prefer-systemd-sigrtmin-plus-3-non-systemd-no-fix",
			input: "FROM nginx:1.27\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-systemd-sigrtmin-plus-3")...),
			wantApplied: 0,
		},
		// Cross-rule: systemd + no-ungraceful on SIGKILL. systemd rule wins
		// with Priority -1, replacing SIGKILL with SIGRTMIN+3 instead of SIGTERM.
		{
			name:  "prefer-systemd-sigrtmin-plus-3-cross-no-ungraceful",
			input: "FROM fedora:40\nSTOPSIGNAL SIGKILL\nENTRYPOINT [\"/sbin/init\"]\n",
			args: append(
				[]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				mustSelectRules("tally/prefer-systemd-sigrtmin-plus-3", "tally/no-ungraceful-stopsignal")...),
			wantApplied: 1, // systemd fix wins with Priority -1
		},
		// Cross-rule: systemd + canonical on RTMIN+3. Only canonical fires
		// (normalizes RTMIN+3 → SIGRTMIN+3); systemd sees correct signal after normalization.
		{
			name:  "prefer-systemd-sigrtmin-plus-3-cross-canonical-rtmin",
			input: "FROM fedora:40\nSTOPSIGNAL RTMIN+3\nENTRYPOINT [\"/sbin/init\"]\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/prefer-systemd-sigrtmin-plus-3", "tally/prefer-canonical-stopsignal")...),
			wantApplied: 1, // only canonical applies (RTMIN+3 → SIGRTMIN+3)
		},

		// User created but never used: insert USER before CMD (FixUnsafe)
		{
			name:  "user-created-but-never-used",
			input: "FROM ubuntu:22.04\nRUN useradd -r appuser\nCMD [\"app\"]\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/user-created-but-never-used")...),
			wantApplied: 1,
		},

		// copy-after-user-without-chown: adds --chown flag (preferred fix)
		{
			name:  "copy-after-user-without-chown-basic",
			input: "FROM ubuntu:22.04\nUSER appuser\nCOPY app /app\nRUN setup.sh\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/copy-after-user-without-chown")...),
			wantApplied: 1,
		},

		// copy-after-user-without-chown: ADD also fixed
		{
			name:  "copy-after-user-without-chown-add",
			input: "FROM ubuntu:22.04\nUSER 1000\nADD config.tar.gz /etc/app/\nRUN setup.sh\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/copy-after-user-without-chown")...),
			wantApplied: 1,
		},

		// copy-after-user-without-chown: multiple COPY/ADD all fixed
		{
			name:  "copy-after-user-without-chown-multi",
			input: "FROM ubuntu:22.04\nUSER appuser\nCOPY a /a\nCOPY b /b\nRUN setup.sh\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/copy-after-user-without-chown")...),
			wantApplied: 2,
		},

		// world-writable-state-path-workaround: replaces 777 with 775
		{
			name:  "world-writable-state-path-workaround",
			input: "FROM ubuntu:22.04\nRUN chmod 777 /data\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/world-writable-state-path-workaround")...),
			wantApplied: 1,
		},

		// world-writable-state-path-workaround: 666 → 664
		{
			name:  "world-writable-state-path-workaround-666",
			input: "FROM ubuntu:22.04\nRUN chmod 666 /app/config\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/world-writable-state-path-workaround")...),
			wantApplied: 1,
		},

		// Cross-rule: world-writable + prefer-copy-chmod on same COPY+RUN chmod 777 pair.
		// prefer-copy-chmod (priority 99) deletes the RUN and adds COPY --chmod=777,
		// which makes world-writable's fix on the (now-deleted) RUN moot. Only the
		// prefer-copy-chmod fix applies.
		{
			name:  "world-writable-cross-prefer-copy-chmod",
			input: "FROM ubuntu:22.04\nCOPY app /app\nRUN chmod 777 /app\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/world-writable-state-path-workaround", "tally/prefer-copy-chmod")...),
			wantApplied: 1, // prefer-copy-chmod subsumes the RUN
		},

		// Cross-rule: world-writable + copy-after-user-without-chown on different instructions.
		// copy-after-user adds --chown to COPY; world-writable fixes chmod 777→775 in RUN.
		// Both fixes target different instructions so both apply.
		{
			name:  "world-writable-cross-copy-after-user-without-chown",
			input: "FROM ubuntu:22.04\nUSER appuser\nCOPY app /app\nRUN chmod 777 /app\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/world-writable-state-path-workaround", "tally/copy-after-user-without-chown")...),
			wantApplied: 2,
		},

		// Cross-rule: copy-after-user-without-chown + prefer-copy-chmod compose correctly
		{
			name: "copy-after-user-chown-plus-chmod",
			input: "FROM ubuntu:22.04\nUSER appuser\n" +
				"COPY entrypoint.sh /app/entrypoint.sh\n" +
				"RUN chmod +x /app/entrypoint.sh\n",
			args: append(
				[]string{"--fix"},
				mustSelectRules("tally/copy-after-user-without-chown", "tally/prefer-copy-chmod")...),
			wantApplied: 2,
		},

		// Named identity in passwd-less stage: USER fix (FixSuggestion, needs --fix --fix-unsafe)
		{
			name:  "named-identity-user-fix",
			input: "FROM scratch\nUSER appuser\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/named-identity-in-passwdless-stage")...),
			wantApplied: 1,
		},
		// Named identity: --chown fix
		{
			name:  "named-identity-chown-fix",
			input: "FROM golang:1.22 AS builder\nRUN echo hello > /app\n\nFROM scratch\nCOPY --chown=appuser:appgroup --from=builder /app /app\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/named-identity-in-passwdless-stage")...),
			wantApplied: 1,
		},
		// Named identity: numeric USER in scratch — no fix needed
		{
			name:  "named-identity-numeric-no-fix",
			input: "FROM scratch\nUSER 65532\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/named-identity-in-passwdless-stage")...),
			wantApplied: 0,
		},
		// Named identity: passwd copied — no fix needed
		{
			name: "named-identity-passwd-copied-no-fix",
			input: "FROM golang:1.22 AS builder\nRUN useradd -r appuser\n\nFROM scratch\n" +
				"COPY --from=builder /etc/passwd /etc/passwd\n" +
				"COPY --from=builder /etc/group /etc/group\nUSER appuser\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				mustSelectRules("tally/named-identity-in-passwdless-stage")...),
			wantApplied: 0,
		},
	}
}
