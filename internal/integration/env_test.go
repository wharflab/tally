package integration

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

func integrationCommandEnv(extra ...string) []string {
	env := append(os.Environ(), "GOCOVERDIR="+coverageDir)
	env = appendEnvOverrides(env, registryEnv...)
	env = appendDiscoveredPowerShellEnv(env)
	return append(env, extra...)
}

func appendDiscoveredPowerShellEnv(env []string) []string {
	if envValue(env, "TALLY_POWERSHELL") == "" {
		if exe := findPowerShellExecutable(); exe != "" {
			env = appendEnvOverride(env, "TALLY_POWERSHELL", exe)
		}
	}
	if envValue(env, "PSModulePath") == "" {
		if moduleRoot := findPSScriptAnalyzerModuleRoot(); moduleRoot != "" {
			env = appendEnvOverride(env, "PSModulePath", moduleRoot)
		}
	}
	return env
}

func findPowerShellExecutable() string {
	names := []string{"pwsh"}
	if runtime.GOOS == "windows" {
		names = append(names, "pwsh.exe")
	}
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return absPath(path)
		}
	}

	for _, path := range powerShellExecutableCandidates() {
		if isFile(path) {
			return path
		}
	}
	return ""
}

func powerShellExecutableCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/bin/pwsh",
			"/usr/local/bin/pwsh",
			"/usr/bin/pwsh",
		}
	case "linux":
		return []string{
			"/usr/bin/pwsh",
			"/usr/local/bin/pwsh",
			"/snap/bin/pwsh",
			"/opt/microsoft/powershell/7/pwsh",
		}
	case "windows":
		return []string{
			`C:\Program Files\PowerShell\7\pwsh.exe`,
			`C:\Program Files (x86)\PowerShell\7\pwsh.exe`,
		}
	default:
		return nil
	}
}

func findPSScriptAnalyzerModuleRoot() string {
	for _, root := range powerShellModuleRootCandidates() {
		if hasPSScriptAnalyzerModule(root) {
			return root
		}
	}
	return ""
}

func powerShellModuleRootCandidates() []string {
	var roots []string
	for _, home := range userHomeCandidates() {
		if runtime.GOOS == "windows" {
			roots = append(roots,
				filepath.Join(home, "Documents", "PowerShell", "Modules"),
				filepath.Join(home, "Documents", "WindowsPowerShell", "Modules"),
			)
			continue
		}
		roots = append(roots, filepath.Join(home, ".local", "share", "powershell", "Modules"))
	}

	switch runtime.GOOS {
	case "darwin":
		roots = append(roots,
			"/usr/local/share/powershell/Modules",
			"/opt/homebrew/share/powershell/Modules",
		)
	case "linux":
		roots = append(roots,
			"/usr/local/share/powershell/Modules",
			"/usr/share/powershell/Modules",
			"/opt/microsoft/powershell/7/Modules",
		)
	case "windows":
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			roots = append(roots,
				filepath.Join(programFiles, "PowerShell", "Modules"),
				filepath.Join(programFiles, "PowerShell", "7", "Modules"),
				filepath.Join(programFiles, "WindowsPowerShell", "Modules"),
			)
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			roots = append(roots, filepath.Join(localAppData, "PowerShell", "Modules"))
		}
	}
	return dedupeNonEmpty(roots)
}

func userHomeCandidates() []string {
	var homes []string
	if home, err := os.UserHomeDir(); err == nil {
		homes = append(homes, home)
	}
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		homes = append(homes, current.HomeDir)
	}
	return dedupeNonEmpty(homes)
}

func hasPSScriptAnalyzerModule(root string) bool {
	matches, err := filepath.Glob(filepath.Join(root, "PSScriptAnalyzer", "*", "PSScriptAnalyzer.psd1"))
	return err == nil && len(matches) > 0
}

func envValue(env []string, key string) string {
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func appendEnvOverride(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		k, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(k, key) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, key+"="+value)
}

func appendEnvOverrides(env []string, overrides ...string) []string {
	out := env
	for _, override := range overrides {
		key, value, ok := strings.Cut(override, "=")
		if !ok {
			continue
		}
		out = appendEnvOverride(out, key, value)
	}
	return out
}

func dedupeNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := values[:0]
	for _, value := range values {
		if value == "" {
			continue
		}
		clean := filepath.Clean(value)
		key := clean
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
