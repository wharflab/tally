package telemetry

import (
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/shell"
)

func TestCatalogAndStageSignals(t *testing.T) {
	t.Parallel()

	tools := OrderedTools()
	if len(tools) == 0 {
		t.Fatal("OrderedTools returned no tools")
	}
	if tools[0].ID != ToolBun {
		t.Fatalf("OrderedTools()[0].ID = %q, want %q", tools[0].ID, ToolBun)
	}

	tools[0].Name = "mutated"
	if fresh := OrderedTools(); fresh[0].Name == "mutated" {
		t.Fatal("OrderedTools returned a mutable backing slice")
	}

	next, ok := ToolByID(ToolNextJS)
	if !ok {
		t.Fatal("ToolByID(nextjs) = false, want true")
	}
	if next.EnvKey != "NEXT_TELEMETRY_DISABLED" {
		t.Fatalf("ToolByID(nextjs).EnvKey = %q, want NEXT_TELEMETRY_DISABLED", next.EnvKey)
	}
	if _, ok := ToolByID(ToolID("missing")); ok {
		t.Fatal("ToolByID(missing) = true, want false")
	}

	var nilSignals *StageSignals
	if !nilSignals.Empty() {
		t.Fatal("(*StageSignals)(nil).Empty() = false, want true")
	}

	signals := &StageSignals{}
	if !signals.Empty() {
		t.Fatal("empty StageSignals.Empty() = false, want true")
	}

	signals.addSignal(ToolBun, SignalKindCommand, "bun", anchorCandidate{line: 12})
	signals.addSignal(ToolBun, SignalKindInstall, "later bun", anchorCandidate{line: 18})
	signals.addSignal(ToolAzureCLI, SignalKindInstall, "azure", anchorCandidate{line: 4})

	if signals.Empty() {
		t.Fatal("StageSignals.Empty() = true after addSignal")
	}
	if got := signals.OrderedToolIDs(); !slices.Equal(got, []ToolID{ToolBun, ToolAzureCLI}) {
		t.Fatalf("OrderedToolIDs() = %v, want %v", got, []ToolID{ToolBun, ToolAzureCLI})
	}

	anchor, ok := signals.Anchor()
	if !ok {
		t.Fatal("Anchor() = false, want true")
	}
	if anchor.ToolID != ToolAzureCLI || anchor.Line != 4 {
		t.Fatalf("Anchor() = %+v, want tool %q at line 4", anchor, ToolAzureCLI)
	}
	if got := signals.byTool[ToolBun].Line; got != 12 {
		t.Fatalf("stored Bun signal line = %d, want 12", got)
	}
}

func TestEarlierCandidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    anchorCandidate
		b    anchorCandidate
		want anchorCandidate
	}{
		{
			name: "takes other candidate when first is invalid",
			b:    anchorCandidate{line: 8},
			want: anchorCandidate{line: 8},
		},
		{
			name: "keeps first candidate when second is invalid",
			a:    anchorCandidate{line: 3},
			want: anchorCandidate{line: 3},
		},
		{
			name: "takes earlier candidate",
			a:    anchorCandidate{line: 9},
			b:    anchorCandidate{line: 5},
			want: anchorCandidate{line: 5},
		},
		{
			name: "keeps first candidate on tie",
			a:    anchorCandidate{line: 7},
			b:    anchorCandidate{line: 7},
			want: anchorCandidate{line: 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := earlierCandidate(tt.a, tt.b); got != tt.want {
				t.Fatalf("earlierCandidate(%+v, %+v) = %+v, want %+v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDirectToolFromCommandName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  ToolID
	}{
		{name: "bun", input: "bun", want: ToolBun},
		{name: "bunx", input: "bunx", want: ToolBun},
		{name: "azure cli", input: "az", want: ToolAzureCLI},
		{name: "wrangler", input: "wrangler", want: ToolWrangler},
		{name: "hugging face short", input: "hf", want: ToolHuggingFace},
		{name: "hugging face long", input: "huggingface-cli", want: ToolHuggingFace},
		{name: "next", input: "next", want: ToolNextJS},
		{name: "nuxt", input: "nuxt", want: ToolNuxt},
		{name: "nuxi", input: "nuxi", want: ToolNuxt},
		{name: "gatsby", input: "gatsby", want: ToolGatsby},
		{name: "astro", input: "astro", want: ToolAstro},
		{name: "turbo", input: "turbo", want: ToolTurborepo},
		{name: "dotnet", input: "dotnet", want: ToolDotNetCLI},
		{name: "pwsh", input: "pwsh", want: ToolPowerShell},
		{name: "powershell", input: "powershell", want: ToolPowerShell},
		{name: "vcpkg", input: "vcpkg", want: ToolVcpkg},
		{name: "vcpkg exe", input: "vcpkg.exe", want: ToolVcpkg},
		{name: "bootstrap vcpkg", input: "bootstrap-vcpkg", want: ToolVcpkg},
		{name: "bootstrap bat", input: "bootstrap-vcpkg.bat", want: ToolVcpkg},
		{name: "bootstrap sh", input: "bootstrap-vcpkg.sh", want: ToolVcpkg},
		{name: "brew", input: "brew", want: ToolHomebrew},
		{name: "unknown", input: "curl", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := directToolFromCommandName(tt.input); got != tt.want {
				t.Fatalf("directToolFromCommandName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCanonicalPackageSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "scoped package version", input: "@cloudflare/wrangler@latest", want: "@cloudflare/wrangler"},
		{name: "extras and specifier", input: "huggingface_hub[cli]>=1.0", want: "huggingface-hub"},
		{name: "underscores normalized", input: "huggingface_hub", want: "huggingface-hub"},
		{name: "plain package version", input: "next@15.2.0", want: "next"},
		{name: "trim whitespace", input: " turbo ", want: "turbo"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalPackageSpec(tt.input); got != tt.want {
				t.Fatalf("canonicalPackageSpec(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInstalledToolFromPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  ToolID
	}{
		{name: "azure cli", input: "azure-cli", want: ToolAzureCLI},
		{name: "wrangler scoped", input: "@cloudflare/wrangler@latest", want: ToolWrangler},
		{name: "huggingface hub", input: "huggingface_hub", want: ToolHuggingFace},
		{name: "transformers", input: "transformers>=4.0", want: ToolHuggingFace},
		{name: "next", input: "next", want: ToolNextJS},
		{name: "nuxt", input: "nuxt", want: ToolNuxt},
		{name: "gatsby", input: "gatsby", want: ToolGatsby},
		{name: "astro", input: "astro", want: ToolAstro},
		{name: "turbo", input: "turbo", want: ToolTurborepo},
		{name: "powershell preview", input: "PowerShell-preview", want: ToolPowerShell},
		{name: "vcpkg", input: "vcpkg", want: ToolVcpkg},
		{name: "dotnet sdk", input: "dotnet-sdk-8.0", want: ToolDotNetCLI},
		{name: "unsupported", input: "bun", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := installedToolFromPackage(tt.input); got != tt.want {
				t.Fatalf("installedToolFromPackage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolFromExecPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantTool   ToolID
		wantReason string
		wantOK     bool
	}{
		{
			name:       "wrangler",
			input:      "wrangler",
			wantTool:   ToolWrangler,
			wantReason: "stage executes Wrangler via a package manager",
			wantOK:     true,
		},
		{
			name:       "next",
			input:      "next",
			wantTool:   ToolNextJS,
			wantReason: "stage executes Next.js via a package manager",
			wantOK:     true,
		},
		{
			name:       "nuxi",
			input:      "nuxi",
			wantTool:   ToolNuxt,
			wantReason: "stage executes Nuxt via a package manager",
			wantOK:     true,
		},
		{
			name:       "gatsby",
			input:      "gatsby",
			wantTool:   ToolGatsby,
			wantReason: "stage executes Gatsby via a package manager",
			wantOK:     true,
		},
		{
			name:       "astro",
			input:      "astro",
			wantTool:   ToolAstro,
			wantReason: "stage executes Astro via a package manager",
			wantOK:     true,
		},
		{
			name:       "turbo",
			input:      "turbo",
			wantTool:   ToolTurborepo,
			wantReason: "stage executes Turborepo via a package manager",
			wantOK:     true,
		},
		{name: "unsupported", input: "eslint", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTool, gotReason, gotOK := toolFromExecPackage(tt.input)
			if gotTool != tt.wantTool || gotReason != tt.wantReason || gotOK != tt.wantOK {
				t.Fatalf(
					"toolFromExecPackage(%q) = (%q, %q, %t), want (%q, %q, %t)",
					tt.input,
					gotTool,
					gotReason,
					gotOK,
					tt.wantTool,
					tt.wantReason,
					tt.wantOK,
				)
			}
		})
	}
}

func TestExecPackageFromCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want string
		ok   bool
	}{
		{
			name: "npx skips flags",
			cmd:  shell.CommandInfo{Name: "npx", Args: []string{"--yes", "wrangler", "deploy"}},
			want: "wrangler",
			ok:   true,
		},
		{
			name: "npm exec",
			cmd:  shell.CommandInfo{Name: "npm", Args: []string{"exec", "--", "next", "build"}},
			want: "next",
			ok:   true,
		},
		{
			name: "pnpm dlx",
			cmd:  shell.CommandInfo{Name: "pnpm", Args: []string{"dlx", "astro", "dev"}},
			want: "astro",
			ok:   true,
		},
		{
			name: "pnpm exec like subcommand",
			cmd:  shell.CommandInfo{Name: "pnpm", Subcommand: "nuxi"},
			want: "nuxi",
			ok:   true,
		},
		{
			name: "yarn dlx",
			cmd:  shell.CommandInfo{Name: "yarn", Args: []string{"dlx", "gatsby", "build"}},
			want: "gatsby",
			ok:   true,
		},
		{
			name: "bunx",
			cmd:  shell.CommandInfo{Name: "bunx", Args: []string{"turbo", "run", "build"}},
			want: "turbo",
			ok:   true,
		},
		{
			name: "bun x",
			cmd:  shell.CommandInfo{Name: "bun", Args: []string{"x", "wrangler", "deploy"}},
			want: "wrangler",
			ok:   true,
		},
		{name: "unsupported", cmd: shell.CommandInfo{Name: "npm", Args: []string{"install"}}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := execPackageFromCommand(tt.cmd)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("execPackageFromCommand(%+v) = (%q, %t), want (%q, %t)", tt.cmd, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestExecPackageFromArgv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want string
		ok   bool
	}{
		{name: "empty", ok: false},
		{
			name: "npx",
			argv: []string{"npx", "--yes", "wrangler", "deploy"},
			want: "wrangler",
			ok:   true,
		},
		{
			name: "npm exec",
			argv: []string{"npm", "exec", "--", "next", "build"},
			want: "next",
			ok:   true,
		},
		{
			name: "pnpm dlx",
			argv: []string{"pnpm", "dlx", "astro", "dev"},
			want: "astro",
			ok:   true,
		},
		{
			name: "pnpm exec like subcommand",
			argv: []string{"pnpm", "turbo", "run", "build"},
			want: "turbo",
			ok:   true,
		},
		{
			name: "yarn dlx",
			argv: []string{"yarn", "dlx", "gatsby", "build"},
			want: "gatsby",
			ok:   true,
		},
		{
			name: "bunx",
			argv: []string{"bunx", "nuxt", "build"},
			want: "nuxt",
			ok:   true,
		},
		{
			name: "bun x",
			argv: []string{"bun", "x", "@cloudflare/wrangler", "deploy"},
			want: "@cloudflare/wrangler",
			ok:   true,
		},
		{
			name: "unsupported",
			argv: []string{"npm", "install"},
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := execPackageFromArgv(tt.argv)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("execPackageFromArgv(%v) = (%q, %t), want (%q, %t)", tt.argv, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestTelemetryPredicates(t *testing.T) {
	t.Parallel()

	t.Run("toolFromExecLikeSubcommand", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			input string
			want  bool
		}{
			{name: "wrangler", input: "wrangler", want: true},
			{name: "scoped wrangler", input: "@cloudflare/wrangler", want: true},
			{name: "next", input: "next", want: true},
			{name: "nuxi", input: "nuxi", want: true},
			{name: "unsupported", input: "eslint", want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := toolFromExecLikeSubcommand(tt.input); got != tt.want {
					t.Fatalf("toolFromExecLikeSubcommand(%q) = %t, want %t", tt.input, got, tt.want)
				}
			})
		}
	})

	t.Run("isHuggingFacePackage", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			input string
			want  bool
		}{
			{name: "hub", input: "huggingface_hub", want: true},
			{name: "transformers", input: "transformers", want: true},
			{name: "diffusers", input: "diffusers", want: true},
			{name: "gradio", input: "gradio", want: true},
			{name: "node package", input: "@huggingface/hub", want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := isHuggingFacePackage(tt.input); got != tt.want {
					t.Fatalf("isHuggingFacePackage(%q) = %t, want %t", tt.input, got, tt.want)
				}
			})
		}
	})

	t.Run("isPythonModuleArgv", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name   string
			argv   []string
			module string
			want   bool
		}{
			{
				name:   "matches module",
				argv:   []string{"python", "-m", "huggingface_hub", "scan-cache"},
				module: "huggingface_hub",
				want:   true,
			},
			{
				name:   "different module",
				argv:   []string{"python3", "-m", "pip", "install"},
				module: "huggingface_hub",
				want:   false,
			},
			{
				name:   "not python",
				argv:   []string{"node", "-m", "huggingface_hub"},
				module: "huggingface_hub",
				want:   false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := isPythonModuleArgv(tt.argv, tt.module); got != tt.want {
					t.Fatalf("isPythonModuleArgv(%v, %q) = %t, want %t", tt.argv, tt.module, got, tt.want)
				}
			})
		}
	})

	t.Run("isNodeScriptCommand", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			cmd  shell.CommandInfo
			want bool
		}{
			{name: "npm run", cmd: shell.CommandInfo{Name: "npm", Subcommand: "run"}, want: true},
			{name: "npm start", cmd: shell.CommandInfo{Name: "npm", Subcommand: "start"}, want: true},
			{name: "npm install", cmd: shell.CommandInfo{Name: "npm", Subcommand: "install"}, want: false},
			{name: "pnpm preview", cmd: shell.CommandInfo{Name: "pnpm", Subcommand: "preview"}, want: true},
			{name: "pnpm add", cmd: shell.CommandInfo{Name: "pnpm", Subcommand: "add"}, want: false},
			{name: "yarn build", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "build"}, want: true},
			{name: "yarn install", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "install"}, want: false},
			{name: "yarn bin", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "bin"}, want: false},
			{name: "yarn constraints", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "constraints"}, want: false},
			{name: "yarn explain", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "explain"}, want: false},
			{name: "yarn info", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "info"}, want: false},
			{name: "yarn node", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "node"}, want: false},
			{name: "yarn pack", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "pack"}, want: false},
			{name: "yarn patch", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "patch"}, want: false},
			{name: "yarn patch-commit", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "patch-commit"}, want: false},
			{name: "yarn plugin", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "plugin"}, want: false},
			{name: "yarn search", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "search"}, want: false},
			{name: "yarn stage", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "stage"}, want: false},
			{name: "yarn tag", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "tag"}, want: false},
			{name: "yarn version", cmd: shell.CommandInfo{Name: "yarn", Subcommand: "version"}, want: false},
			{name: "bun run", cmd: shell.CommandInfo{Name: "bun", Subcommand: "run"}, want: true},
			{name: "bun install", cmd: shell.CommandInfo{Name: "bun", Subcommand: "install"}, want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := isNodeScriptCommand(tt.cmd); got != tt.want {
					t.Fatalf("isNodeScriptCommand(%+v) = %t, want %t", tt.cmd, got, tt.want)
				}
			})
		}
	})

	t.Run("containsBerryYarnSpecifier", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			args []string
			want bool
		}{
			{name: "berry alias", args: []string{"prepare", "yarn@berry"}, want: true},
			{name: "v4 version", args: []string{"use", "yarn@4.2.2"}, want: true},
			{name: "classic version", args: []string{"use", "yarn@1.22.0"}, want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := containsBerryYarnSpecifier(tt.args); got != tt.want {
					t.Fatalf("containsBerryYarnSpecifier(%v) = %t, want %t", tt.args, got, tt.want)
				}
			})
		}
	})

	t.Run("isPythonManifestFile", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			input string
			want  bool
		}{
			{name: "requirements", input: "requirements.txt", want: true},
			{name: "requirements dev", input: "requirements-dev.txt", want: true},
			{name: "pyproject", input: "pyproject.toml", want: true},
			{name: "uv lock", input: "uv.lock", want: true},
			{name: "package json", input: "package.json", want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := isPythonManifestFile(tt.input); got != tt.want {
					t.Fatalf("isPythonManifestFile(%q) = %t, want %t", tt.input, got, tt.want)
				}
			})
		}
	})

	t.Run("hasAnyArgFold", func(t *testing.T) {
		t.Parallel()
		if !hasAnyArgFold([]string{"prepare", "ENABLE"}, "enable") {
			t.Fatal("hasAnyArgFold did not match case-insensitive arg")
		}
		if hasAnyArgFold([]string{"prepare", "disable"}, "enable") {
			t.Fatal("hasAnyArgFold matched unexpected arg")
		}
	})
}

func TestStageScannerScanCommandInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cmd          shell.CommandInfo
		wantTools    []ToolID
		wantPython   bool
		wantNode     bool
		wantYarn     bool
		wantBerry    bool
		wantCorepack bool
	}{
		{
			name:       "python module command",
			cmd:        shell.CommandInfo{Name: "python", Args: []string{"-m", "huggingface_hub", "scan-cache"}},
			wantTools:  []ToolID{ToolHuggingFace},
			wantPython: true,
		},
		{
			name:      "npx wrangler",
			cmd:       shell.CommandInfo{Name: "npx", Args: []string{"wrangler", "deploy"}},
			wantTools: []ToolID{ToolWrangler},
		},
		{
			name:      "bunx turbo",
			cmd:       shell.CommandInfo{Name: "bunx", Args: []string{"turbo", "run", "build"}},
			wantTools: []ToolID{ToolBun, ToolTurborepo},
		},
		{
			name:         "npm run build",
			cmd:          shell.CommandInfo{Name: "npm", Subcommand: "run", Args: []string{"run", "build"}},
			wantNode:     true,
			wantTools:    nil,
			wantPython:   false,
			wantYarn:     false,
			wantBerry:    false,
			wantCorepack: false,
		},
		{
			name:         "pnpm build",
			cmd:          shell.CommandInfo{Name: "pnpm", Subcommand: "build"},
			wantNode:     true,
			wantTools:    nil,
			wantPython:   false,
			wantYarn:     false,
			wantBerry:    false,
			wantCorepack: false,
		},
		{
			name:       "yarn berry command",
			cmd:        shell.CommandInfo{Name: "yarn", Subcommand: "set", Args: []string{"set", "version", "berry"}},
			wantTools:  []ToolID{ToolYarnBerry},
			wantYarn:   true,
			wantBerry:  true,
			wantPython: false,
		},
		{
			name:         "corepack enable",
			cmd:          shell.CommandInfo{Name: "corepack", Args: []string{"prepare", "ENABLE"}},
			wantCorepack: true,
		},
		{name: "azure cli", cmd: shell.CommandInfo{Name: "az"}, wantTools: []ToolID{ToolAzureCLI}},
		{name: "dotnet", cmd: shell.CommandInfo{Name: "dotnet"}, wantTools: []ToolID{ToolDotNetCLI}},
		{name: "powershell", cmd: shell.CommandInfo{Name: "pwsh"}, wantTools: []ToolID{ToolPowerShell}},
		{name: "vcpkg", cmd: shell.CommandInfo{Name: "vcpkg"}, wantTools: []ToolID{ToolVcpkg}},
		{name: "homebrew", cmd: shell.CommandInfo{Name: "brew"}, wantTools: []ToolID{ToolHomebrew}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scanner := stageScanner{}
			scanner.scanCommandInfo(tt.cmd, anchorCandidate{line: 42})

			if got := scanner.result.OrderedToolIDs(); !slices.Equal(got, tt.wantTools) {
				t.Fatalf("scanCommandInfo tools = %v, want %v", got, tt.wantTools)
			}
			if got := scanner.pythonActivity.valid(); got != tt.wantPython {
				t.Fatalf("pythonActivity.valid() = %t, want %t", got, tt.wantPython)
			}
			if got := scanner.nodeScriptActivity.valid(); got != tt.wantNode {
				t.Fatalf("nodeScriptActivity.valid() = %t, want %t", got, tt.wantNode)
			}
			if got := scanner.yarnActivity.valid(); got != tt.wantYarn {
				t.Fatalf("yarnActivity.valid() = %t, want %t", got, tt.wantYarn)
			}
			if got := scanner.berryEvidence.valid(); got != tt.wantBerry {
				t.Fatalf("berryEvidence.valid() = %t, want %t", got, tt.wantBerry)
			}
			if got := scanner.corepackEnable.valid(); got != tt.wantCorepack {
				t.Fatalf("corepackEnable.valid() = %t, want %t", got, tt.wantCorepack)
			}
		})
	}
}

