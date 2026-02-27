package integration

import "testing"

func TestFixShellcheckSC1040MixedIndentationSnapshot(t *testing.T) {
	t.Parallel()

	args, err := selectRules("shellcheck/SC1040")
	if err != nil {
		t.Fatalf("build rule-selection args: %v", err)
	}

	runFixCase(t, fixCase{
		name: "shellcheck-sc1040-mixed-indentation-standalone",
		input: "FROM alpine:3.20\n" +
			"\n" +
			"RUN <<SCRIPT\n" +
			"cat <<-EOF\n" +
			"hello\n" +
			"\t  \t   EOF\n" +
			"EOF\n" +
			"SCRIPT\n",
		args:        append([]string{"--fix"}, args...),
		wantApplied: 1,
	})
}
