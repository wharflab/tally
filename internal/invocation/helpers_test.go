package invocation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvocationKeyUsesUnambiguousSeparator(t *testing.T) {
	t.Parallel()

	first := InvocationKey(InvocationSource{Kind: "compose", File: "a|b", Name: "c"}, "d")
	second := InvocationKey(InvocationSource{Kind: "compose", File: "a", Name: "b|c"}, "d")
	if first == second {
		t.Fatalf("InvocationKey collision: %q", first)
	}
	if strings.Contains(first, "|") && !strings.Contains(first, "\x00") {
		t.Fatalf("InvocationKey = %q, want NUL-separated fields", first)
	}
}

func TestClassifyContextRefReturnsAbsoluteLocalDir(t *testing.T) {
	t.Parallel()

	ref, err := ClassifyContextRef(filepath.Join(".", "relative-context"), ".")
	if err != nil {
		t.Fatalf("ClassifyContextRef() error: %v", err)
	}
	if ref.Kind != ContextKindDir {
		t.Fatalf("kind = %q, want %q", ref.Kind, ContextKindDir)
	}
	if !filepath.IsAbs(ref.Value) {
		t.Fatalf("context value = %q, want absolute path", ref.Value)
	}
}

func TestProbeEntrypointKindJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.json")
	if err := os.WriteFile(composePath, []byte(`{"services":{"app":{"build":"."}}}`), 0o644); err != nil {
		t.Fatalf("write compose JSON: %v", err)
	}
	bakePath := filepath.Join(dir, "docker-bake.json")
	if err := os.WriteFile(bakePath, []byte(`{"target":{"app":{"context":"."}}}`), 0o644); err != nil {
		t.Fatalf("write Bake JSON: %v", err)
	}

	if got, ok := ProbeEntrypointKind(composePath); !ok || got != KindCompose {
		t.Fatalf("ProbeEntrypointKind(compose) = %q, %v; want %q, true", got, ok, KindCompose)
	}
	if got, ok := ProbeEntrypointKind(bakePath); !ok || got != KindBake {
		t.Fatalf("ProbeEntrypointKind(bake) = %q, %v; want %q, true", got, ok, KindBake)
	}
}

func TestProbeEntrypointKindText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.conf")
	if err := os.WriteFile(composePath, []byte("# comment\nname: app\nservices:\n  api:\n    build: .\n"), 0o644); err != nil {
		t.Fatalf("write compose text: %v", err)
	}
	spacedComposePath := filepath.Join(dir, "compose-spaced.conf")
	if err := os.WriteFile(spacedComposePath, []byte("name: app\nservices :\n  api:\n    build: .\n"), 0o644); err != nil {
		t.Fatalf("write spaced compose text: %v", err)
	}
	bakePath := filepath.Join(dir, "Bakefile")
	if err := os.WriteFile(bakePath, []byte("// comment\nvariable \"TAG\" {}\ntarget \"api\" {}\n"), 0o644); err != nil {
		t.Fatalf("write Bake text: %v", err)
	}
	compactBakePath := filepath.Join(dir, "Bakefile.compact")
	if err := os.WriteFile(compactBakePath, []byte("/* target \"ignored\" {} */\ntarget\"api\"{}\n"), 0o644); err != nil {
		t.Fatalf("write compact Bake text: %v", err)
	}
	otherPath := filepath.Join(dir, "README")
	if err := os.WriteFile(otherPath, []byte("not an orchestrator\n"), 0o644); err != nil {
		t.Fatalf("write other text: %v", err)
	}

	if got, ok := ProbeEntrypointKind(composePath); !ok || got != KindCompose {
		t.Fatalf("ProbeEntrypointKind(compose text) = %q, %v; want %q, true", got, ok, KindCompose)
	}
	if got, ok := ProbeEntrypointKind(spacedComposePath); !ok || got != KindCompose {
		t.Fatalf("ProbeEntrypointKind(spaced compose text) = %q, %v; want %q, true", got, ok, KindCompose)
	}
	if got, ok := ProbeEntrypointKind(bakePath); !ok || got != KindBake {
		t.Fatalf("ProbeEntrypointKind(Bakefile) = %q, %v; want %q, true", got, ok, KindBake)
	}
	if got, ok := ProbeEntrypointKind(compactBakePath); !ok || got != KindBake {
		t.Fatalf("ProbeEntrypointKind(compact Bakefile) = %q, %v; want %q, true", got, ok, KindBake)
	}
	if got, ok := ProbeEntrypointKind(otherPath); ok || got != "" {
		t.Fatalf("ProbeEntrypointKind(other) = %q, %v; want empty, false", got, ok)
	}
}

func TestParsePortRangeRejectsOutOfRangePorts(t *testing.T) {
	t.Parallel()

	tests := []string{"-1", "65536", "80-65536"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, _, err := parsePortRange(value); err == nil {
				t.Fatalf("parsePortRange(%q) error = nil, want out-of-range error", value)
			}
		})
	}
}
