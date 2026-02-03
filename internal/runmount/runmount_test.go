package runmount

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func parseRun(t *testing.T, dockerfile string) *instructions.RunCommand {
	t.Helper()
	result, err := parser.Parse(strings.NewReader(dockerfile))
	if err != nil {
		t.Fatalf("failed to parse dockerfile: %v", err)
	}

	stages, _, err := instructions.Parse(result.AST, nil)
	if err != nil {
		t.Fatalf("failed to parse instructions: %v", err)
	}

	for _, stage := range stages {
		for _, cmd := range stage.Commands {
			if run, ok := cmd.(*instructions.RunCommand); ok {
				return run
			}
		}
	}
	t.Fatal("no RUN command found")
	return nil
}

func TestGetMounts_CacheMount(t *testing.T) {
	dockerfile := `FROM ubuntu:22.04
RUN --mount=type=cache,target=/var/cache/apt apt-get update
`
	run := parseRun(t, dockerfile)
	mounts := GetMounts(run)

	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}

	m := mounts[0]
	if m.Type != instructions.MountTypeCache {
		t.Errorf("Type = %q, want %q", m.Type, instructions.MountTypeCache)
	}
	if m.Target != "/var/cache/apt" {
		t.Errorf("Target = %q, want %q", m.Target, "/var/cache/apt")
	}
}

func TestGetMounts_BindMount(t *testing.T) {
	dockerfile := `FROM ubuntu:22.04
RUN --mount=type=bind,source=./src,target=/app echo hello
`
	run := parseRun(t, dockerfile)
	mounts := GetMounts(run)

	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}

	m := mounts[0]
	if m.Type != instructions.MountTypeBind {
		t.Errorf("Type = %q, want %q", m.Type, instructions.MountTypeBind)
	}
	if m.Source != "./src" {
		t.Errorf("Source = %q, want %q", m.Source, "./src")
	}
	if m.Target != "/app" {
		t.Errorf("Target = %q, want %q", m.Target, "/app")
	}
}

func TestGetMounts_MultipleMounts(t *testing.T) {
	dockerfile := `FROM ubuntu:22.04
RUN --mount=type=cache,target=/var/cache/apt --mount=type=cache,target=/root/.cache apt-get update
`
	run := parseRun(t, dockerfile)
	mounts := GetMounts(run)

	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(mounts))
	}

	// Check first mount
	if mounts[0].Type != instructions.MountTypeCache {
		t.Errorf("mounts[0].Type = %q, want %q", mounts[0].Type, instructions.MountTypeCache)
	}
	if mounts[0].Target != "/var/cache/apt" {
		t.Errorf("mounts[0].Target = %q, want %q", mounts[0].Target, "/var/cache/apt")
	}

	// Check second mount
	if mounts[1].Type != instructions.MountTypeCache {
		t.Errorf("mounts[1].Type = %q, want %q", mounts[1].Type, instructions.MountTypeCache)
	}
	if mounts[1].Target != "/root/.cache" {
		t.Errorf("mounts[1].Target = %q, want %q", mounts[1].Target, "/root/.cache")
	}
}

func TestGetMounts_NoMount(t *testing.T) {
	dockerfile := `FROM ubuntu:22.04
RUN apt-get update
`
	run := parseRun(t, dockerfile)
	mounts := GetMounts(run)

	if len(mounts) != 0 {
		t.Fatalf("expected 0 mounts, got %d", len(mounts))
	}
}

func TestGetMounts_SecretMount(t *testing.T) {
	dockerfile := `FROM ubuntu:22.04
RUN --mount=type=secret,id=mysecret,target=/run/secrets/mysecret cat /run/secrets/mysecret
`
	run := parseRun(t, dockerfile)
	mounts := GetMounts(run)

	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}

	m := mounts[0]
	if m.Type != instructions.MountTypeSecret {
		t.Errorf("Type = %q, want %q", m.Type, instructions.MountTypeSecret)
	}
	if m.CacheID != "mysecret" {
		t.Errorf("CacheID = %q, want %q", m.CacheID, "mysecret")
	}
	if m.Target != "/run/secrets/mysecret" {
		t.Errorf("Target = %q, want %q", m.Target, "/run/secrets/mysecret")
	}
}

func TestGetMounts_CacheWithSharing(t *testing.T) {
	dockerfile := `FROM ubuntu:22.04
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update
`
	run := parseRun(t, dockerfile)
	mounts := GetMounts(run)

	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}

	m := mounts[0]
	if m.CacheSharing != instructions.MountSharingLocked {
		t.Errorf("CacheSharing = %q, want %q", m.CacheSharing, instructions.MountSharingLocked)
	}
}

func TestMountsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []*instructions.Mount
		b    []*instructions.Mount
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "both empty",
			a:    []*instructions.Mount{},
			b:    []*instructions.Mount{},
			want: true,
		},
		{
			name: "nil vs empty",
			a:    nil,
			b:    []*instructions.Mount{},
			want: true,
		},
		{
			name: "one nil one with mount",
			a:    nil,
			b:    []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache"}},
			want: false,
		},
		{
			name: "same single mount",
			a:    []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache"}},
			b:    []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache"}},
			want: true,
		},
		{
			name: "different mount type",
			a:    []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache"}},
			b:    []*instructions.Mount{{Type: instructions.MountTypeBind, Target: "/cache"}},
			want: false,
		},
		{
			name: "different target",
			a:    []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache1"}},
			b:    []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache2"}},
			want: false,
		},
		{
			name: "same multiple mounts same order",
			a: []*instructions.Mount{
				{Type: instructions.MountTypeCache, Target: "/cache"},
				{Type: instructions.MountTypeBind, Source: "/src", Target: "/app"},
			},
			b: []*instructions.Mount{
				{Type: instructions.MountTypeCache, Target: "/cache"},
				{Type: instructions.MountTypeBind, Source: "/src", Target: "/app"},
			},
			want: true,
		},
		{
			name: "same multiple mounts different order",
			a: []*instructions.Mount{
				{Type: instructions.MountTypeCache, Target: "/cache"},
				{Type: instructions.MountTypeBind, Source: "/src", Target: "/app"},
			},
			b: []*instructions.Mount{
				{Type: instructions.MountTypeBind, Source: "/src", Target: "/app"},
				{Type: instructions.MountTypeCache, Target: "/cache"},
			},
			want: true, // Order-independent comparison
		},
		{
			name: "different number of mounts",
			a: []*instructions.Mount{
				{Type: instructions.MountTypeCache, Target: "/cache"},
			},
			b: []*instructions.Mount{
				{Type: instructions.MountTypeCache, Target: "/cache"},
				{Type: instructions.MountTypeBind, Source: "/src", Target: "/app"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MountsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("MountsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatMount(t *testing.T) {
	tests := []struct {
		name  string
		mount *instructions.Mount
		want  string
	}{
		{
			name: "cache mount basic",
			mount: &instructions.Mount{
				Type:   instructions.MountTypeCache,
				Target: "/var/cache/apt",
			},
			want: "--mount=type=cache,target=/var/cache/apt",
		},
		{
			name: "cache mount with id",
			mount: &instructions.Mount{
				Type:    instructions.MountTypeCache,
				Target:  "/var/cache/apt",
				CacheID: "apt-cache",
			},
			want: "--mount=type=cache,target=/var/cache/apt,id=apt-cache",
		},
		{
			name: "cache mount with sharing",
			mount: &instructions.Mount{
				Type:         instructions.MountTypeCache,
				Target:       "/var/cache/apt",
				CacheSharing: instructions.MountSharingLocked,
			},
			want: "--mount=type=cache,target=/var/cache/apt,sharing=locked",
		},
		{
			name: "bind mount",
			mount: &instructions.Mount{
				Type:     instructions.MountTypeBind,
				Source:   "/src",
				Target:   "/app",
				ReadOnly: true, // Default for bind
			},
			want: "--mount=type=bind,target=/app,source=/src",
		},
		{
			name: "bind mount rw",
			mount: &instructions.Mount{
				Type:     instructions.MountTypeBind,
				Source:   "/src",
				Target:   "/app",
				ReadOnly: false,
			},
			want: "--mount=type=bind,target=/app,source=/src,rw",
		},
		{
			name: "secret mount",
			mount: &instructions.Mount{
				Type:    instructions.MountTypeSecret,
				CacheID: "mysecret",
				Target:  "/run/secrets/mysecret",
			},
			want: "--mount=type=secret,id=mysecret,target=/run/secrets/mysecret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMount(tt.mount)
			if got != tt.want {
				t.Errorf("FormatMount() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatMounts(t *testing.T) {
	mounts := []*instructions.Mount{
		{Type: instructions.MountTypeCache, Target: "/var/cache/apt"},
		{Type: instructions.MountTypeCache, Target: "/root/.cache"},
	}

	got := FormatMounts(mounts)
	want := "--mount=type=cache,target=/var/cache/apt --mount=type=cache,target=/root/.cache"

	if got != want {
		t.Errorf("FormatMounts() = %q, want %q", got, want)
	}
}
