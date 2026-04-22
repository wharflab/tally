package shell

import (
	"slices"
	"testing"
)

func TestFindInstallPackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		script   string
		variant  Variant
		wantCmds int
		wantPkgs [][]string // per-command package values
	}{
		{
			name:     "apt-get install",
			script:   "apt-get install -y curl wget git",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget", "git"}},
		},
		{
			name:     "apk add",
			script:   "apk add --no-cache curl git",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "git"}},
		},
		{
			name:     "npm install",
			script:   "npm install express lodash",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"express", "lodash"}},
		},
		{
			name:     "npm i shorthand",
			script:   "npm i express lodash",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"express", "lodash"}},
		},
		{
			name:     "pip install",
			script:   "pip install flask django",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask", "django"}},
		},
		{
			name:     "pip install with == versions",
			script:   "pip install flask==2.0 django==4.0",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask==2.0", "django==4.0"}},
		},
		{
			name:     "apt-get install with =version",
			script:   "apt-get install -y curl=7.88.1-10+deb12u5 wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl=7.88.1-10+deb12u5", "wget"}},
		},
		{
			name:     "pip install -r requirements.txt skipped",
			script:   "pip install -r requirements.txt",
			variant:  VariantBash,
			wantCmds: 0,
		},
		{
			name:     "pip install -e . skipped",
			script:   "pip install -e .",
			variant:  VariantBash,
			wantCmds: 0,
		},
		{
			name:     "pip install . skipped",
			script:   "pip install .",
			variant:  VariantBash,
			wantCmds: 0,
		},
		{
			name:     "pip install -c constraint still extracts packages",
			script:   "pip install -c constraints.txt flask django",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask", "django"}},
		},
		{
			name:     "yarn add",
			script:   "yarn add react react-dom",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"react", "react-dom"}},
		},
		{
			name:     "pnpm add",
			script:   "pnpm add vue axios",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"vue", "axios"}},
		},
		{
			name:     "bun add",
			script:   "bun add elysia hono",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"elysia", "hono"}},
		},
		{
			name:     "composer require",
			script:   "composer require laravel/framework monolog/monolog",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"laravel/framework", "monolog/monolog"}},
		},
		{
			name:     "zypper install",
			script:   "zypper install curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "zypper in shorthand",
			script:   "zypper in curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "dnf install",
			script:   "dnf install -y curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "microdnf install",
			script:   "microdnf install -y curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "choco install",
			script:   "choco install -y git nodejs python3",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"git", "nodejs", "python3"}},
		},
		{
			name:     "choco install with --source flag",
			script:   "choco install -y --source https://chocolatey.org/api/v2/ git nodejs",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"git", "nodejs"}},
		},
		{
			name:     "choco -version flag consumes next argument",
			script:   "choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"microsoft-build-tools"}},
		},
		{
			name:     "pnpm install with add as package name",
			script:   "pnpm install add-package foo",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"add-package", "foo"}},
		},
		{
			name:     "uv add",
			script:   "uv add flask django",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask", "django"}},
		},
		{
			name:     "uv pip install",
			script:   "uv pip install flask django",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask", "django"}},
		},
		{
			name:     "uv pip install -r skipped",
			script:   "uv pip install -r requirements.txt",
			variant:  VariantBash,
			wantCmds: 0,
		},
		{
			name:     "uv pip install with --index-url flag",
			script:   "uv pip install --index-url https://pypi.corp.com/simple flask django",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask", "django"}},
		},
		{
			name:     "uv add with --extra flag",
			script:   "uv add --extra test flask django",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask", "django"}},
		},
		{
			name:     "not an install command",
			script:   "apt-get update",
			variant:  VariantBash,
			wantCmds: 0,
		},
		{
			name:     "chained install commands",
			script:   "apt-get update && apt-get install -y curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "multiple install commands",
			script:   "apt-get install -y curl && pip install flask",
			variant:  VariantBash,
			wantCmds: 2,
			wantPkgs: [][]string{{"curl"}, {"flask"}},
		},
		{
			name:     "variable arguments detected",
			script:   "npm install foo $PKG ${OTHER}",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"foo", "$PKG", "${OTHER}"}},
		},
		{
			name:     "unknown command ignored",
			script:   "go install github.com/foo/bar@latest",
			variant:  VariantBash,
			wantCmds: 0,
		},
		{
			name:     "env wrapped install",
			script:   "env DEBIAN_FRONTEND=noninteractive apt-get install -y curl",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl"}},
		},
		{
			name:     "flag with value not treated as package",
			script:   "apt-get install -y -t stable curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "long flag with value not treated as package",
			script:   "npm install --prefix /app express lodash",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"express", "lodash"}},
		},
		{
			name:     "flag with = is self-contained",
			script:   "apt-get install -y --option=Dpkg::Options::=--force-confdef curl",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl"}},
		},
		{
			name:     "-o option consumes next argument",
			script:   "apt-get install -o Dpkg::Use-Pty=0 curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "--target-release consumes next argument",
			script:   "apt-get install --target-release bookworm curl wget",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"curl", "wget"}},
		},
		{
			name:     "pip --trusted-host consumes next argument",
			script:   "pip install --trusted-host pypi.org --trusted-host files.pythonhosted.org flask",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"flask"}},
		},
		{
			name:     "pip --upgrade with packages",
			script:   "pip install --upgrade pip setuptools wheel",
			variant:  VariantBash,
			wantCmds: 1,
			wantPkgs: [][]string{{"pip", "setuptools", "wheel"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			commands := FindInstallPackages(tt.script, tt.variant)

			if len(commands) != tt.wantCmds {
				t.Fatalf("got %d commands, want %d", len(commands), tt.wantCmds)
			}
			if len(tt.wantPkgs) != tt.wantCmds {
				t.Fatalf("invalid test case: wantPkgs rows (%d) must match wantCmds (%d)",
					len(tt.wantPkgs), tt.wantCmds)
			}

			for i, cmd := range commands {
				var got []string
				for _, p := range cmd.Packages {
					got = append(got, p.Value)
				}
				if !slices.Equal(got, tt.wantPkgs[i]) {
					t.Errorf("command[%d] packages = %v, want %v", i, got, tt.wantPkgs[i])
				}
			}
		})
	}
}

