//go:build containers_image_openpgp && containers_image_storage_stub && containers_image_docker_daemon_stub

package registry

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/types"
)

// These tests hit real Docker Hub and are gated behind TALLY_TEST_REGISTRY=1.
// They're useful for debugging registry connectivity but should not run in CI.

func TestDebugHTTP(t *testing.T) {
	if os.Getenv("TALLY_TEST_REGISTRY") == "" {
		t.Skip("set TALLY_TEST_REGISTRY=1 to run real registry tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry-1.docker.io/v2/", http.NoBody)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	t.Logf("HTTP elapsed: %s, err=%v", time.Since(start), err)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	t.Logf("status: %d", resp.StatusCode)
}

func TestDebugResolveECR(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stderr)

	resolver := NewContainersResolverWithContext(&types.SystemContext{
		DockerRegistryUserAgent:     "tally/test",
		SystemRegistriesConfPath:    "/dev/null",
		SystemRegistriesConfDirPath: "/dev/null",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	start := time.Now()
	cfg, err := resolver.ResolveConfig(ctx, "public.ecr.aws/docker/library/alpine:3.19", "linux/amd64")
	elapsed := time.Since(start)
	t.Logf("ResolveConfig took %s", elapsed)
	if err != nil {
		t.Logf("error type: %T", err)
		t.Logf("error: %v", err)
		t.Fatal(err)
	}
	t.Logf("config: OS=%s Arch=%s Variant=%s Digest=%s", cfg.OS, cfg.Arch, cfg.Variant, cfg.Digest)
	t.Logf("env: %v", cfg.Env)
}

func TestDebugResolveDockerHub(t *testing.T) {
	if os.Getenv("TALLY_TEST_REGISTRY") == "" {
		t.Skip("set TALLY_TEST_REGISTRY=1 to run Docker Hub tests (may be rate-limited)")
	}
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stderr)

	resolver := NewContainersResolverWithContext(&types.SystemContext{
		DockerRegistryUserAgent:     "tally/test",
		SystemRegistriesConfPath:    "/dev/null",
		SystemRegistriesConfDirPath: "/dev/null",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	start := time.Now()
	cfg, err := resolver.ResolveConfig(ctx, "alpine:3.19", "linux/amd64")
	elapsed := time.Since(start)
	t.Logf("ResolveConfig took %s", elapsed)
	if err != nil {
		t.Logf("error type: %T", err)
		t.Logf("error: %v", err)
		var netErr *NetworkError
		if errors.As(err, &netErr) {
			t.Logf("correctly classified as NetworkError")
		} else {
			t.Errorf("expected NetworkError, got %T: %v", err, err)
		}
		return
	}
	t.Logf("config: OS=%s Arch=%s Env=%v", cfg.OS, cfg.Arch, cfg.Env)
}
