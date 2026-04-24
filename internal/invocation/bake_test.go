package invocation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBakeProviderDiscoverUnknownRequestedTarget(t *testing.T) {
	t.Parallel()

	entrypoint := writeBakeFixture(t, `target "api" {
  context = "."
}
`)

	_, err := BakeProvider{}.Discover(context.Background(), ResolveOptions{
		Path:    entrypoint,
		Targets: []string{"unknown"},
	})
	if err == nil {
		t.Fatal("expected unknown target error")
	}
	if !strings.Contains(err.Error(), `unknown bake target "unknown"`) {
		t.Fatalf("error = %q, want unknown bake target", err)
	}
}

func TestBakeProviderDiscoverRequestedGroup(t *testing.T) {
	t.Parallel()

	entrypoint := writeBakeFixture(t, `group "default" {
  targets = ["api"]
}

group "backend" {
  targets = ["api", "worker"]
}

target "api" {
  context = "."
}

target "worker" {
  context = "."
}
`)

	result, err := BakeProvider{}.Discover(context.Background(), ResolveOptions{
		Path:    entrypoint,
		Targets: []string{"backend"},
	})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	got := invocationNames(result.Invocations)
	want := []string{"api", "worker"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("invocation names = %v, want %v", got, want)
	}
}

func writeBakeFixture(t *testing.T, bakeContent string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	entrypoint := filepath.Join(dir, "docker-bake.hcl")
	if err := os.WriteFile(entrypoint, []byte(bakeContent), 0o644); err != nil {
		t.Fatalf("write Bake file: %v", err)
	}
	return entrypoint
}

func invocationNames(invocations []BuildInvocation) []string {
	names := make([]string, 0, len(invocations))
	for _, inv := range invocations {
		names = append(names, inv.Source.Name)
	}
	return names
}
