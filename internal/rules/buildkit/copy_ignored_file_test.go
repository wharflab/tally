package buildkit

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

type mockBuildContext struct {
	files           map[string]string
	ignoredPaths    map[string]bool
	ignoreErr       map[string]error
	heredocPaths    map[string]bool
	fileExistsCalls map[string]int
	pathExistsCalls map[string]int
}

func (m *mockBuildContext) IsIgnored(path string) (bool, error) {
	if err := m.ignoreErr[path]; err != nil {
		return false, err
	}
	return m.ignoredPaths[path], nil
}

func (m *mockBuildContext) FileExists(path string) bool {
	if m.fileExistsCalls != nil {
		m.fileExistsCalls[path]++
	}
	return m.exists(path)
}

func (m *mockBuildContext) PathExists(path string) bool {
	if m.pathExistsCalls != nil {
		m.pathExistsCalls[path]++
	}
	return m.exists(path)
}

func (m *mockBuildContext) ReadFile(path string) ([]byte, error) {
	content, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("missing file %q", path)
	}
	return []byte(content), nil
}

func (m *mockBuildContext) IsHeredocFile(path string) bool {
	return m.heredocPaths[path]
}

func (m *mockBuildContext) exists(path string) bool {
	_, ok := m.files[path]
	return ok
}

func TestCopyIgnoredFileRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewCopyIgnoredFileRule().Metadata())
}

func TestCopyIgnoredFileRule_Check_NoInvocationContext(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM scratch
COPY ignored.txt /ignored.txt
`)
	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations without invocation context, got %d", len(violations))
	}
}

func TestCopyIgnoredFileRule_Check_AvailableContextSource(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
COPY app.go /app.go
`, &mockBuildContext{files: map[string]string{"app.go": "package main\n"}})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations for available source, got %d", len(violations))
	}
}

func TestCopyIgnoredFileRule_Check_SkipsWholeContextCopy(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
COPY . /app
`, &mockBuildContext{})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations for whole-context COPY, got %d", len(violations))
	}
}

func TestCopyIgnoredFileRule_Check_SkipsGlobContextSourceAvailabilityProbe(t *testing.T) {
	t.Parallel()

	context := &mockBuildContext{
		fileExistsCalls: map[string]int{},
		pathExistsCalls: map[string]int{},
	}
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
COPY *.go /app
`, context)

	violations := NewCopyIgnoredFileRule().Check(input)
	for _, violation := range violations {
		if strings.Contains(violation.Message, "source '*.go' is not available") {
			t.Fatalf("expected no unavailable-source violation for glob source, got %q", violation.Message)
		}
	}
	if got := context.pathExistsCalls["*.go"]; got != 0 {
		t.Fatalf("PathExists(\"*.go\") calls = %d, want 0", got)
	}
	if got := context.fileExistsCalls["*.go"]; got != 0 {
		t.Fatalf("FileExists(\"*.go\") calls = %d, want 0", got)
	}
}

func TestCopyIgnoredFileRule_Check_UnavailableContextSource(t *testing.T) {
	t.Parallel()

	r := NewCopyIgnoredFileRule()
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
COPY ignored.txt /ignored.txt
`, &mockBuildContext{
		files:        map[string]string{"ignored.txt": "secret\n"},
		ignoredPaths: map[string]bool{"ignored.txt": true},
	})

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Severity != r.Metadata().DefaultSeverity {
		t.Fatalf("severity = %s, want %s", violations[0].Severity, r.Metadata().DefaultSeverity)
	}
	if violations[0].RuleCode != rules.BuildKitRulePrefix+"CopyIgnoredFile" {
		t.Fatalf("violation code = %q", violations[0].RuleCode)
	}
	if !strings.Contains(violations[0].Message, "not available in the build context") {
		t.Fatalf("violation message = %q", violations[0].Message)
	}
	if violations[0].Line() != 2 {
		t.Fatalf("violation line = %d, want 2", violations[0].Line())
	}
}

func TestCopyIgnoredFileRule_Check_ContextAvailabilityError(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
COPY app.go /app.go
`, &mockBuildContext{
		ignoreErr: map[string]error{"app.go": errors.New("broken ignore matcher")},
	})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(violations))
	}
	if violations[0].Severity != rules.SeverityWarning {
		t.Fatalf("severity = %s, want warning", violations[0].Severity)
	}
	if !strings.Contains(violations[0].Message, "failed to evaluate build context availability") {
		t.Fatalf("violation message = %q", violations[0].Message)
	}
}

func TestCopyIgnoredFileRule_Check_SkipsCopyFromStage(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch AS builder
COPY ignored.txt /ignored.txt
FROM scratch
COPY --from=builder /ignored.txt /ignored.txt
`, &mockBuildContext{
		files:        map[string]string{"ignored.txt": "secret\n"},
		ignoredPaths: map[string]bool{"ignored.txt": true},
	})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected only the context COPY to be reported, got %d", len(violations))
	}
	if violations[0].Line() != 2 {
		t.Fatalf("violation line = %d, want 2", violations[0].Line())
	}
}

func TestCopyIgnoredFileRule_Check_SkipsURLs(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
ADD https://example.com/file.txt /file.txt
`, &mockBuildContext{
		ignoredPaths: map[string]bool{"https://example.com/file.txt": true},
	})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations for URL ADD source, got %d", len(violations))
	}
}

func TestCopyIgnoredFileRule_Check_ADDUnavailableContextSource(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
ADD ignored.tar.gz /tmp/
`, &mockBuildContext{
		files:        map[string]string{"ignored.tar.gz": "archive\n"},
		ignoredPaths: map[string]bool{"ignored.tar.gz": true},
	})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for ADD source, got %d", len(violations))
	}
}

func TestCopyIgnoredFileRule_Check_MultipleStages(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithContext(t, "Dockerfile", `FROM scratch
COPY ignored1.txt /ignored1.txt
FROM scratch
COPY ignored2.txt /ignored2.txt
`, &mockBuildContext{
		files: map[string]string{
			"ignored1.txt": "one\n",
			"ignored2.txt": "two\n",
		},
		ignoredPaths: map[string]bool{
			"ignored1.txt": true,
			"ignored2.txt": true,
		},
	})

	violations := NewCopyIgnoredFileRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations from 2 stages, got %d", len(violations))
	}
}
