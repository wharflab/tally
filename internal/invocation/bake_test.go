package invocation

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/docker/buildx/util/buildflags"
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

	entrypoint := writeBakeFixture(t, `// Deliberately present so requesting "backend" does not fall through to default.
group "default" {
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
	if !slices.Equal(got, want) {
		t.Fatalf("invocation names = %v, want %v", got, want)
	}
}

func TestBakeProviderDiscoverRejectsSiblingBakeFile(t *testing.T) {
	t.Parallel()

	entrypoint := writeBakeFixture(t, `target "api" {
  context = "."
}
`)
	sibling := filepath.Join(filepath.Dir(entrypoint), "docker-bake.override.hcl")
	if err := os.WriteFile(sibling, []byte(`target "worker" {
  context = "."
}
`), 0o600); err != nil {
		t.Fatalf("write sibling Bake file: %v", err)
	}

	_, err := BakeProvider{}.Discover(context.Background(), ResolveOptions{Path: entrypoint})
	if err == nil {
		t.Fatal("expected multi-file Bake error")
	}
	if !strings.Contains(err.Error(), "multi-file Bake setup") {
		t.Fatalf("error = %q, want multi-file Bake setup", err)
	}
	if !strings.Contains(err.Error(), "docker-bake.override.hcl") {
		t.Fatalf("error = %q, want sibling file name", err)
	}
}

func TestBakeProviderDiscoverIgnoresUnrelatedSiblingHCLAndJSON(t *testing.T) {
	t.Parallel()

	entrypoint := writeBakeFixture(t, `target "api" {
  context = "."
}
`)
	dir := filepath.Dir(entrypoint)
	if err := os.WriteFile(filepath.Join(dir, "terraform.hcl"), []byte(`resource "x" "y" {}`), 0o600); err != nil {
		t.Fatalf("write unrelated HCL: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"build":"go build"}}`), 0o600); err != nil {
		t.Fatalf("write unrelated JSON: %v", err)
	}

	result, err := BakeProvider{}.Discover(context.Background(), ResolveOptions{
		Path:    entrypoint,
		Targets: []string{"api"},
	})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if got := invocationNames(result.Invocations); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("invocation names = %v, want [api]", got)
	}
}

func TestBakeSecretsDiscriminatesEnvSource(t *testing.T) {
	t.Parallel()

	secrets := bakeSecrets(t.TempDir(), buildflags.Secrets{&buildflags.Secret{
		ID:  "token",
		Env: "TOKEN",
	}})
	if len(secrets) != 1 {
		t.Fatalf("bakeSecrets() returned %d secrets, want 1", len(secrets))
	}
	if secrets[0].Source != "env:TOKEN" {
		t.Fatalf("secret Source = %q, want env:TOKEN", secrets[0].Source)
	}
}

func writeBakeFixture(t *testing.T, bakeContent string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	entrypoint := filepath.Join(dir, "docker-bake.hcl")
	if err := os.WriteFile(entrypoint, []byte(bakeContent), 0o600); err != nil {
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