func TestStageScannerScanInstallCommand(t *testing.T) {
	t.Parallel()

	scanner := stageScanner{}
	scanner.scanInstallCommand(
		shell.InstallCommand{
			Manager: "apt-get",
			Packages: []shell.PackageArg{
				{Normalized: "azure-cli"},
				{Normalized: "powershell"},
				{Normalized: "dotnet-sdk-8.0"},
			},
		},
		anchorCandidate{line: 10},
	)
	scanner.scanInstallCommand(
		shell.InstallCommand{
			Manager: "npm",
			Packages: []shell.PackageArg{
				{Normalized: "@cloudflare/wrangler@latest"},
				{Normalized: "next@15"},
				{Normalized: "nuxt"},
				{Normalized: "gatsby"},
				{Normalized: "astro"},
				{Normalized: "turbo"},
			},
		},
		anchorCandidate{line: 20},
	)
	scanner.scanInstallCommand(
		shell.InstallCommand{
			Manager: "pip",
			Packages: []shell.PackageArg{
				{Normalized: "transformers>=4.0"},
			},
		},
		anchorCandidate{line: 30},
	)
	scanner.scanInstallCommand(
		shell.InstallCommand{
			Manager: "yarn",
			Packages: []shell.PackageArg{
				{Normalized: "next"},
			},
		},
		anchorCandidate{line: 40},
	)
	scanner.scanInstallCommand(
		shell.InstallCommand{
			Manager: "choco",
			Packages: []shell.PackageArg{
				{Normalized: "vcpkg"},
			},
		},
		anchorCandidate{line: 50},
	)

	want := []ToolID{
		ToolAzureCLI,
		ToolWrangler,
		ToolHuggingFace,
		ToolNextJS,
		ToolNuxt,
		ToolGatsby,
		ToolAstro,
		ToolTurborepo,
		ToolDotNetCLI,
		ToolPowerShell,
		ToolVcpkg,
	}
	if got := scanner.result.OrderedToolIDs(); !slices.Equal(got, want) {
		t.Fatalf("scanInstallCommand tools = %v, want %v", got, want)
	}
	if !scanner.pythonActivity.valid() || scanner.pythonActivity.line != 30 {
		t.Fatalf("pythonActivity = %+v, want line 30", scanner.pythonActivity)
	}
	if !scanner.yarnActivity.valid() || scanner.yarnActivity.line != 40 {
		t.Fatalf("yarnActivity = %+v, want line 40", scanner.yarnActivity)
	}
}

