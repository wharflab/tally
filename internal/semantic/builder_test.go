package semantic

import (
	"fmt"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/shell"
)

const testShellBash = "bash"

func TestBuilderWithShellDirectivesAppliesToFollowingStages(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18 AS s0
# tally shell=dash
# tally shell=bash
FROM alpine:3.18 AS s1
RUN echo "ok"
`
	pr := parseDockerfile(t, content)

	// Intentionally pass directives out of order to ensure builder picks by line, not slice order.
	directives := []ShellDirective{
		{Shell: "bash", Line: 2},
		{Shell: "dash", Line: 1},
	}

	model := NewBuilder(pr, nil, "Dockerfile").
		WithShellDirectives(directives).
		Build()

	// Stage 0: directives appear after its FROM, so they must not apply.
	info0 := model.StageInfo(0)
	if info0.ShellSetting.Source != ShellSourceDefault {
		t.Errorf("expected stage 0 ShellSetting.Source=%v, got %v", ShellSourceDefault, info0.ShellSetting.Source)
	}
	if info0.ShellSetting.Line != -1 {
		t.Errorf("expected stage 0 ShellSetting.Line=-1, got %d", info0.ShellSetting.Line)
	}

	// Stage 1: should pick the most recent directive before its FROM (line 2 -> bash).
	info1 := model.StageInfo(1)
	if info1.ShellSetting.Source != ShellSourceDirective {
		t.Errorf("expected stage 1 ShellSetting.Source=%v, got %v", ShellSourceDirective, info1.ShellSetting.Source)
	}
	if info1.ShellSetting.Line != 2 {
		t.Errorf("expected stage 1 ShellSetting.Line=2, got %d", info1.ShellSetting.Line)
	}
	if info1.ShellSetting.Variant != shell.VariantBash {
		t.Errorf("expected stage 1 ShellSetting.Variant=%v, got %v", shell.VariantBash, info1.ShellSetting.Variant)
	}
	// Directive should also propagate the shell name into ShellSetting.Shell.
	if len(info1.ShellSetting.Shell) == 0 || info1.ShellSetting.Shell[0] != testShellBash {
		t.Errorf("expected stage 1 ShellSetting.Shell[0]=%q, got %v", testShellBash, info1.ShellSetting.Shell)
	}
}

func TestShellNameAtLineTracksTransitions(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN echo "default shell"
SHELL ["/bin/bash", "-c"]
RUN echo "bash shell"
SHELL ["/bin/dash", "-c"]
RUN echo "dash shell"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")
	info := model.StageInfo(0)

	// Line 2: RUN before any SHELL → default "/bin/sh"
	if got := info.ShellNameAtLine(2); got != "/bin/sh" {
		t.Errorf("line 2: ShellNameAtLine=%q, want %q", got, "/bin/sh")
	}
	// Line 3: SHELL instruction itself → still "/bin/sh" (SHELL hasn't taken effect yet)
	if got := info.ShellNameAtLine(3); got != "/bin/sh" {
		t.Errorf("line 3: ShellNameAtLine=%q, want %q", got, "/bin/sh")
	}
	// Line 4: RUN after SHELL ["/bin/bash"] → "/bin/bash"
	if got := info.ShellNameAtLine(4); got != "/bin/bash" {
		t.Errorf("line 4: ShellNameAtLine=%q, want %q", got, "/bin/bash")
	}
	// Line 6: RUN after SHELL ["/bin/dash"] → "/bin/dash"
	if got := info.ShellNameAtLine(6); got != "/bin/dash" {
		t.Errorf("line 6: ShellNameAtLine=%q, want %q", got, "/bin/dash")
	}
}

func TestShellVariantAtLineTracksTransitions(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN echo "default"
SHELL ["/bin/bash", "-c"]
RUN echo "bash"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")
	info := model.StageInfo(0)

	// Before SHELL: default variant (POSIX for /bin/sh)
	if got := info.ShellVariantAtLine(2); got != shell.VariantPOSIX {
		t.Errorf("line 2: ShellVariantAtLine=%v, want VariantPOSIX", got)
	}
	// After SHELL ["/bin/bash"]: VariantBash
	if got := info.ShellVariantAtLine(4); got != shell.VariantBash {
		t.Errorf("line 4: ShellVariantAtLine=%v, want VariantBash", got)
	}
}

func TestShellNameAtLineWithDirective(t *testing.T) {
	t.Parallel()
	content := `# tally shell=bash
FROM alpine:3.18
RUN echo "bash via directive"
`
	pr := parseDockerfile(t, content)
	directives := []ShellDirective{{Shell: testShellBash, Line: 0}}

	model := NewBuilder(pr, nil, "Dockerfile").
		WithShellDirectives(directives).
		Build()
	info := model.StageInfo(0)

	// Directive sets shell to bash, so all lines should reflect that.
	if got := info.ShellNameAtLine(3); got != testShellBash {
		t.Errorf("line 3: ShellNameAtLine=%q, want %q", got, testShellBash)
	}
	if got := info.ShellVariantAtLine(3); got != shell.VariantBash {
		t.Errorf("line 3: ShellVariantAtLine=%v, want VariantBash", got)
	}
}

func TestShellNameAtLineFallbackForUnmappedLines(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN echo "hello"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")
	info := model.StageInfo(0)

	// Querying an unmapped line (e.g., a blank or comment line) should return
	// the fallback shell name.
	if got := info.ShellNameAtLine(999); got != "/bin/sh" {
		t.Errorf("unmapped line: ShellNameAtLine=%q, want %q", got, "/bin/sh")
	}
}

func TestProcessShellCommandEmptyShellDoesNotPanic(t *testing.T) {
	t.Parallel()

	b := NewBuilder(nil, nil, "Dockerfile")
	info := &StageInfo{
		BaseImageOS: BaseImageOSUnknown,
		ShellSetting: ShellSetting{
			Shell:   DefaultShell,
			Variant: shell.VariantBash,
			Source:  ShellSourceDefault,
			Line:    -1,
		},
	}

	b.processShellCommand(&instructions.ShellCommand{}, info)

	if info.BaseImageOS != BaseImageOSUnknown {
		t.Fatalf("expected BaseImageOSUnknown, got %v", info.BaseImageOS)
	}
	if info.ShellSetting.Source != ShellSourceInstruction {
		t.Fatalf("expected shell source %v, got %v", ShellSourceInstruction, info.ShellSetting.Source)
	}
	if len(info.ShellSetting.Shell) != 0 {
		t.Fatalf("expected empty shell setting, got %v", info.ShellSetting.Shell)
	}
}

func TestBuilderExtractPackageInstallsFromRun(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN apk add curl wget
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if len(info.InstalledPackages) != 1 {
		t.Fatalf("expected 1 package install, got %d", len(info.InstalledPackages))
	}

	install := info.InstalledPackages[0]
	if install.Manager != shell.PackageManagerApk {
		t.Errorf("expected manager %q, got %q", shell.PackageManagerApk, install.Manager)
	}
	if install.Line != 2 {
		t.Errorf("expected RUN line=2, got %d", install.Line)
	}
	if len(install.Packages) != 2 || install.Packages[0] != "curl" || install.Packages[1] != "wget" {
		t.Errorf("unexpected packages: %v", install.Packages)
	}
}

func TestBuilderExtractPackageInstallsFromRunHeredoc(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN <<EOF
apk add curl wget
EOF
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if len(info.InstalledPackages) != 1 {
		t.Fatalf("expected 1 package install, got %d", len(info.InstalledPackages))
	}

	install := info.InstalledPackages[0]
	if install.Manager != shell.PackageManagerApk {
		t.Errorf("expected manager %q, got %q", shell.PackageManagerApk, install.Manager)
	}
	if install.Line != 2 {
		t.Errorf("expected RUN line=2, got %d", install.Line)
	}
	if len(install.Packages) != 2 || install.Packages[0] != "curl" || install.Packages[1] != "wget" {
		t.Errorf("unexpected packages: %v", install.Packages)
	}
}

func TestBuilderHeredocShellOverride(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN <<EOF
#!/bin/bash
set -e
echo hello
EOF
RUN echo "no heredoc shebang"
RUN <<SCRIPT
#!/bin/sh
echo world
SCRIPT
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if len(info.HeredocShellOverrides) != 2 {
		t.Fatalf("expected 2 heredoc shell overrides, got %d", len(info.HeredocShellOverrides))
	}

	if info.HeredocShellOverrides[0].Shell != "bash" {
		t.Errorf("override[0].Shell = %q, want %q", info.HeredocShellOverrides[0].Shell, "bash")
	}
	if info.HeredocShellOverrides[0].Variant != shell.VariantBash {
		t.Errorf("override[0].Variant = %v, want VariantBash", info.HeredocShellOverrides[0].Variant)
	}
	if info.HeredocShellOverrides[0].Line != 2 {
		t.Errorf("override[0].Line = %d, want 2", info.HeredocShellOverrides[0].Line)
	}

	if info.HeredocShellOverrides[1].Shell != "sh" {
		t.Errorf("override[1].Shell = %q, want %q", info.HeredocShellOverrides[1].Shell, "sh")
	}
	if info.HeredocShellOverrides[1].Variant != shell.VariantPOSIX {
		t.Errorf("override[1].Variant = %v, want VariantPOSIX", info.HeredocShellOverrides[1].Variant)
	}
}

func TestBuilderCopyFromInvalidNumericDoesNotCreateDependency(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18 AS s0
RUN echo "base"

FROM alpine:3.18 AS s1
COPY --from=1 /a /b
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(1)
	if len(info.CopyFromRefs) != 1 {
		t.Fatalf("expected 1 COPY --from ref, got %d", len(info.CopyFromRefs))
	}
	if info.CopyFromRefs[0].IsStageRef {
		t.Error("expected invalid numeric COPY --from not to be a stage ref")
	}
	if info.CopyFromRefs[0].StageIndex != -1 {
		t.Errorf("expected invalid numeric COPY --from StageIndex=-1, got %d", info.CopyFromRefs[0].StageIndex)
	}

	// Invalid numeric references should not be treated as external images either.
	if refs := model.Graph().ExternalRefs(1); len(refs) != 0 {
		t.Errorf("expected 0 external refs, got %v", refs)
	}
}

func TestBuilderOnbuildCopyFromInvalidNumeric(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
ONBUILD COPY --from=0 /a /b
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if len(info.OnbuildCopyFromRefs) != 1 {
		t.Fatalf("expected 1 ONBUILD COPY --from ref, got %d", len(info.OnbuildCopyFromRefs))
	}
	if info.OnbuildCopyFromRefs[0].IsStageRef {
		t.Error("expected invalid numeric ONBUILD COPY --from not to be a stage ref")
	}
	if info.OnbuildCopyFromRefs[0].StageIndex != -1 {
		t.Errorf("expected invalid numeric ONBUILD COPY --from StageIndex=-1, got %d", info.OnbuildCopyFromRefs[0].StageIndex)
	}
}

func TestBuilderNilInputBuildsEmptyModel(t *testing.T) {
	t.Parallel()
	b := NewBuilder(nil, nil, "Dockerfile")
	model := b.Build()

	if model.StageCount() != 0 {
		t.Fatalf("expected 0 stages, got %d", model.StageCount())
	}
}

func TestParseOnbuildExpressionInvalidExpressionReturnsNil(t *testing.T) {
	t.Parallel()
	// "COPY" without arguments is invalid and should return nil.
	if cmd := parseOnbuildExpression("COPY", 0); cmd != nil {
		t.Errorf("expected nil for invalid ONBUILD expression, got %#v", cmd)
	}
}

func TestParseOnbuildExpressionValidExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expr     string
		wantType string
	}{
		{"RUN command", "RUN echo hello", "*instructions.RunCommand"},
		{"COPY command", "COPY --from=builder /app /app", "*instructions.CopyCommand"},
		{"ENV command", "ENV FOO=bar", "*instructions.EnvCommand"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := parseOnbuildExpression(tt.expr, 0)
			if cmd == nil {
				t.Fatalf("expected non-nil command for %q", tt.expr)
			}
			if got := fmt.Sprintf("%T", cmd); got != tt.wantType {
				t.Errorf("got type %s, want %s", got, tt.wantType)
			}
		})
	}
}

func TestParseOnbuildExpressionPatchesLocation(t *testing.T) {
	t.Parallel()
	cmd := parseOnbuildExpression("RUN echo hello", 42)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	loc := cmd.Location()
	if len(loc) == 0 {
		t.Fatal("expected non-empty location")
	}
	if loc[0].Start.Line != 42 {
		t.Errorf("Start.Line = %d, want 42", loc[0].Start.Line)
	}
}

func TestParseOnbuildExpressionInvalidSyntaxReturnsNil(t *testing.T) {
	t.Parallel()
	// Completely invalid syntax
	if cmd := parseOnbuildExpression("NOT_A_COMMAND ???", 0); cmd != nil {
		t.Errorf("expected nil for invalid syntax, got %#v", cmd)
	}
}
