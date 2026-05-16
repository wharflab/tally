package customlint

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv("GOEXPERIMENT")
	if os.Getenv("TEST_SRCDIR") != "" {
		fmt.Fprintln(os.Stderr,
			"skipping customlint analysistest suite under Bazel; analysistest requires a host Go command with a matching runtime GOROOT")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
