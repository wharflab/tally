package invocation

import "testing"

func TestInvocationContextInvocationNilSafe(t *testing.T) {
	t.Parallel()

	var ctx *InvocationContext
	if got := ctx.Invocation(); got != nil {
		t.Fatalf("nil context Invocation() = %#v, want nil", got)
	}

	inv := &BuildInvocation{DockerfilePath: "/workspace/Dockerfile"}
	ctx = NewContext(inv)
	if got := ctx.Invocation(); got != inv {
		t.Fatalf("Invocation() = %#v, want %#v", got, inv)
	}
}
