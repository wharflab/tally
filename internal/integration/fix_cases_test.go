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
		{
			name: "ai-autofix-prefer-uv-over-conda",
			input: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN conda install -y numpy torch
CMD ["python"]
`,
			args: append([]string{
				"--fix",
				"--fix-unsafe",
				"--fix-rule", "tally/gpu/prefer-uv-over-conda",
			}, mustSelectRules("tally/gpu/prefer-uv-over-conda")...),
			wantApplied: 1,
			config: fmt.Sprintf(`[ai]
enabled = true
timeout = "10s"
redact-secrets = false
command = ['%s', '-mode=uv_over_conda']

[rules.tally."gpu/prefer-uv-over-conda"]
fix = "explicit"
`, acpAgentPath),
		},
	}
}
