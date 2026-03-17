package semantic

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wharflab/tally/internal/shell"
)

func TestDetectBaseImageOS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     string
		platform string
		want     BaseImageOS
	}{
		// Windows via platform
		{"platform windows/amd64", "myimage", "windows/amd64", BaseImageOSWindows},
		{"platform Windows/amd64", "myimage", "Windows/amd64", BaseImageOSWindows},
		// Linux via platform
		{"platform linux/amd64", "myimage", "linux/amd64", BaseImageOSLinux},
		{"platform linux/arm64", "myimage", "linux/arm64", BaseImageOSLinux},

		// Windows MCR images
		{"servercore ltsc2019", "mcr.microsoft.com/windows/servercore:ltsc2019", "", BaseImageOSWindows},
		{"servercore ltsc2022", "mcr.microsoft.com/windows/servercore:ltsc2022", "", BaseImageOSWindows},
		{"nanoserver", "mcr.microsoft.com/windows/nanoserver:ltsc2022", "", BaseImageOSWindows},
		{"windows base", "mcr.microsoft.com/windows:ltsc2022", "", BaseImageOSWindows},
		{"servercore/iis", "mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019", "", BaseImageOSWindows},
		{"dotnet nanoserver", "mcr.microsoft.com/dotnet/runtime:8.0-nanoserver-ltsc2022", "", BaseImageOSWindows},
		{"dotnet servercore", "mcr.microsoft.com/dotnet/sdk:8.0-windowsservercore-ltsc2022", "", BaseImageOSWindows},
		{"powershell nanoserver", "mcr.microsoft.com/powershell:nanoserver-ltsc2022", "", BaseImageOSWindows},

		// Linux MCR images
		{"dotnet linux", "mcr.microsoft.com/dotnet/sdk:8.0", "", BaseImageOSLinux},
		{"dotnet bookworm", "mcr.microsoft.com/dotnet/aspnet:8.0-bookworm-slim", "", BaseImageOSLinux},
		{"powershell ubuntu", "mcr.microsoft.com/powershell:ubuntu-22.04", "", BaseImageOSLinux},

		// Well-known Linux distros
		{"alpine", "alpine:3.20", "", BaseImageOSLinux},
		{"ubuntu", "ubuntu:22.04", "", BaseImageOSLinux},
		{"debian", "debian:bookworm-slim", "", BaseImageOSLinux},
		{"fedora", "fedora:39", "", BaseImageOSLinux},
		{"busybox", "busybox:latest", "", BaseImageOSLinux},
		{"al2023", "al2023:latest", "", BaseImageOSLinux},
		{"al2", "al2:latest", "", BaseImageOSLinux},
		{"wolfi", "wolfi:latest", "", BaseImageOSLinux},
		{"photon", "photon:5.0", "", BaseImageOSLinux},
		{"opensuse", "opensuse:leap", "", BaseImageOSLinux},
		{"kali-rolling", "kalilinux/kali-rolling:latest", "", BaseImageOSLinux},
		{"kali-last-release", "kalilinux/kali-last-release:latest", "", BaseImageOSLinux},
		{"registry prefixed alpine", "docker.io/library/alpine:3.20", "", BaseImageOSLinux},
		// Digest-pinned refs
		{"alpine digest", "alpine@sha256:abcdef1234567890", "", BaseImageOSLinux},
		{"ubuntu digest", "ubuntu@sha256:abcdef1234567890", "", BaseImageOSLinux},
		// Registry-prefixed with digest
		{"ghcr alpine", "ghcr.io/library/alpine:3.20", "", BaseImageOSLinux},
		// Registry-prefixed kali
		{"registry kali", "docker.io/kalilinux/kali-rolling:latest", "", BaseImageOSLinux},
		// Registry-prefixed Windows
		{"registry servercore", "mcr.microsoft.com/windows/servercore:ltsc2022", "", BaseImageOSWindows},

		// Unknown
		{"scratch", "scratch", "", BaseImageOSUnknown},
		{"custom image", "mycompany/myapp:latest", "", BaseImageOSUnknown},
		{"stage ref", "builder", "", BaseImageOSUnknown},
		{"empty", "", "", BaseImageOSUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := detectBaseImageOS(tt.base, tt.platform)
			if got != tt.want {
				t.Errorf("detectBaseImageOS(%q, %q) = %v, want %v", tt.base, tt.platform, got, tt.want)
			}
		})
	}
}

func TestInferStageOSHeuristically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    BaseImageOS
	}{
		{
			name: "windows signals",
			content: `FROM ${base}
RUN setx /M PATH "%PATH%;C:\Tools"
RUN cmd /c icacls.exe C:\\BuildAgent\\* /grant:r Users:(OI)(CI)F
`,
			want: BaseImageOSWindows,
		},
		{
			name: "linux signals",
			content: `FROM ${base}
RUN apk add --no-cache curl git
RUN chmod +x /usr/local/bin/tool
`,
			want: BaseImageOSLinux,
		},
		{
			name: "pwsh alone is neutral",
			content: `FROM ${base}
RUN pwsh -NoLogo -NoProfile -Command "Write-Host hi"
`,
			want: BaseImageOSUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pr := parseDockerfile(t, tt.content)
			if len(pr.Stages) != 1 {
				t.Fatalf("expected 1 stage, got %d", len(pr.Stages))
			}

			got := inferStageOSHeuristically(&pr.Stages[0])
			if got != tt.want {
				t.Fatalf("inferStageOSHeuristically() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuilderRealWorldTeamCityNanoServerFixtureInfersWindowsSecondStage(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "integration", "testdata", "real-world-fix-teamcity-nanoserver", "Dockerfile"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	pr := parseDockerfile(t, string(content))
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(1)
	if info == nil {
		t.Fatal("expected second stage info")
	}
	if !info.IsWindows() {
		t.Fatalf("expected second stage to infer Windows, got %v", info.BaseImageOS)
	}

	wantShell := DefaultWindowsShell()
	if len(info.ShellSetting.Shell) != len(wantShell) {
		t.Fatalf("expected shell %v, got %v", wantShell, info.ShellSetting.Shell)
	}
	for i, part := range wantShell {
		if info.ShellSetting.Shell[i] != part {
			t.Fatalf("expected shell[%d]=%q, got %q", i, part, info.ShellSetting.Shell[i])
		}
	}
}

func TestBuilderRealWorldPowerShellAlpineFixtureStaysLinux(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "integration", "testdata", "real-world-fix-powershell-alpine", "Dockerfile"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	pr := parseDockerfile(t, string(content))
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if info == nil {
		t.Fatal("expected stage info")
	}
	if !info.IsLinux() {
		t.Fatalf("expected Linux stage, got %v", info.BaseImageOS)
	}
	if info.ShellSetting.Variant != shell.VariantPOSIX {
		t.Fatalf("expected final shell variant to be POSIX after /bin/ash SHELL, got %v", info.ShellSetting.Variant)
	}
}