func TestFindInstallPackagesPositions(t *testing.T) {
	t.Parallel()

	// Verify position tracking for a simple case
	script := "apt-get install -y curl wget"
	commands := FindInstallPackages(script, VariantBash)

	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(commands))
	}

	cmd := commands[0]
	if len(cmd.Packages) != 2 {
		t.Fatalf("got %d packages, want 2", len(cmd.Packages))
	}

	// "curl" should be at line 0, col 19-23
	if cmd.Packages[0].Value != "curl" {
		t.Errorf("packages[0].Value = %q, want %q", cmd.Packages[0].Value, "curl")
	}
	if cmd.Packages[0].Line != 0 {
		t.Errorf("packages[0].Line = %d, want 0", cmd.Packages[0].Line)
	}
	if cmd.Packages[0].StartCol != 19 {
		t.Errorf("packages[0].StartCol = %d, want 19", cmd.Packages[0].StartCol)
	}
	if cmd.Packages[0].EndCol != 23 {
		t.Errorf("packages[0].EndCol = %d, want 23", cmd.Packages[0].EndCol)
	}

	// "wget" should be at line 0, col 24-28
	if cmd.Packages[1].Value != "wget" {
		t.Errorf("packages[1].Value = %q, want %q", cmd.Packages[1].Value, "wget")
	}
	if cmd.Packages[1].StartCol != 24 {
		t.Errorf("packages[1].StartCol = %d, want 24", cmd.Packages[1].StartCol)
	}
	if cmd.Packages[1].EndCol != 28 {
		t.Errorf("packages[1].EndCol = %d, want 28", cmd.Packages[1].EndCol)
	}
}

func TestFindInstallPackagesIsVar(t *testing.T) {
	t.Parallel()

	script := "npm install foo $PKG ${OTHER}"
	commands := FindInstallPackages(script, VariantBash)

	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(commands))
	}

	pkgs := commands[0].Packages
	if len(pkgs) != 3 {
		t.Fatalf("got %d packages, want 3", len(pkgs))
	}

	if pkgs[0].IsVar {
		t.Error("packages[0] (foo) should not be a variable")
	}
	if !pkgs[1].IsVar {
		t.Error("packages[1] ($PKG) should be a variable")
	}
	if !pkgs[2].IsVar {
		t.Error("packages[2] (${OTHER}) should be a variable")
	}
}

func TestFindInstallPackagesQuotedPreservesRaw(t *testing.T) {
	t.Parallel()

	// Quoted packages should preserve raw text (including quotes) in Value
	// and have unquoted text in Normalized for round-trip safe edits.
	script := `pip install "flask==2.0" 'django==4.0'`
	commands := FindInstallPackages(script, VariantBash)

	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(commands))
	}

	pkgs := commands[0].Packages
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2", len(pkgs))
	}

	// Value should be the raw token including quotes
	if pkgs[0].Value != `"flask==2.0"` {
		t.Errorf("packages[0].Value = %q, want %q", pkgs[0].Value, `"flask==2.0"`)
	}
	if pkgs[1].Value != `'django==4.0'` {
		t.Errorf("packages[1].Value = %q, want %q", pkgs[1].Value, `'django==4.0'`)
	}

	// Normalized should be unquoted
	if pkgs[0].Normalized != "flask==2.0" {
		t.Errorf("packages[0].Normalized = %q, want %q", pkgs[0].Normalized, "flask==2.0")
	}
	if pkgs[1].Normalized != "django==4.0" {
		t.Errorf("packages[1].Normalized = %q, want %q", pkgs[1].Normalized, "django==4.0")
	}
}

