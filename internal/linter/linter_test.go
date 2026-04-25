package linter

import (
	"testing"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/invocation"
	"github.com/wharflab/tally/internal/rules"
)

func TestAttachInvocation_DockerfileSetsKeyWithoutSource(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key: "dockerfile|Dockerfile||Dockerfile",
		Source: invocation.InvocationSource{
			Kind: invocation.KindDockerfile,
			File: "Dockerfile",
		},
		DockerfilePath: "Dockerfile",
	}
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 1),
			"test/rule",
			"message",
			rules.SeverityWarning,
		),
	}

	attachInvocation(violations, inv)

	if violations[0].InvocationKey != inv.Key {
		t.Fatalf("InvocationKey = %q, want %q", violations[0].InvocationKey, inv.Key)
	}
	if violations[0].Invocation != nil {
		t.Fatalf("Invocation = %#v, want nil", violations[0].Invocation)
	}
}

func TestAttachInvocation_OrchestratorSetsKeyAndSource(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key: "compose|compose.yaml|api|Dockerfile",
		Source: invocation.InvocationSource{
			Kind: invocation.KindCompose,
			File: "compose.yaml",
			Name: "api",
		},
		DockerfilePath: "Dockerfile",
	}
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 1),
			"test/rule",
			"message",
			rules.SeverityWarning,
		),
	}

	attachInvocation(violations, inv)

	if violations[0].InvocationKey != inv.Key {
		t.Fatalf("InvocationKey = %q, want %q", violations[0].InvocationKey, inv.Key)
	}
	if violations[0].Invocation == nil {
		t.Fatal("Invocation = nil, want source")
	}
	if *violations[0].Invocation != inv.Source {
		t.Fatalf("Invocation = %#v, want %#v", *violations[0].Invocation, inv.Source)
	}
}

func TestAttachInvocation_OrchestratorUsesPerViolationSourceCopy(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key: "compose\x00compose.yaml\x00api\x00Dockerfile",
		Source: invocation.InvocationSource{
			Kind: invocation.KindCompose,
			File: "compose.yaml",
			Name: "api",
		},
		DockerfilePath: "Dockerfile",
	}
	violations := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("Dockerfile", 1), "test/rule", "one", rules.SeverityWarning),
		rules.NewViolation(rules.NewLineLocation("Dockerfile", 2), "test/rule", "two", rules.SeverityWarning),
	}

	attachInvocation(violations, inv)

	if violations[0].Invocation == nil || violations[1].Invocation == nil {
		t.Fatal("Invocation source missing")
	}
	if violations[0].Invocation == violations[1].Invocation {
		t.Fatal("violations share Invocation source pointer, want per-violation copies")
	}
}

func TestAttachInvocationToAsyncRequest_DockerfileSetsKeyWithoutSource(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key: "dockerfile|Dockerfile||Dockerfile",
		Source: invocation.InvocationSource{
			Kind: invocation.KindDockerfile,
			File: "Dockerfile",
		},
		DockerfilePath: "Dockerfile",
	}
	req := &async.CheckRequest{
		Handler: staticAsyncHandler{
			rules.NewViolation(
				rules.NewLineLocation("Dockerfile", 1),
				"test/rule",
				"message",
				rules.SeverityWarning,
			),
		},
	}

	attachInvocationToAsyncRequest(req, inv)

	if req.InvocationKey != inv.Key {
		t.Fatalf("request InvocationKey = %q, want %q", req.InvocationKey, inv.Key)
	}
	results := req.Handler.OnSuccess(nil)
	if len(results) != 1 {
		t.Fatalf("OnSuccess returned %d results, want 1", len(results))
	}
	violation, ok := results[0].(rules.Violation)
	if !ok {
		t.Fatalf("OnSuccess result type = %T, want rules.Violation", results[0])
	}
	if violation.InvocationKey != inv.Key {
		t.Fatalf("result InvocationKey = %q, want %q", violation.InvocationKey, inv.Key)
	}
	if violation.Invocation != nil {
		t.Fatalf("result Invocation = %#v, want nil", violation.Invocation)
	}
}

func TestAttachInvocationToAsyncRequest_DockerfileSetsCompletedCheckKey(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key: "dockerfile\x00Dockerfile\x00\x00Dockerfile",
		Source: invocation.InvocationSource{
			Kind: invocation.KindDockerfile,
			File: "Dockerfile",
		},
		DockerfilePath: "Dockerfile",
	}
	req := &async.CheckRequest{
		Handler: staticAsyncHandler{
			async.CompletedCheck{RuleCode: "test/rule", File: "Dockerfile"},
		},
	}

	attachInvocationToAsyncRequest(req, inv)

	results := req.Handler.OnSuccess(nil)
	if len(results) != 1 {
		t.Fatalf("OnSuccess returned %d results, want 1", len(results))
	}
	completed, ok := results[0].(async.CompletedCheck)
	if !ok {
		t.Fatalf("OnSuccess result type = %T, want async.CompletedCheck", results[0])
	}
	if completed.InvocationKey != inv.Key {
		t.Fatalf("completed InvocationKey = %q, want %q", completed.InvocationKey, inv.Key)
	}
}

type staticAsyncHandler []any

func (h staticAsyncHandler) OnSuccess(any) []any {
	out := make([]any, len(h))
	copy(out, h)
	return out
}
