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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			got := MountsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("MountsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatMount(t *testing.T) {
	t.Parallel()
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
		{
			name: "tmpfs mount",
			mount: &instructions.Mount{
				Type:      instructions.MountTypeTmpfs,
				Target:    "/tmp/build",
				SizeLimit: 1073741824, // 1GB
			},
			want: "--mount=type=tmpfs,target=/tmp/build,size=1073741824",
		},
		{
			name: "ssh mount",
			mount: &instructions.Mount{
				Type:    instructions.MountTypeSSH,
				CacheID: "default",
			},
			want: "--mount=type=ssh,id=default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatMount(tt.mount)
			if got != tt.want {
				t.Errorf("FormatMount() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatMounts(t *testing.T) {
	t.Parallel()
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

func TestFormatMounts_Empty(t *testing.T) {
	t.Parallel()
	got := FormatMounts(nil)
	if got != "" {
		t.Errorf("FormatMounts(nil) = %q, want empty string", got)
	}

	got = FormatMounts([]*instructions.Mount{})
	if got != "" {
		t.Errorf("FormatMounts([]) = %q, want empty string", got)
	}
}

func TestFormatMount_WithUIDGIDMode(t *testing.T) {
	t.Parallel()
	uid := uint64(1000)
	gid := uint64(1000)
	mode := uint64(0o755)

	tests := []struct {
		name  string
		mount *instructions.Mount
		want  string
	}{
		{
			name: "cache mount with uid/gid/mode",
			mount: &instructions.Mount{
				Type:   instructions.MountTypeCache,
				Target: "/cache",
				UID:    &uid,
				GID:    &gid,
				Mode:   &mode,
			},
			want: "--mount=type=cache,target=/cache,uid=1000,gid=1000,mode=0755",
		},
		{
			name: "secret mount with uid/gid/mode",
			mount: &instructions.Mount{
				Type:    instructions.MountTypeSecret,
				CacheID: "mysecret",
				UID:     &uid,
				GID:     &gid,
				Mode:    &mode,
			},
			want: "--mount=type=secret,id=mysecret,uid=1000,gid=1000,mode=0755",
		},
		{
			name: "ssh mount with required",
			mount: &instructions.Mount{
				Type:     instructions.MountTypeSSH,
				CacheID:  "default",
				Required: true,
			},
			want: "--mount=type=ssh,id=default,required",
		},
		{
			name: "cache mount with from and source",
			mount: &instructions.Mount{
				Type:   instructions.MountTypeCache,
				Target: "/cache",
				From:   "builder",
				Source: "/build/cache",
			},
			want: "--mount=type=cache,target=/cache,from=builder,source=/build/cache",
		},
		{
			name: "cache mount with readonly",
			mount: &instructions.Mount{
				Type:     instructions.MountTypeCache,
				Target:   "/cache",
				ReadOnly: true,
			},
			want: "--mount=type=cache,target=/cache,ro",
		},
		{
			name: "bind mount with from",
			mount: &instructions.Mount{
				Type:     instructions.MountTypeBind,
				Target:   "/app",
				From:     "builder",
				ReadOnly: true,
			},
			want: "--mount=type=bind,target=/app,from=builder",
		},
		{
			name: "secret mount with target",
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
			t.Parallel()
			got := FormatMount(tt.mount)
			if got != tt.want {
				t.Errorf("FormatMount() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMountEqual_AllFields(t *testing.T) {
	t.Parallel()
	uid1 := uint64(1000)
	uid2 := uint64(2000)
	uid1Copy := uint64(1000) // Same value, different pointer

	tests := []struct {
		name string
		a    *instructions.Mount
		b    *instructions.Mount
		want bool
	}{
		{
			name: "different source",
			a:    &instructions.Mount{Type: instructions.MountTypeBind, Source: "/src1", Target: "/app"},
			b:    &instructions.Mount{Type: instructions.MountTypeBind, Source: "/src2", Target: "/app"},
			want: false,
		},
		{
			name: "different from",
			a:    &instructions.Mount{Type: instructions.MountTypeBind, Target: "/app", From: "stage1"},
			b:    &instructions.Mount{Type: instructions.MountTypeBind, Target: "/app", From: "stage2"},
			want: false,
		},
		{
			name: "different readonly",
			a:    &instructions.Mount{Type: instructions.MountTypeBind, Target: "/app", ReadOnly: true},
			b:    &instructions.Mount{Type: instructions.MountTypeBind, Target: "/app", ReadOnly: false},
			want: false,
		},
		{
			name: "different cache id",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", CacheID: "id1"},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", CacheID: "id2"},
			want: false,
		},
		{
			name: "different cache sharing",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", CacheSharing: instructions.MountSharingShared},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", CacheSharing: instructions.MountSharingLocked},
			want: false,
		},
		{
			name: "different UID - both set",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", UID: &uid1},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", UID: &uid2},
			want: false,
		},
		{
			name: "different UID - one nil",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", UID: &uid1},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", UID: nil},
			want: false,
		},
		{
			name: "same UID",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", UID: &uid1},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", UID: &uid1Copy},
			want: true,
		},
		{
			name: "different GID",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", GID: &uid1},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", GID: &uid2},
			want: false,
		},
		{
			name: "different Mode",
			a:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", Mode: &uid1},
			b:    &instructions.Mount{Type: instructions.MountTypeCache, Target: "/cache", Mode: &uid2},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mountEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("mountEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMountsEqual_SecretSSHKeys(t *testing.T) {
	t.Parallel()
	// Test that secret/ssh mounts use CacheID as key instead of Target
	tests := []struct {
		name string
		a    []*instructions.Mount
		b    []*instructions.Mount
		want bool
	}{
		{
			name: "secret mounts same id",
			a:    []*instructions.Mount{{Type: instructions.MountTypeSecret, CacheID: "mysecret"}},
			b:    []*instructions.Mount{{Type: instructions.MountTypeSecret, CacheID: "mysecret"}},
			want: true,
		},
		{
			name: "secret mounts different id",
			a:    []*instructions.Mount{{Type: instructions.MountTypeSecret, CacheID: "secret1"}},
			b:    []*instructions.Mount{{Type: instructions.MountTypeSecret, CacheID: "secret2"}},
			want: false,
		},
		{
			name: "ssh mounts same id",
			a:    []*instructions.Mount{{Type: instructions.MountTypeSSH, CacheID: "default"}},
			b:    []*instructions.Mount{{Type: instructions.MountTypeSSH, CacheID: "default"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MountsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("MountsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUint64PtrEqual(t *testing.T) {
	t.Parallel()
	val1 := uint64(100)
	val2 := uint64(200)
	val1Copy := uint64(100)

	tests := []struct {
		name string
		a    *uint64
		b    *uint64
		want bool
	}{
		{name: "both nil", a: nil, b: nil, want: true},
		{name: "a nil b set", a: nil, b: &val1, want: false},
		{name: "a set b nil", a: &val1, b: nil, want: false},
		{name: "both same value", a: &val1, b: &val1Copy, want: true},
		{name: "different values", a: &val1, b: &val2, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := uint64PtrEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("uint64PtrEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatOctal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input uint64
		want  string
	}{
		{0o755, "0755"},
		{0o644, "0644"},
		{0o777, "0777"},
		{0, "0000"},
		{1, "0001"},
		{8, "0010"},   // Octal 10 = decimal 8
		{64, "0100"},  // Octal 100 = decimal 64
		{511, "0777"}, // Octal 777 = decimal 511
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := formatOctal(tt.input)
			if got != tt.want {
				t.Errorf("formatOctal(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatUint64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{1000, "1000"},
		{18446744073709551615, "18446744073709551615"}, // Max uint64
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := formatUint64(tt.input)
			if got != tt.want {
				t.Errorf("formatUint64(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMountsPopulated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mounts []*instructions.Mount
		want   bool
	}{
		{
			name:   "empty slice",
			mounts: []*instructions.Mount{},
			want:   false,
		},
		{
			name:   "unparsed bind mount (default)",
			mounts: []*instructions.Mount{{Type: instructions.MountTypeBind, Target: ""}},
			want:   false,
		},
		{
			name:   "parsed bind mount with target",
			mounts: []*instructions.Mount{{Type: instructions.MountTypeBind, Target: "/app"}},
			want:   true,
		},
		{
			name:   "parsed cache mount",
			mounts: []*instructions.Mount{{Type: instructions.MountTypeCache, Target: "/cache"}},
			want:   true,
		},
		{
			name:   "secret mount with cache id",
			mounts: []*instructions.Mount{{Type: instructions.MountTypeSecret, CacheID: "mysecret"}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mountsPopulated(tt.mounts)
			if got != tt.want {
				t.Errorf("mountsPopulated() = %v, want %v", got, tt.want)
			}
		})
	}
}
