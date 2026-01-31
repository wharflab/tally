package shell

import (
	"slices"
	"testing"
)

func TestExtractPackageInstalls(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		variant  Variant
		want     []PackageInstallInfo
	}{
		{
			name:    "apt-get install single package",
			script:  "apt-get install -y curl",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl"}},
			},
		},
		{
			name:    "apt-get install multiple packages",
			script:  "apt-get install -y curl wget vim",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl", "wget", "vim"}},
			},
		},
		{
			name:    "apt install",
			script:  "apt install -y nginx",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"nginx"}},
			},
		},
		{
			name:    "apk add",
			script:  "apk add --no-cache curl wget",
			variant: VariantPOSIX,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApk, Packages: []string{"curl", "wget"}},
			},
		},
		{
			name:    "yum install",
			script:  "yum install -y httpd",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerYum, Packages: []string{"httpd"}},
			},
		},
		{
			name:    "dnf install",
			script:  "dnf install -y nodejs npm",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerDnf, Packages: []string{"nodejs", "npm"}},
			},
		},
		{
			name:    "apt-get update && install",
			script:  "apt-get update && apt-get install -y curl",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl"}},
			},
		},
		{
			name:    "apt-get with DEBIAN_FRONTEND",
			script:  "DEBIAN_FRONTEND=noninteractive apt-get install -y tzdata",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"tzdata"}},
			},
		},
		{
			name:    "multiple install commands",
			script:  "apt-get install -y curl && apt-get install -y wget",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl"}},
				{Manager: PackageManagerApt, Packages: []string{"wget"}},
			},
		},
		{
			name:    "apt-get update only (no install)",
			script:  "apt-get update",
			variant: VariantBash,
			want:    nil,
		},
		{
			name:    "apt-get remove (not install)",
			script:  "apt-get remove -y curl",
			variant: VariantBash,
			want:    nil,
		},
		{
			name:    "no package manager",
			script:  "echo hello && ls -la",
			variant: VariantBash,
			want:    nil,
		},
		{
			name:    "pacman -S",
			script:  "pacman -S --noconfirm curl",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerPacman, Packages: []string{"curl"}},
			},
		},
		{
			name:    "zypper install",
			script:  "zypper install -y curl",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerZypper, Packages: []string{"curl"}},
			},
		},
		{
			name:    "emerge packages",
			script:  "emerge dev-util/git app-misc/screen",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerEmerge, Packages: []string{"dev-util/git", "app-misc/screen"}},
			},
		},
		{
			name:    "apt-get with full path",
			script:  "/usr/bin/apt-get install curl",
			variant: VariantBash,
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPackageInstalls(tt.script, tt.variant)

			if len(got) != len(tt.want) {
				t.Errorf("ExtractPackageInstalls() returned %d installs, want %d", len(got), len(tt.want))
				for i, g := range got {
					t.Logf("  got[%d]: manager=%s, packages=%v", i, g.Manager, g.Packages)
				}
				return
			}

			for i, want := range tt.want {
				if got[i].Manager != want.Manager {
					t.Errorf("install[%d].Manager = %q, want %q", i, got[i].Manager, want.Manager)
				}
				if !slices.Equal(got[i].Packages, want.Packages) {
					t.Errorf("install[%d].Packages = %v, want %v", i, got[i].Packages, want.Packages)
				}
			}
		})
	}
}

func TestFilterPackageArgs(t *testing.T) {
	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"curl", "wget"}, []string{"curl", "wget"}},
		{[]string{"-y", "curl"}, []string{"curl"}},
		{[]string{"--yes", "curl", "wget"}, []string{"curl", "wget"}},
		{[]string{"-o", "option=value", "curl"}, []string{"curl"}},
		{[]string{"curl", "&&", "wget"}, []string{"curl"}},
		{[]string{}, nil},
	}

	for _, tt := range tests {
		got := filterPackageArgs(tt.args)
		if !slices.Equal(got, tt.want) {
			t.Errorf("filterPackageArgs(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestExtractPackageInstallsSimple(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []PackageInstallInfo
	}{
		{
			name:   "apt-get install",
			script: "apt-get install -y curl wget",
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl", "wget"}},
			},
		},
		{
			name:   "apt install",
			script: "apt install nginx",
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"nginx"}},
			},
		},
		{
			name:   "apk add",
			script: "apk add curl wget",
			want: []PackageInstallInfo{
				{Manager: PackageManagerApk, Packages: []string{"curl", "wget"}},
			},
		},
		{
			name:   "yum install",
			script: "yum install httpd",
			want: []PackageInstallInfo{
				{Manager: PackageManagerYum, Packages: []string{"httpd"}},
			},
		},
		{
			name:   "dnf install",
			script: "dnf install nodejs npm",
			want: []PackageInstallInfo{
				{Manager: PackageManagerDnf, Packages: []string{"nodejs", "npm"}},
			},
		},
		{
			name:   "with shell operators",
			script: "apt-get install -y curl && echo done",
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl"}},
			},
		},
		{
			name:   "no package manager",
			script: "echo hello world",
			want:   nil,
		},
		{
			name:   "empty script",
			script: "",
			want:   nil,
		},
		{
			name:   "multiple patterns",
			script: "apt-get install curl && yum install httpd",
			want: []PackageInstallInfo{
				{Manager: PackageManagerApt, Packages: []string{"curl"}},
				{Manager: PackageManagerYum, Packages: []string{"httpd"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPackageInstallsSimple(tt.script)

			if len(got) != len(tt.want) {
				t.Errorf("extractPackageInstallsSimple() returned %d installs, want %d", len(got), len(tt.want))
				for i, g := range got {
					t.Logf("  got[%d]: manager=%s, packages=%v", i, g.Manager, g.Packages)
				}
				return
			}

			for i, want := range tt.want {
				if got[i].Manager != want.Manager {
					t.Errorf("install[%d].Manager = %q, want %q", i, got[i].Manager, want.Manager)
				}
				if !slices.Equal(got[i].Packages, want.Packages) {
					t.Errorf("install[%d].Packages = %v, want %v", i, got[i].Packages, want.Packages)
				}
			}
		})
	}
}

func TestExtractSimplePackages(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple packages",
			input: " curl wget",
			want:  []string{"curl", "wget"},
		},
		{
			name:  "with flags",
			input: " -y curl wget",
			want:  []string{"curl", "wget"},
		},
		{
			name:  "stop at &&",
			input: " curl && echo done",
			want:  []string{"curl"},
		},
		{
			name:  "stop at ||",
			input: " curl || wget",
			want:  []string{"curl"},
		},
		{
			name:  "stop at semicolon",
			input: " curl ; wget",
			want:  []string{"curl"},
		},
		{
			name:  "stop at pipe",
			input: " curl | grep pattern",
			want:  []string{"curl"},
		},
		{
			name:  "skip env vars",
			input: " FOO=bar curl",
			want:  []string{"curl"},
		},
		{
			name:  "skip variables",
			input: " $package curl",
			want:  []string{"curl"},
		},
		{
			name:  "empty input",
			input: "",
			want:  []string{},
		},
		{
			name:  "only flags",
			input: " -y --yes",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSimplePackages(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("extractSimplePackages(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

