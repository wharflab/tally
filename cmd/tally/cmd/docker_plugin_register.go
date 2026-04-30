package cmd

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/wharflab/tally/internal/version"
)

const (
	registerDockerPluginCommandName = "register-docker-plugin"
	registrationActionDowngrade     = "Downgrading"
	registrationActionRefresh       = "Refreshing"
	registrationActionRegister      = "Registering"
	registrationActionReplace       = "Replacing"
	registrationActionUpgrade       = "Upgrading"
	installModeCopy                 = "copy"
	installModeSymlink              = "symlink"
	minimumDockerCLIVersion         = "20.10.0"
	tallyDockerPluginVendor         = "Wharflab"
	windowsGOOS                     = "windows"
)

type registerDockerPluginOptions struct {
	force  bool
	dryRun bool
}

type dockerPluginRegistrationPlan struct {
	SourcePath         string
	TargetPath         string
	Mode               string
	SourceKind         string
	CurrentVersion     string
	DockerVersion      string
	TargetReason       string
	Action             string
	ExistingPlugin     *dockerCLIPluginInfo
	AllowReplaceTarget bool
}

type dockerPluginRegistrar struct {
	goos           string
	homeDir        string
	cwd            string
	tempDir        string
	dockerConfig   string
	args0          string
	currentVersion string
	executable     func() (string, error)
	lookPath       func(string) (string, error)
	commandOut     func(string, ...string) (string, error)
}

type dockerInfoOutput struct {
	ClientInfo *dockerCLIInfo `json:"ClientInfo"`
}

type dockerCLIInfo struct {
	Version string                `json:"Version"`
	Plugins []dockerCLIPluginInfo `json:"Plugins"`
}

type dockerCLIPluginInfo struct {
	Vendor           string `json:"Vendor"`
	Version          string `json:"Version"`
	ShortDescription string `json:"ShortDescription"`
	Name             string `json:"Name"`
	Path             string `json:"Path"`
}

