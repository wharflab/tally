package testutil

import (
	"testing"
)

func TestMockRegistry_AddImage(t *testing.T) {
	t.Parallel()
	mr := New()
	defer mr.Close()

	digest, err := mr.AddImage(ImageOpts{
		Repo: "library/alpine",
		Tag:  "3.19",
		OS:   "linux",
		Arch: "amd64",
		Env:  map[string]string{"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
	})
	if err != nil {
		t.Fatalf("AddImage failed: %v", err)
	}
	if digest == "" {
		t.Error("expected non-empty digest")
	}

	// Verify the registry was hit.
	if !mr.HasRequest("PUT") {
		t.Error("expected PUT request to mock registry")
	}

	// Verify request tracking.
	reqs := mr.Requests()
	if len(reqs) == 0 {
		t.Error("expected recorded requests")
	}
	mr.ResetRequests()
	if len(mr.Requests()) != 0 {
		t.Error("expected empty requests after reset")
	}
}

func TestMockRegistry_AddIndex(t *testing.T) {
	t.Parallel()
	mr := New()
	defer mr.Close()

	digest, err := mr.AddIndex("library/python", "3.12", []ImageOpts{
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
		t.Fatalf("AddIndex failed: %v", err)
	}
	if digest == "" {
		t.Error("expected non-empty digest")
	}
}

func TestMockRegistry_WriteRegistriesConf(t *testing.T) {
	t.Parallel()
	mr := New()
	defer mr.Close()

	confPath, err := mr.WriteRegistriesConf(t.TempDir(), "docker.io", "ghcr.io")
	if err != nil {
		t.Fatalf("WriteRegistriesConf failed: %v", err)
	}
	if confPath == "" {
		t.Error("expected non-empty path")
	}
}