func TestStageScannerFinalizeManifestSignals(t *testing.T) {
	t.Parallel()

	scanner := stageScanner{
		pythonActivity:     anchorCandidate{line: 30},
		hfManifest:         anchorCandidate{line: 10},
		berryEvidence:      anchorCandidate{line: 12},
		corepackEnable:     anchorCandidate{line: 16},
		nodeScriptActivity: anchorCandidate{line: 40},
		nextManifest:       anchorCandidate{line: 18},
		nuxtManifest:       anchorCandidate{line: 19},
		gatsbyManifest:     anchorCandidate{line: 20},
		astroManifest:      anchorCandidate{line: 21},
		turboManifest:      anchorCandidate{line: 22},
	}

	scanner.finalizeManifestSignals()

	want := []ToolID{
		ToolHuggingFace,
		ToolYarnBerry,
		ToolNextJS,
		ToolNuxt,
		ToolGatsby,
		ToolAstro,
		ToolTurborepo,
	}
	if got := scanner.result.OrderedToolIDs(); !slices.Equal(got, want) {
		t.Fatalf("finalizeManifestSignals tools = %v, want %v", got, want)
	}

	anchor, ok := scanner.result.Anchor()
	if !ok {
		t.Fatal("Anchor() = false, want true")
	}
	if anchor.ToolID != ToolHuggingFace || anchor.Line != 10 {
		t.Fatalf("Anchor() = %+v, want hugging-face at line 10", anchor)
	}
}

