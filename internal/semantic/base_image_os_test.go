package semantic

import "testing"

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
		{"wolfi", "wolfi:latest", "", BaseImageOSLinux},
		{"photon", "photon:5.0", "", BaseImageOSLinux},
		{"registry prefixed alpine", "docker.io/library/alpine:3.20", "", BaseImageOSLinux},

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
