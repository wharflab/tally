package integration

import "testing"

func TestLint(t *testing.T) {
	t.Parallel()
	for _, tc := range lintCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runLintCase(t, tc)
		})
	}
}
