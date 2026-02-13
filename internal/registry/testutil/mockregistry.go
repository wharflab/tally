// Package testutil provides a deterministic mock OCI registry for testing
// async checks that resolve image configs from registries.
package testutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// MockRegistry is an in-memory OCI registry backed by go-containerregistry.
// It tracks HTTP requests for test assertions.
type MockRegistry struct {
	Server   *httptest.Server
	mu       sync.Mutex
	requests []string
	delays   map[string]time.Duration // repo prefix → artificial delay
}

// New creates and starts a mock registry server.
func New() *MockRegistry {
	mr := &MockRegistry{delays: make(map[string]time.Duration)}
	handler := registry.New()
	mr.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := r.Method + " " + r.URL.Path
		mr.mu.Lock()
		mr.requests = append(mr.requests, req)
		// Check for artificial delay on this repo.
		var delay time.Duration
		for prefix, d := range mr.delays {
			if strings.Contains(r.URL.Path, prefix) {
				delay = d
				break
			}
		}
		mr.mu.Unlock()

		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}

		handler.ServeHTTP(w, r)
	}))
	return mr
}

// SetDelay registers an artificial delay for any request whose path contains
// the given repo prefix (e.g. "library/slowimage"). The delay is applied
// before the real handler responds; it is context-aware and cancellable.
func (mr *MockRegistry) SetDelay(repo string, delay time.Duration) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.delays[repo] = delay
}

// Close shuts down the server.
func (mr *MockRegistry) Close() { mr.Server.Close() }

// Host returns "host:port" of the mock registry.
func (mr *MockRegistry) Host() string { return mr.Server.Listener.Addr().String() }

// Requests returns a copy of all requests recorded since the last reset.
func (mr *MockRegistry) Requests() []string {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	out := make([]string, len(mr.requests))
	copy(out, mr.requests)
	return out
}

// ResetRequests clears the recorded requests.
func (mr *MockRegistry) ResetRequests() {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.requests = nil
}

// HasRequest checks whether any recorded request contains the pattern.
func (mr *MockRegistry) HasRequest(pattern string) bool {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	for _, r := range mr.requests {
		if strings.Contains(r, pattern) {
			return true
		}
	}
	return false
}

// ImageOpts configures a single-platform image pushed to the mock registry.
type ImageOpts struct {
	Repo        string            // e.g. "library/python"
	Tag         string            // e.g. "3.12"
	OS          string            // e.g. "linux"
	Arch        string            // e.g. "amd64"
	Variant     string            // e.g. "v8" (optional)
	Env         map[string]string // e.g. {"PATH": "/usr/bin", "PYTHON_VERSION": "3.12"}
	Healthcheck []string          // e.g. {"CMD-SHELL", "curl -f http://localhost/ || exit 1"} (optional)
}

// AddImage pushes a single-platform image and returns its digest.
func (mr *MockRegistry) AddImage(opts ImageOpts) (string, error) {
	img, err := buildImage(opts)
	if err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}

	ref, err := name.ParseReference(mr.Host()+"/"+opts.Repo+":"+opts.Tag, name.Insecure)
	if err != nil {
		return "", fmt.Errorf("parse ref: %w", err)
	}

	if err := remote.Write(ref, img); err != nil {
		return "", fmt.Errorf("push image: %w", err)
	}

	d, err := img.Digest()
	if err != nil {
		return "", err
	}
	return d.String(), nil
}

// AddIndex pushes a multi-arch image index (manifest list) and returns the index digest.
// Each entry in manifests is pushed as a child image under the same repo.
func (mr *MockRegistry) AddIndex(repo, tag string, manifests []ImageOpts) (string, error) {
	var adds []mutate.IndexAddendum
	for _, m := range manifests {
		img, err := buildImage(m)
		if err != nil {
			return "", fmt.Errorf("build image %s/%s: %w", m.OS, m.Arch, err)
		}
		platform := &v1.Platform{
			OS:           m.OS,
			Architecture: m.Arch,
			Variant:      m.Variant,
		}
		adds = append(adds, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				Platform: platform,
			},
		})
	}

	idx := mutate.AppendManifests(empty.Index, adds...)

	ref, err := name.ParseReference(mr.Host()+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		return "", fmt.Errorf("parse ref: %w", err)
	}

	if err := remote.WriteIndex(ref, idx); err != nil {
		return "", fmt.Errorf("push index: %w", err)
	}

	d, err := idx.Digest()
	if err != nil {
		return "", err
	}
	return d.String(), nil
}

// WriteRegistriesConf creates a registries.conf that redirects the given
// registries (default: docker.io) to this mock server. Returns the file path.
// The caller should set CONTAINERS_REGISTRIES_CONF to this path.
func (mr *MockRegistry) WriteRegistriesConf(dir string, registries ...string) (string, error) {
	if len(registries) == 0 {
		registries = []string{"docker.io"}
	}

	confPath := filepath.Join(dir, "registries.conf")
	f, err := os.Create(confPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	for _, reg := range registries {
		if _, err := fmt.Fprintf(f, "[[registry]]\nprefix = %q\nlocation = %q\ninsecure = true\n\n", reg, mr.Host()); err != nil {
			return "", err
		}
	}

	return confPath, nil
}

// buildImage creates a single-platform OCI image with the specified config.
func buildImage(opts ImageOpts) (v1.Image, error) {
	// Start from a random image (gives us a non-empty layer so the manifest is valid).
	img, err := random.Image(256, 1)
	if err != nil {
		return nil, err
	}

	// Set platform via config file.
	cfgFile, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfgFile.OS = opts.OS
	cfgFile.Architecture = opts.Arch
	cfgFile.Variant = opts.Variant

	// Set env.
	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}
	cfgFile.Config.Env = env

	// Set healthcheck if provided.
	if len(opts.Healthcheck) > 0 {
		cfgFile.Config.Healthcheck = &v1.HealthConfig{
			Test: opts.Healthcheck,
		}
	}

	img, err = mutate.ConfigFile(img, cfgFile)
	if err != nil {
		return nil, err
	}

	// Ensure media type is OCI.
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	return img, nil
}
