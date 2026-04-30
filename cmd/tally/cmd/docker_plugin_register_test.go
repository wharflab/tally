package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerPluginRegistrarClassifySource(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "Users", "me")
	npmRoot := filepath.Join(home, ".nvm", "versions", "node", "v24.0.0", "lib", "node_modules")
	bunRoot := filepath.Join(home, ".bun", "install", "global")
	bunBin := filepath.Join(home, ".bun", "bin")
	uvToolDir := filepath.Join(home, ".local", "share", "uv", "tools")
	virtualEnv := filepath.Join(home, "work", "project", ".custom-env")
	registrar := dockerPluginRegistrar{
		goos:       "linux",
		homeDir:    home,
		cwd:        filepath.Join(home, "work", "project"),
		tempDir:    filepath.Join(string(filepath.Separator), "tmp"),
		virtualEnv: virtualEnv,
		commandOut: func(name string, args ...string) (string, error) {
			switch name + " " + strings.Join(args, " ") {
			case "npm root -g":
				return npmRoot, nil
			case "bun pm bin -g":
				return bunBin, nil
			case "uv tool dir":
				return uvToolDir, nil
			default:
				return "", os.ErrNotExist
			}
		},
	}

	cases := []struct {
		name    string
		path    string
		want    string
		wantErr string
	}{
		{
			name: "homebrew",
			path: filepath.Join(string(filepath.Separator), "opt", "homebrew", "bin", "tally"),
			want: "Homebrew",
		},
		{
			name: "winget",
			path: filepath.Join(
				home,
				"AppData",
				"Local",
				"Microsoft",
				"WinGet",
				"Packages",
				"Wharflab.Tally_8wekyb3d8bbwe",
				"tally.exe",
			),
			want: "WinGet",
		},
		{
			name: "npm global",
			path: filepath.Join(npmRoot, "tally-cli", "node_modules", "@wharflab", "tally-darwin-arm64", "bin", "tally"),
			want: "global npm",
		},
		{
			name: "bun global",
			path: filepath.Join(
				bunRoot,
				"node_modules",
				"tally-cli",
				"node_modules",
				"@wharflab",
				"tally-darwin-arm64",
				"bin",
				"tally",
			),
			want: "global Bun",
		},
		{
			name: "bun global bin",
			path: filepath.Join(bunBin, "tally"),
			want: "global Bun",
		},
		{
			name: "npm local",
			path: filepath.Join(
				home,
				"work",
				"project",
				"node_modules",
				"tally-cli",
				"node_modules",
				"@wharflab",
				"tally-darwin-arm64",
				"bin",
				"tally",
			),
			wantErr: "project-local npm",
		},
		{
			name: "bun local",
			path: filepath.Join(
				home,
				"work",
				"project",
				"node_modules",
				"tally-cli",
				"node_modules",
				"@wharflab",
				"tally-darwin-arm64",
				"bin",
				"tally",
			),
			wantErr: "project-local npm/Bun",
		},
		{
			name: "uv tool",
			path: filepath.Join(
				uvToolDir,
				"tally-cli",
				"lib",
				"python3.14",
				"site-packages",
				"tally_cli",
				"bin",
				"tally-linux-x86_64",
				"tally",
			),
			want: "uv tool",
		},
		{
			name: "python package install",
			path: filepath.Join(
				home,
				"work",
				"project",
				"python-env",
				"lib",
				"python3.14",
				"site-packages",
				"tally_cli",
				"bin",
				"tally-linux-x86_64",
				"tally",
			),
			wantErr: "Python virtual environment",
		},
		{
			name:    "active virtualenv",
			path:    filepath.Join(virtualEnv, "bin", "tally"),
			wantErr: "active Python virtual environment",
		},
		{
			name: "venv directory name without virtualenv",
			path: filepath.Join(home, "tools", "venv", "bin", "tally"),
			want: "global binary",
		},
		{
			name:    "temporary go run",
			path:    filepath.Join(string(filepath.Separator), "tmp", "go-build123", "b001", "exe", "tally"),
			wantErr: "temporary executable",
		},
		{
			name:    "already plugin",
			path:    filepath.Join(home, ".docker", "cli-plugins", "docker-lint"),
			wantErr: "already running as docker-lint",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := registrar.classifySource(tc.path)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("classifySource(%q) error = %v, want containing %q", tc.path, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("classifySource(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("classifySource(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestDockerPluginRegistrarTargetPath(t *testing.T) {
	t.Parallel()

	registrar := dockerPluginRegistrar{
		goos:    "linux",
		homeDir: filepath.Join(string(filepath.Separator), "Users", "me"),
	}
	got, reason, err := registrar.targetPath(dockerCLIInfo{}, nil)
	if err != nil {
		t.Fatalf("targetPath: %v", err)
	}
	want := filepath.Join(string(filepath.Separator), "Users", "me", ".docker", "cli-plugins", "docker-lint")
	if got != want {
		t.Fatalf("targetPath = %q, want %q", got, want)
	}
	if reason != "Docker per-user CLI plugin directory" {
		t.Fatalf("target reason = %q", reason)
	}
}

func TestDockerPluginRegistrarTargetPathUsesDockerConfig(t *testing.T) {
	t.Parallel()

	registrar := dockerPluginRegistrar{
		goos:         windowsGOOS,
		homeDir:      filepath.Join(string(filepath.Separator), "Users", "me"),
		dockerConfig: filepath.Join(string(filepath.Separator), "tmp", "docker-config"),
	}
	got, _, err := registrar.targetPath(dockerCLIInfo{}, nil)
	if err != nil {
		t.Fatalf("targetPath: %v", err)
	}
	want := filepath.Join(string(filepath.Separator), "tmp", "docker-config", "cli-plugins", "docker-lint.exe")
	if got != want {
		t.Fatalf("targetPath = %q, want %q", got, want)
	}
}

func TestDockerPluginRegistrarTargetPathUsesExistingPluginDirectory(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(string(filepath.Separator), "Users", "me", ".docker", "cli-plugins")
	registrar := dockerPluginRegistrar{
		goos:    "linux",
		homeDir: filepath.Join(string(filepath.Separator), "Users", "me"),
	}
	got, reason, err := registrar.targetPath(dockerCLIInfo{
		Plugins: []dockerCLIPluginInfo{
			{Name: "buildx", Path: filepath.Join(pluginDir, "docker-buildx")},
			{Name: "compose", Path: filepath.Join(pluginDir, "docker-compose")},
		},
	}, nil)
	if err != nil {
		t.Fatalf("targetPath: %v", err)
	}
	want := filepath.Join(pluginDir, "docker-lint")
	if got != want {
		t.Fatalf("targetPath = %q, want %q", got, want)
	}
	if reason != "existing Docker CLI plugin directory" {
		t.Fatalf("target reason = %q", reason)
	}
}

func TestDockerPluginRegistrarPlanRejectsThirdPartyLintPlugin(t *testing.T) {
	t.Parallel()

	registrar := testDockerPluginRegistrar(t, "0.7.19")
	_, err := registrar.plan(dockerCLIInfo{
		Version: "29.4.1",
		Plugins: []dockerCLIPluginInfo{{
			Name:             "lint",
			Vendor:           "Example Corp.",
			ShortDescription: "Another Dockerfile linter",
			Version:          "1.2.3",
			Path:             filepath.Join(t.TempDir(), "docker-lint"),
		}},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "`lint` command is already registered for Example Corp.: Another Dockerfile linter") {
		t.Fatalf("Plan error = %v, want third-party lint rejection", err)
	}
}

func TestDockerPluginRegistrarPlanRejectsNewerTallyPlugin(t *testing.T) {
	t.Parallel()

	registrar := testDockerPluginRegistrar(t, "0.7.19")
	_, err := registrar.plan(dockerCLIInfo{
		Version: "29.4.1",
		Plugins: []dockerCLIPluginInfo{{
			Name:             "lint",
			Vendor:           tallyDockerPluginVendor,
			ShortDescription: "Lint Dockerfiles and Containerfiles",
			Version:          "0.8.0",
			Path:             filepath.Join(t.TempDir(), "docker-lint"),
		}},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to downgrade") {
		t.Fatalf("Plan error = %v, want downgrade rejection", err)
	}
}

func TestDockerPluginRegistrarPlanUpgradesExistingTallyPlugin(t *testing.T) {
	t.Parallel()

	registrar := testDockerPluginRegistrar(t, "0.7.19")
	target := filepath.Join(t.TempDir(), "docker-lint")
	plan, err := registrar.plan(dockerCLIInfo{
		Version: "29.4.1",
		Plugins: []dockerCLIPluginInfo{{
			Name:             "lint",
			Vendor:           tallyDockerPluginVendor,
			ShortDescription: "Lint Dockerfiles and Containerfiles",
			Version:          "0.5.6",
			Path:             target,
		}},
	}, false)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Action != registrationActionUpgrade {
		t.Fatalf("Action = %q, want %q", plan.Action, registrationActionUpgrade)
	}
	if !plan.AllowReplaceTarget {
		t.Fatal("AllowReplaceTarget = false, want true")
	}
	if plan.TargetPath != target {
		t.Fatalf("TargetPath = %q, want %q", plan.TargetPath, target)
	}
}

func TestDockerPluginRegistrarInspectDocker(t *testing.T) {
	t.Parallel()

	registrar := dockerPluginRegistrar{
		lookPath: func(name string) (string, error) {
			if name == "docker" {
				return "/usr/local/bin/docker", nil
			}
			return "", os.ErrNotExist
		},
		commandOut: func(name string, args ...string) (string, error) {
			if name+" "+strings.Join(args, " ") != "docker info --format json" {
				return "", os.ErrNotExist
			}
			return `{"ClientInfo":{"Version":"29.4.1","Plugins":[{"Name":"compose","Vendor":"Docker Inc.","Path":"/p/docker-compose"}]}}`, nil
		},
	}
	info, err := registrar.inspectDocker()
	if err != nil {
		t.Fatalf("inspectDocker: %v", err)
	}
	if info.Version != "29.4.1" {
		t.Fatalf("Version = %q, want 29.4.1", info.Version)
	}
	if len(info.Plugins) != 1 || info.Plugins[0].Name != "compose" {
		t.Fatalf("Plugins = %#v", info.Plugins)
	}
}

func TestDockerPluginRegistrarRejectsProjectLocalBinaryFromNestedCWD(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(project, "cmd"), 0o750); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/project\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	registrar := dockerPluginRegistrar{
		goos:    "linux",
		homeDir: tmp,
		cwd:     filepath.Join(project, "cmd"),
		tempDir: filepath.Join(tmp, "tmp"),
	}
	source := filepath.Join(project, "bin", "tally")

	_, err := registrar.classifySource(source)
	if err == nil || !strings.Contains(err.Error(), "inside the current project") {
		t.Fatalf("classifySource(%q) error = %v, want current project rejection", source, err)
	}
}

func TestDockerPluginRegistrarRegisterSymlink(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("symlink registration is Unix-specific")
	}
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "bin", "tally")
	if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("binary"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	target := filepath.Join(tmp, ".docker", "cli-plugins", "docker-lint")
	registrar := dockerPluginRegistrar{goos: "linux"}
	plan := dockerPluginRegistrationPlan{
		SourcePath: source,
		TargetPath: target,
		Mode:       installModeSymlink,
		SourceKind: "global binary",
	}

	if err := registrar.register(plan, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if got != source {
		t.Fatalf("symlink target = %q, want %q", got, source)
	}
}

func TestDockerPluginRegistrarRegisterCopy(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "bin", "tally.exe")
	if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("binary"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	target := filepath.Join(tmp, ".docker", "cli-plugins", "docker-lint.exe")
	registrar := dockerPluginRegistrar{goos: windowsGOOS}
	plan := dockerPluginRegistrationPlan{
		SourcePath: source,
		TargetPath: target,
		Mode:       installModeCopy,
		SourceKind: "WinGet",
	}

	if err := registrar.register(plan, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "binary" {
		t.Fatalf("copied content = %q, want binary", got)
	}
	if err := registrar.register(plan, false); err != nil {
		t.Fatalf("register second identical copy: %v", err)
	}
	if err := os.WriteFile(source, []byte("new binary"), 0o700); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	if err := registrar.register(plan, false); err == nil {
		t.Fatal("register with changed source succeeded without --force")
	}
	if err := registrar.register(plan, true); err != nil {
		t.Fatalf("register with force: %v", err)
	}
	got, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("read forced target: %v", err)
	}
	if string(got) != "new binary" {
		t.Fatalf("forced copied content = %q, want new binary", got)
	}
}

func testDockerPluginRegistrar(t *testing.T, currentVersion string) dockerPluginRegistrar {
	t.Helper()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "bin", "tally")
	if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("binary"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return dockerPluginRegistrar{
		goos:           "linux",
		homeDir:        tmp,
		cwd:            filepath.Join(tmp, "outside"),
		tempDir:        filepath.Join(tmp, "tmp"),
		args0:          source,
		currentVersion: currentVersion,
	}
}