func registerDockerPluginCommand() *cobra.Command {
	opts := &registerDockerPluginOptions{}

	cmd := &cobra.Command{
		Use:   registerDockerPluginCommandName,
		Short: "Register tally as the docker lint CLI plugin",
		Long: `Register tally as the Docker CLI plugin used by docker lint.

The command registers docker-lint in Docker's per-user CLI plugin directory.
It does not download Docker, Docker Compose, Docker Buildx, or another tally
binary. It only runs from persistent global installs such as Homebrew, WinGet,
global npm installs, uv tool installs, or a direct global binary.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			registrar := newDockerPluginRegistrar()
			out := cmd.OutOrStdout()

			dockerInfo, err := registrar.inspectDocker()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Unable to register tally plugin - %v\n", err)
				return exitWith(ExitConfigError)
			}
			fmt.Fprintf(out, "Found Docker CLI version %s\n", dockerInfo.Version)

			plan, err := registrar.plan(dockerInfo, opts.force)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Unable to register tally plugin - %v\n", err)
				return exitWith(ExitConfigError)
			}

			printDockerPluginRegistrationPlan(out, plan, opts.dryRun)
			if opts.dryRun {
				return nil
			}

			if err := registrar.register(plan, opts.force); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Unable to register tally plugin - %v\n", err)
				return exitWith(ExitConfigError)
			}
			verified, err := registrar.verify(plan)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Unable to verify tally plugin registration - %v\n", err)
				return exitWith(ExitConfigError)
			}
			printDockerPluginRegistrationResult(out, plan, verified)
			return nil
		},
	}

	cmd.Flags().BoolVar(&opts.force, "force", false, "Replace an existing docker-lint file controlled by tally")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the registration plan without changing files")
	return cmd
}

func newDockerPluginRegistrar() dockerPluginRegistrar {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return dockerPluginRegistrar{
		goos:           runtime.GOOS,
		homeDir:        home,
		cwd:            cwd,
		tempDir:        os.TempDir(),
		dockerConfig:   os.Getenv("DOCKER_CONFIG"),
		args0:          os.Args[0],
		currentVersion: version.RawVersion(),
		executable:     os.Executable,
		lookPath:       exec.LookPath,
		commandOut:     commandOutputWithTimeout,
	}
}

func (r dockerPluginRegistrar) inspectDocker() (dockerCLIInfo, error) {
	if r.lookPath != nil {
		if _, err := r.lookPath("docker"); err != nil {
			return dockerCLIInfo{}, errors.New("docker CLI was not found on PATH")
		}
	}
	if r.commandOut == nil {
		return dockerCLIInfo{}, errors.New("docker CLI inspection is unavailable")
	}
	out, err := r.commandOut("docker", "info", "--format", "json")
	if err != nil {
		return dockerCLIInfo{}, fmt.Errorf("docker info --format json failed: %w", err)
	}

	var info dockerInfoOutput
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return dockerCLIInfo{}, fmt.Errorf("parse docker info JSON: %w", err)
	}
	if info.ClientInfo == nil {
		return dockerCLIInfo{}, errors.New("docker info did not include ClientInfo")
	}
	client := *info.ClientInfo
	if client.Version == "" {
		return dockerCLIInfo{}, errors.New("docker info did not include ClientInfo.Version")
	}
	if err := requireMinimumDockerVersion(client.Version); err != nil {
		return dockerCLIInfo{}, err
	}
	if client.Plugins == nil {
		client.Plugins = []dockerCLIPluginInfo{}
	}
	return client, nil
}

func requireMinimumDockerVersion(raw string) error {
	got, err := parseSemverish(raw)
	if err != nil {
		return fmt.Errorf("docker CLI version %q is not a semantic version: %w", raw, err)
	}
	want := semver.MustParse(minimumDockerCLIVersion)
	if got.Compare(want) < 0 {
		return fmt.Errorf("docker CLI version %s is older than the minimum supported version %s", raw, minimumDockerCLIVersion)
	}
	return nil
}

func (r dockerPluginRegistrar) plan(dockerInfo dockerCLIInfo, force bool) (dockerPluginRegistrationPlan, error) {
	source, err := r.currentExecutablePath()
	if err != nil {
		return dockerPluginRegistrationPlan{}, err
	}
	sourceKind, err := r.classifySource(source)
	if err != nil {
		return dockerPluginRegistrationPlan{}, err
	}

	existing := findDockerPlugin(dockerInfo.Plugins, dockerLintPluginName)
	target, targetReason, err := r.targetPath(dockerInfo, existing)
	if err != nil {
		return dockerPluginRegistrationPlan{}, err
	}

	mode := installModeSymlink
	if r.goos == windowsGOOS {
		mode = installModeCopy
	}
	plan := dockerPluginRegistrationPlan{
		SourcePath:     source,
		TargetPath:     target,
		Mode:           mode,
		SourceKind:     sourceKind,
		CurrentVersion: r.currentVersion,
		DockerVersion:  dockerInfo.Version,
		TargetReason:   targetReason,
		Action:         registrationActionRegister,
		ExistingPlugin: existing,
	}

	if existing != nil {
		if !isTallyDockerPlugin(*existing) {
			return dockerPluginRegistrationPlan{}, fmt.Errorf(
				"`lint` command is already registered for %s: %s",
				pluginVendorLabel(*existing),
				pluginDescriptionLabel(*existing),
			)
		}
		action, allowReplace, err := r.planExistingTallyPlugin(*existing, force)
		if err != nil {
			return dockerPluginRegistrationPlan{}, err
		}
		plan.Action = action
		plan.AllowReplaceTarget = allowReplace
	}

	if !force {
		if err := r.checkExistingTarget(plan); err != nil {
			return dockerPluginRegistrationPlan{}, err
		}
	}
	return plan, nil
}

func (r dockerPluginRegistrar) planExistingTallyPlugin(plugin dockerCLIPluginInfo, force bool) (string, bool, error) {
	cmp, ok := compareSemverish(r.currentVersion, plugin.Version)
	if !ok {
		if !force {
			return "", false, fmt.Errorf(
				"unable to compare existing tally lint plugin version %q with current version %q; use --force to replace it",
				plugin.Version,
				r.currentVersion,
			)
		}
		return registrationActionReplace, true, nil
	}

	switch {
	case cmp < 0:
		if !force {
			return "", false, fmt.Errorf(
				"registered tally lint plugin version %s is newer than current tally version %s; refusing to downgrade",
				plugin.Version,
				r.currentVersion,
			)
		}
		return registrationActionDowngrade, true, nil
	case cmp > 0:
		return registrationActionUpgrade, true, nil
	default:
		return registrationActionRefresh, true, nil
	}
}

func (r dockerPluginRegistrar) register(plan dockerPluginRegistrationPlan, force bool) error {
	if err := os.MkdirAll(filepath.Dir(plan.TargetPath), 0o750); err != nil {
		return fmt.Errorf("create Docker CLI plugin directory: %w", err)
	}

	if _, err := os.Lstat(plan.TargetPath); err == nil {
		if sameDockerPluginRegistration(plan) {
			return nil
		}
		if !force && !plan.AllowReplaceTarget {
			return fmt.Errorf("%s already exists; use --force to replace it", plan.TargetPath)
		}
		if err := os.Remove(plan.TargetPath); err != nil {
			return fmt.Errorf("remove existing docker-lint plugin: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing docker-lint plugin: %w", err)
	}

	if plan.Mode == installModeCopy {
		return copyExecutable(plan.SourcePath, plan.TargetPath)
	}
	if err := os.Symlink(plan.SourcePath, plan.TargetPath); err != nil {
		return fmt.Errorf("create docker-lint symlink: %w", err)
	}
	return nil
}

func (r dockerPluginRegistrar) verify(plan dockerPluginRegistrationPlan) (*dockerCLIPluginInfo, error) {
	dockerInfo, err := r.inspectDocker()
	if err != nil {
		return nil, err
	}
	plugin := findDockerPlugin(dockerInfo.Plugins, dockerLintPluginName)
	if plugin == nil {
		return nil, errors.New("docker CLI did not report a `lint` plugin after registration")
	}
	if !isTallyDockerPlugin(*plugin) {
		return nil, fmt.Errorf(
			"docker CLI reports `lint` for %s: %s",
			pluginVendorLabel(*plugin),
			pluginDescriptionLabel(*plugin),
		)
	}
	if plugin.Path != "" && !samePath(plugin.Path, plan.TargetPath) {
		return nil, fmt.Errorf("docker CLI reports `lint` at %s, not %s", plugin.Path, plan.TargetPath)
	}
	if !versionsEquivalent(plan.CurrentVersion, plugin.Version) {
		return nil, fmt.Errorf("docker CLI reports tally lint version %s, expected %s", plugin.Version, plan.CurrentVersion)
	}
	return plugin, nil
}

func (r dockerPluginRegistrar) currentExecutablePath() (string, error) {
	candidates := make([]string, 0, 2)
	if r.args0 != "" {
		if resolved, err := r.resolveArg0(r.args0); err == nil {
			candidates = append(candidates, resolved)
		}
	}
	if r.executable != nil {
		if exe, err := r.executable(); err == nil && exe != "" {
			if abs, err := filepath.Abs(exe); err == nil {
				candidates = append(candidates, filepath.Clean(abs))
			}
		}
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if r.goos != windowsGOOS && info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("current tally executable is not executable: %s", candidate)
		}
		return r.stableExecutablePath(candidate), nil
	}
	return "", errors.New("failed to resolve the current tally executable")
}

func (r dockerPluginRegistrar) resolveArg0(arg0 string) (string, error) {
	if filepath.IsAbs(arg0) || strings.ContainsAny(arg0, `/\`) {
		return filepath.Abs(arg0)
	}
	if r.lookPath == nil {
		return "", errors.New("PATH lookup unavailable")
	}
	return r.lookPath(arg0)
}

func (r dockerPluginRegistrar) stableExecutablePath(path string) string {
	clean := filepath.Clean(path)
	if stable, ok := stableHomebrewExecutable(clean); ok {
		return stable
	}
	return clean
}

func stableHomebrewExecutable(path string) (string, bool) {
	slash := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(slash, "/")
	for idx := 0; idx+4 < len(parts); idx++ {
		if parts[idx] != "Cellar" || parts[idx+1] != "tally" || parts[idx+3] != "bin" {
			continue
		}
		prefix := strings.Join(parts[:idx], "/")
		if prefix == "" {
			prefix = "/"
		}
		stable := filepath.FromSlash(strings.TrimRight(prefix, "/") + "/bin/tally")
		if _, err := os.Stat(stable); err == nil {
			return stable, true
		}
	}
	return "", false
}

func (r dockerPluginRegistrar) classifySource(source string) (string, error) {
	if IsDockerLintPluginExecutable(source) {
		return "", errors.New("already running as docker-lint; run this command through tally")
	}

	source = filepath.Clean(source)
	if r.pathWithin(source, r.tempDir) {
		return "", fmt.Errorf("%s is a temporary executable, not a persistent global install", source)
	}
	if r.isProjectLocalExecutable(source) {
		return "", fmt.Errorf("%s is inside the current project; install tally globally before registering the Docker plugin", source)
	}

	switch {
	case looksLikeHomebrewPath(source):
		return "Homebrew", nil
	case looksLikeWingetPath(source):
		return "WinGet", nil
	case pathHasSegment(source, "node_modules"):
		if r.isNPMGlobalPath(source) {
			return "global npm", nil
		}
		return "", fmt.Errorf("%s looks like a project-local npm install; run npm install -g tally-cli first", source)
	case looksLikePythonPackagePath(source):
		if r.isUVToolPath(source) {
			return "uv tool", nil
		}
		return "", fmt.Errorf(
			"%s looks like a Python virtual environment or package install; use uv tool install tally-cli for automatic plugin registration",
			source,
		)
	case looksLikeGlobalBinaryPath(source):
		return "global binary", nil
	default:
		return "", fmt.Errorf("%s is not a recognized persistent global tally install", source)
	}
}

func (r dockerPluginRegistrar) targetPath(
	dockerInfo dockerCLIInfo,
	existing *dockerCLIPluginInfo,
) (path, reason string, err error) {
	if existing != nil && existing.Path != "" {
		return filepath.Clean(existing.Path), "existing docker lint plugin path", nil
	}

	name := dockerPluginExecutableName(r.goos)
	if dir, ok := commonDockerPluginDir(dockerInfo.Plugins); ok {
		return filepath.Join(dir, name), "existing Docker CLI plugin directory", nil
	}

	configDir := r.dockerConfig
	if configDir == "" {
		if r.homeDir == "" {
			return "", "", errors.New("cannot resolve home directory; set DOCKER_CONFIG")
		}
		configDir = filepath.Join(r.homeDir, ".docker")
	}
	return filepath.Join(configDir, "cli-plugins", name), "Docker per-user CLI plugin directory", nil
}

func dockerPluginExecutableName(goos string) string {
	name := "docker-lint"
	if goos == windowsGOOS {
		name += ".exe"
	}
	return name
}

func commonDockerPluginDir(plugins []dockerCLIPluginInfo) (string, bool) {
	var dir string
	for _, plugin := range plugins {
		if plugin.Path == "" {
			continue
		}
		pluginDir := filepath.Clean(filepath.Dir(plugin.Path))
		if dir == "" {
			dir = pluginDir
			continue
		}
		if !samePath(dir, pluginDir) {
			return "", false
		}
	}
	return dir, dir != ""
}

func (r dockerPluginRegistrar) checkExistingTarget(plan dockerPluginRegistrationPlan) error {
	if _, err := os.Lstat(plan.TargetPath); err == nil {
		if sameDockerPluginRegistration(plan) || plan.AllowReplaceTarget {
			return nil
		}
		return fmt.Errorf("%s already exists; use --force to replace it", plan.TargetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing docker-lint plugin: %w", err)
	}
	return nil
}

func (r dockerPluginRegistrar) isProjectLocalExecutable(path string) bool {
	root := r.projectRoot()
	if root == "" || !r.pathWithin(path, root) {
		return false
	}
	return true
}

func (r dockerPluginRegistrar) projectRoot() string {
	if r.cwd == "" {
		return ""
	}

	dir, err := filepath.Abs(r.cwd)
	if err != nil {
		return ""
	}
	for {
		for _, marker := range []string{".git", "go.mod", "package.json", "pyproject.toml", "node_modules", ".venv", "venv"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func (r dockerPluginRegistrar) isNPMGlobalPath(path string) bool {
	for _, root := range r.npmGlobalRoots() {
		if r.pathWithin(path, root) {
			return true
		}
	}
	return false
}

func (r dockerPluginRegistrar) npmGlobalRoots() []string {
	var roots []string
	if v := strings.TrimSpace(os.Getenv("TALLY_NPM_GLOBAL_ROOT")); v != "" {
		roots = appendPathList(roots, v)
	}
	if r.commandOut != nil {
		if out, err := r.commandOut("npm", "root", "-g"); err == nil {
			roots = appendPathList(roots, out)
		}
	}
	return cleanPathList(roots)
}

func (r dockerPluginRegistrar) isUVToolPath(path string) bool {
	for _, root := range r.uvToolDirs() {
		if r.pathWithin(path, root) {
			return true
		}
	}
	return false
}

func (r dockerPluginRegistrar) uvToolDirs() []string {
	var roots []string
	if v := strings.TrimSpace(os.Getenv("UV_TOOL_DIR")); v != "" {
		roots = appendPathList(roots, v)
	}
	if v := strings.TrimSpace(os.Getenv("TALLY_UV_TOOL_DIR")); v != "" {
		roots = appendPathList(roots, v)
	}
	if r.commandOut != nil {
		if out, err := r.commandOut("uv", "tool", "dir"); err == nil {
			roots = appendPathList(roots, out)
		}
	}
	if r.homeDir != "" {
		roots = append(roots,
			filepath.Join(r.homeDir, ".local", "share", "uv", "tools"),
			filepath.Join(r.homeDir, "Library", "Application Support", "uv", "tools"),
			filepath.Join(r.homeDir, "AppData", "Roaming", "uv", "tools"),
		)
	}
	return cleanPathList(roots)
}

func (r dockerPluginRegistrar) pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func printDockerPluginRegistrationPlan(w io.Writer, plan dockerPluginRegistrationPlan, dryRun bool) {
	action := plan.Action
	if dryRun {
		action = dryRunRegistrationAction(action)
	}

	switch {
	case plan.ExistingPlugin != nil && isTallyDockerPlugin(*plan.ExistingPlugin) && plan.Action == registrationActionRefresh:
		fmt.Fprintf(w, "%s tally lint plugin version %s\n", action, plan.CurrentVersion)
	case plan.ExistingPlugin != nil && isTallyDockerPlugin(*plan.ExistingPlugin):
		fmt.Fprintf(
			w,
			"%s tally lint plugin from version %s to %s\n",
			action,
			plan.ExistingPlugin.Version,
			plan.CurrentVersion,
		)
	default:
		fmt.Fprintf(w, "%s tally lint plugin version %s\n", action, plan.CurrentVersion)
	}
	fmt.Fprintf(w, "  source: %s (%s install)\n", plan.SourcePath, plan.SourceKind)
	fmt.Fprintf(w, "  target: %s (%s)\n", plan.TargetPath, plan.TargetReason)
	fmt.Fprintf(w, "  mode:   %s\n", plan.Mode)
}

func dryRunRegistrationAction(action string) string {
	switch action {
	case registrationActionDowngrade:
		return "Would downgrade"
	case registrationActionRefresh:
		return "Would refresh"
	case registrationActionReplace:
		return "Would replace"
	case registrationActionUpgrade:
		return "Would upgrade"
	default:
		return "Would register"
	}
}

func printDockerPluginRegistrationResult(w io.Writer, plan dockerPluginRegistrationPlan, plugin *dockerCLIPluginInfo) {
	registeredPath := plan.TargetPath
	if plugin != nil && plugin.Path != "" {
		registeredPath = plugin.Path
	}
	fmt.Fprintf(w, "Registered and verified tally lint plugin version %s\n", plan.CurrentVersion)
	fmt.Fprintf(w, "  path: %s\n", registeredPath)
	fmt.Fprintf(w, "Run: docker lint --help\n")
}

func findDockerPlugin(plugins []dockerCLIPluginInfo, name string) *dockerCLIPluginInfo {
	for idx := range plugins {
		if strings.EqualFold(plugins[idx].Name, name) {
			plugin := plugins[idx]
			return &plugin
		}
	}
	return nil
}

func isTallyDockerPlugin(plugin dockerCLIPluginInfo) bool {
	return strings.EqualFold(plugin.Vendor, tallyDockerPluginVendor)
}

func pluginVendorLabel(plugin dockerCLIPluginInfo) string {
	if plugin.Vendor != "" {
		return plugin.Vendor
	}
	return "unknown vendor"
}

func pluginDescriptionLabel(plugin dockerCLIPluginInfo) string {
	if plugin.ShortDescription != "" {
		return plugin.ShortDescription
	}
	return "no description"
}

func sameDockerPluginRegistration(plan dockerPluginRegistrationPlan) bool {
	if plan.Mode == installModeCopy {
		return sameExecutableContent(plan.TargetPath, plan.SourcePath)
	}
	return sameDockerPluginTarget(plan.TargetPath, plan.SourcePath)
}

func sameDockerPluginTarget(target, source string) bool {
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		targetReal = target
	}
	sourceReal, err := filepath.EvalSymlinks(source)
	if err != nil {
		sourceReal = source
	}
	return samePath(targetReal, sourceReal)
}

func samePath(left, right string) bool {
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	leftClean := filepath.Clean(leftAbs)
	rightClean := filepath.Clean(rightAbs)
	if runtime.GOOS == windowsGOOS {
		return strings.EqualFold(leftClean, rightClean)
	}
	return leftClean == rightClean
}

func sameExecutableContent(target, source string) bool {
	targetInfo, err := os.Stat(target)
	if err != nil || targetInfo.IsDir() {
		return false
	}
	sourceInfo, err := os.Stat(source)
	if err != nil || sourceInfo.IsDir() || targetInfo.Size() != sourceInfo.Size() {
		return false
	}
	targetBytes, err := os.ReadFile(target)
	if err != nil {
		return false
	}
	sourceBytes, err := os.ReadFile(source)
	if err != nil {
		return false
	}
	return bytes.Equal(targetBytes, sourceBytes)
}

func copyExecutable(source, target string) error {
	input, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read current tally executable: %w", err)
	}
	if err := os.WriteFile(target, input, 0o700); err != nil { //nolint:gosec // Docker CLI plugins must be executable.
		return fmt.Errorf("write docker-lint executable: %w", err)
	}
	return nil
}

func commandOutputWithTimeout(name string, args ...string) (string, error) {
	switch name + " " + strings.Join(args, " ") {
	case "docker info --format json":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return commandTextOutput(exec.CommandContext(ctx, "docker", "info", "--format", "json"))
	case "npm root -g":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return commandTextOutput(exec.CommandContext(ctx, "npm", "root", "-g"))
	case "uv tool dir":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return commandTextOutput(exec.CommandContext(ctx, "uv", "tool", "dir"))
	default:
		return "", fmt.Errorf("unsupported command probe: %s %s", name, strings.Join(args, " "))
	}
}

func commandTextOutput(cmd *exec.Cmd) (string, error) {
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("%w: %s", err, text)
		}
		return "", err
	}
	return text, nil
}

func compareSemverish(current, existing string) (int, bool) {
	currentVersion, err := parseSemverish(current)
	if err != nil {
		return 0, false
	}
	existingVersion, err := parseSemverish(existing)
	if err != nil {
		return 0, false
	}
	return currentVersion.Compare(existingVersion), true
}

func versionsEquivalent(left, right string) bool {
	if cmp, ok := compareSemverish(left, right); ok {
		return cmp == 0
	}
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

func parseSemverish(raw string) (*semver.Version, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty version")
	}
	if fields := strings.Fields(raw); len(fields) > 0 {
		raw = fields[0]
	}
	return semver.NewVersion(raw)
}

func looksLikeHomebrewPath(path string) bool {
	slash := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return strings.Contains(slash, "/cellar/tally/") ||
		strings.Contains(slash, "/homebrew/bin/tally") ||
		strings.Contains(slash, "/linuxbrew/bin/tally")
}

func looksLikeWingetPath(path string) bool {
	slash := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return strings.Contains(slash, "/microsoft/winget/packages/wharflab.tally") ||
		strings.Contains(slash, "/microsoft/winget/links/tally")
}

func looksLikePythonPackagePath(path string) bool {
	return pathHasSegment(path, "site-packages") ||
		pathHasSegment(path, "dist-packages") ||
		pathHasSegment(path, ".venv") ||
		pathHasSegment(path, "venv")
}

func looksLikeGlobalBinaryPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base != "tally" && base != "tally.exe" {
		return false
	}
	for _, segment := range []string{"bin", "sbin", "go", "tools", "packages"} {
		if pathHasSegment(path, segment) {
			return true
		}
	}
	return false
}

func pathHasSegment(pathValue, want string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(pathValue), func(r rune) bool { return r == '/' }) {
		if strings.EqualFold(part, want) {
			return true
		}
	}
	return false
}

func appendPathList(paths []string, value string) []string {
	for _, part := range filepath.SplitList(strings.TrimSpace(value)) {
		if part = strings.TrimSpace(part); part != "" {
			paths = append(paths, part)
		}
	}
	return paths
}

func cleanPathList(paths []string) []string {
	out := paths[:0]
	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		out = append(out, filepath.Clean(abs))
	}
	return out
}
