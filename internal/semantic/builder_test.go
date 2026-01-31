package semantic

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/shell"
)

func TestBuilderWithShellDirectivesAppliesToFollowingStages(t *testing.T) {
	content := `FROM alpine:3.18 AS s0
# tally shell=dash
# tally shell=bash
FROM alpine:3.18 AS s1
RUN echo "ok"
`
	pr := parseDockerfile(t, content)

	// Intentionally pass directives out of order to ensure builder picks by line, not slice order.
	directives := []directive.ShellDirective{
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
}

func TestBuilderExtractPackageInstallsFromRun(t *testing.T) {
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

func TestBuilderCopyFromInvalidNumericDoesNotCreateDependency(t *testing.T) {
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

func TestBuilderHelpersHandleNilInputs(t *testing.T) {
	b := NewBuilder(nil, nil, "Dockerfile")
	b.checkDL3061InstructionOrder()
	b.checkDL3043ForbiddenOnbuildTriggers()

	if len(b.issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(b.issues))
	}

	if nodes := topLevelInstructionNodes(nil); nodes != nil {
		t.Errorf("expected nil nodes for nil root, got %v", nodes)
	}
	if kw := onbuildTriggerKeyword(nil); kw != "" {
		t.Errorf("expected empty keyword for nil node, got %q", kw)
	}
}

func TestParseOnbuildCopyInvalidExpressionReturnsNil(t *testing.T) {
	b := NewBuilder(nil, nil, "Dockerfile")
	if cmd := b.parseOnbuildCopy("COPY"); cmd != nil {
		t.Errorf("expected nil for invalid ONBUILD expression, got %#v", cmd)
	}
}
