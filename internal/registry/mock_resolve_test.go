//go:build containers_image_openpgp && containers_image_storage_stub && containers_image_docker_daemon_stub

package registry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wharflab/tally/internal/registry/testutil"
	"go.podman.io/image/v5/types"
)

// TestContainersResolver_MockRegistry tests the full HTTP integration path:
// go-containerregistry mock registry ← containers/image resolver ← registries.conf redirect.
func TestContainersResolver_MockRegistry_SingleImage(t *testing.T) {
	t.Parallel()

	mr := testutil.New()
	defer mr.Close()

	// Push a single-platform image with known env.
	_, err := mr.AddImage(testutil.ImageOpts{
		Repo: "library/alpine",
		Tag:  "3.19",
		OS:   "linux",
		Arch: "amd64",
		Env: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
	})
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	// Create a resolver pointing at the mock registry (no registries.conf needed
	// since we reference the mock host directly).
	resolver := NewContainersResolverWithContext(&types.SystemContext{
		DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := resolver.ResolveConfig(ctx, mr.Host()+"/library/alpine:3.19", "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}

	if cfg.OS != "linux" {
		t.Errorf("OS = %q, want linux", cfg.OS)
	}
	if cfg.Arch != "amd64" {
		t.Errorf("Arch = %q, want amd64", cfg.Arch)
	}
	if cfg.Env["PATH"] != "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" {
		t.Errorf("PATH = %q, want standard PATH", cfg.Env["PATH"])
	}
}

func TestContainersResolver_MockRegistry_MultiArch(t *testing.T) {
	t.Parallel()

	mr := testutil.New()
	defer mr.Close()

	// Push a multi-arch index.
	_, err := mr.AddIndex("library/python", "3.12", []testutil.ImageOpts{
		{
			OS:   "linux",
			Arch: "amd64",
			Env: map[string]string{
				"PATH":           "/usr/local/bin:/usr/bin",
				"PYTHON_VERSION": "3.12.0",
			},
		},
		{
			OS:   "linux",
			Arch: "arm64",
			Env: map[string]string{
				"PATH":           "/usr/local/bin:/usr/bin",
				"PYTHON_VERSION": "3.12.0",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddIndex: %v", err)
	}

	resolver := NewContainersResolverWithContext(&types.SystemContext{
		DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Resolve linux/amd64 variant.
	cfg, err := resolver.ResolveConfig(ctx, mr.Host()+"/library/python:3.12", "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveConfig amd64: %v", err)
	}
	if cfg.OS != "linux" || cfg.Arch != "amd64" {
		t.Errorf("platform = %s/%s, want linux/amd64", cfg.OS, cfg.Arch)
	}
	if cfg.Env["PYTHON_VERSION"] != "3.12.0" {
		t.Errorf("PYTHON_VERSION = %q, want 3.12.0", cfg.Env["PYTHON_VERSION"])
	}

	// Resolve linux/arm64 variant.
	cfg, err = resolver.ResolveConfig(ctx, mr.Host()+"/library/python:3.12", "linux/arm64")
	if err != nil {
		t.Fatalf("ResolveConfig arm64: %v", err)
	}
	if cfg.OS != "linux" || cfg.Arch != "arm64" {
		t.Errorf("platform = %s/%s, want linux/arm64", cfg.OS, cfg.Arch)
	}
}

func TestContainersResolver_MockRegistry_PlatformMismatch(t *testing.T) {
	t.Parallel()

	mr := testutil.New()
	defer mr.Close()

	// Push a single-platform image (linux/arm64 only).
	_, err := mr.AddImage(testutil.ImageOpts{
		Repo: "library/myimage",
		Tag:  "latest",
		OS:   "linux",
		Arch: "arm64",
		Env:  map[string]string{"PATH": "/bin"},
	})
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	resolver := NewContainersResolverWithContext(&types.SystemContext{
		DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Request linux/amd64 — should get PlatformMismatchError.
	cfg, err := resolver.ResolveConfig(ctx, mr.Host()+"/library/myimage:latest", "linux/amd64")
	if err == nil {
		t.Fatal("expected PlatformMismatchError, got nil")
	}

	var platErr *PlatformMismatchError
	if !errors.As(err, &platErr) {
		t.Fatalf("expected PlatformMismatchError, got %T: %v", err, err)
	}
	// Partial config should still have the actual platform.
	if cfg.Arch != "arm64" {
		t.Errorf("partial config Arch = %q, want arm64", cfg.Arch)
	}
}

func TestContainersResolver_MockRegistry_RegistriesConf(t *testing.T) {
	t.Parallel()

	mr := testutil.New()
	defer mr.Close()

	// Push an image under library/alpine.
	_, err := mr.AddImage(testutil.ImageOpts{
		Repo: "library/alpine",
		Tag:  "3.19",
		OS:   "linux",
		Arch: "amd64",
		Env:  map[string]string{"PATH": "/bin"},
	})
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	// Create registries.conf redirecting docker.io to the mock.
	confPath, err := mr.WriteRegistriesConf(t.TempDir(), "docker.io")
	if err != nil {
		t.Fatalf("WriteRegistriesConf: %v", err)
	}

	// Create a resolver using the registries.conf redirect.
	resolver := NewContainersResolverWithContext(&types.SystemContext{
		SystemRegistriesConfPath:    confPath,
		SystemRegistriesConfDirPath: "/dev/null",
		DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mr.ResetRequests()

	// Resolve using the docker.io short name — should be redirected to mock.
	cfg, err := resolver.ResolveConfig(ctx, "alpine:3.19", "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveConfig via registries.conf: %v", err)
	}

	if cfg.OS != "linux" || cfg.Arch != "amd64" {
		t.Errorf("platform = %s/%s, want linux/amd64", cfg.OS, cfg.Arch)
	}

	// Verify the mock was hit (not the real Docker Hub).
	if !mr.HasRequest("/v2/library/alpine/manifests/3.19") {
		t.Errorf("expected mock to receive manifest request, got: %v", mr.Requests())
	}
}