func TestFindInstallPackagesMultiLine(t *testing.T) {
	t.Parallel()

	script := "apt-get install -y \\\n    curl \\\n    wget"
	commands := FindInstallPackages(script, VariantBash)

	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(commands))
	}

	pkgs := commands[0].Packages
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2", len(pkgs))
	}

	// "curl" is on line 1 (0-based)
	if pkgs[0].Line != 1 {
		t.Errorf("packages[0].Line = %d, want 1", pkgs[0].Line)
	}
	// "wget" is on line 2
	if pkgs[1].Line != 2 {
		t.Errorf("packages[1].Line = %d, want 2", pkgs[1].Line)
	}
}

func TestStripPackageVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		// apt/apk/dnf-style "=" pin.
		{"apt version pin", "wget=1.21.3-1ubuntu1", "wget"},
		{"apk version pin", "curl=8.11.1-r2", "curl"},
		// pip's "==" pin.
		{"pip equality", "flask==2.0.3", "flask"},
		{"pip equality bare name", "numpy", "numpy"},
		// npm plain and versioned.
		{"npm plain", "express", "express"},
		{"npm versioned", "express@5.0.0", "express"},
		// Scoped npm packages: leading "@" is part of the name, must be
		// preserved. The version separator is the second "@".
		{"npm scoped bare", "@types/node", "@types/node"},
		{"npm scoped versioned", "@types/node@22.0.0", "@types/node"},
		{"npm scoped with tag", "@babel/core@next", "@babel/core"},
		// Debian arch qualifier.
		{"arch qualifier", "libfoo:amd64", "libfoo"},
		// No separator at all.
		{"plain name", "curl", "curl"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := StripPackageVersion(tt.in); got != tt.want {
				t.Errorf("StripPackageVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFindInstallPackages_PowerShellChocoInstall(t *testing.T) {
	t.Parallel()

	// PowerShell-authored Dockerfile: the install uses ";" as a statement
	// separator (illegal in POSIX) and the continuation character is a
	// backtick. FindInstallPackages should still recognize the choco install
	// and report package positions relative to the original script.
	script := "$ErrorActionPreference='Stop'; choco install -y curl wget ca-certificates"
	installs := FindInstallPackages(script, VariantPowerShell)
	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
	ic := installs[0]
	if ic.Manager != "choco" || ic.Subcommand != "install" {
		t.Fatalf("unexpected manager/subcommand: %+v", ic)
	}
	gotPkgs := make([]string, 0, len(ic.Packages))
	for _, pkg := range ic.Packages {
		gotPkgs = append(gotPkgs, pkg.Normalized)
		// Per-arg positions must be within the script line and point at the
		// package token, not at the "-y" flag or the statement separator.
		if pkg.Line != 0 {
			t.Errorf("package %q on unexpected line %d", pkg.Normalized, pkg.Line)
		}
		slice := script[pkg.StartCol:pkg.EndCol]
		if slice != pkg.Normalized {
			t.Errorf("package %q range [%d:%d] = %q", pkg.Normalized, pkg.StartCol, pkg.EndCol, slice)
		}
	}
	want := []string{"curl", "wget", "ca-certificates"}
	if !slices.Equal(gotPkgs, want) {
		t.Fatalf("packages = %v, want %v", gotPkgs, want)
	}
}

func TestFindInstallPackages_CmdChocoInstall(t *testing.T) {
	t.Parallel()

	// cmd.exe-authored install: no alien tokens in this simple case, but
	// FindInstallPackages still needs to go through the cmd tree-sitter path
	// so it doesn't misinterpret cmd-specific syntax elsewhere.
	script := "choco install -y curl wget"
	installs := FindInstallPackages(script, VariantCmd)
	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
	ic := installs[0]
	if ic.Manager != "choco" || len(ic.Packages) != 2 {
		t.Fatalf("unexpected install: %+v", ic)
	}
	gotPkgs := []string{ic.Packages[0].Normalized, ic.Packages[1].Normalized}
	want := []string{"curl", "wget"}
	if !slices.Equal(gotPkgs, want) {
		t.Fatalf("packages = %v, want %v", gotPkgs, want)
	}
}
