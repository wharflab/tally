package fix

import (
	"context"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

// mockResolver is a test resolver that returns predefined edits.
type mockResolver struct {
	id    string
	edits []rules.TextEdit
	err   error
}

func (m *mockResolver) ID() string { return m.id }

func (m *mockResolver) Resolve(_ context.Context, _ ResolveContext, _ *rules.SuggestedFix) ([]rules.TextEdit, error) {
	return m.edits, m.err
}

func TestResolverRegistry(t *testing.T) {
	// Not parallel: mutates global resolver registry.
	// Clear registry before test
	ClearResolvers()
	defer ClearResolvers()

	// Register a resolver
	r := &mockResolver{id: "test-resolver"}
	RegisterResolver(r)

	// Should be retrievable
	got := GetResolver("test-resolver")
	if got == nil {
		t.Fatal("GetResolver returned nil")
	}
	if got.ID() != "test-resolver" {
		t.Errorf("ID() = %q, want %q", got.ID(), "test-resolver")
	}

	// Unknown resolver should return nil
	if GetResolver("unknown") != nil {
		t.Error("GetResolver should return nil for unknown ID")
	}

	// List should include the resolver
	ids := ListResolvers()
	if len(ids) != 1 || ids[0] != "test-resolver" {
		t.Errorf("ListResolvers() = %v, want [test-resolver]", ids)
	}
}

func TestRegisterResolver_Duplicate(t *testing.T) {
	// Not parallel: mutates global resolver registry.
	ClearResolvers()
	defer ClearResolvers()

	r := &mockResolver{id: "dup"}
	RegisterResolver(r)

	// Registering duplicate should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic on duplicate registration")
		}
	}()

	RegisterResolver(&mockResolver{id: "dup"})
}

func TestMockResolver_Resolve(t *testing.T) {
	t.Parallel()
	loc := rules.NewLineLocation("Dockerfile", 1)
	edits := []rules.TextEdit{
		{Location: loc, NewText: "new text"},
	}

	r := &mockResolver{
		id:    "test",
		edits: edits,
	}

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   "test",
	}

	got, err := r.Resolve(context.Background(), ResolveContext{}, fix)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(edits) = %d, want 1", len(got))
	}
	if got[0].NewText != "new text" {
		t.Errorf("NewText = %q, want %q", got[0].NewText, "new text")
	}
}