func TestScanPackageJSON(t *testing.T) {
	t.Parallel()

	t.Run("extracts package manager and framework dependencies", func(t *testing.T) {
		t.Parallel()

		scanner := stageScanner{}
		scanner.scanPackageJSON(`{
			"packageManager": "yarn@4.2.2",
			"dependencies": {"next": "15.0.0", "nuxt": "3.0.0"},
			"devDependencies": {"gatsby": "5.0.0"},
			"optionalDependencies": {"astro": "4.0.0"},
			"peerDependencies": {"turbo": "2.0.0"}
		}`, anchorCandidate{line: 7})

		if !scanner.berryEvidence.valid() || scanner.berryEvidence.line != 7 {
			t.Fatalf("berryEvidence = %+v, want line 7", scanner.berryEvidence)
		}
		if !scanner.nextManifest.valid() || !scanner.nuxtManifest.valid() ||
			!scanner.gatsbyManifest.valid() || !scanner.astroManifest.valid() || !scanner.turboManifest.valid() {
			t.Fatal("scanPackageJSON did not record all manifest candidates")
		}
	})

	t.Run("ignores invalid json", func(t *testing.T) {
		t.Parallel()

		scanner := stageScanner{}
		scanner.scanPackageJSON("{not-json", anchorCandidate{line: 9})
		if !scanner.result.Empty() || scanner.berryEvidence.valid() || scanner.nextManifest.valid() {
			t.Fatal("scanPackageJSON recorded signals for invalid JSON")
		}
	})
}
