package invocation

import (
	"bufio"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// CanonicalPath returns an absolute, cleaned local path.
func CanonicalPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// InvocationKey returns the stable cross-run identity for an invocation.
func InvocationKey(source InvocationSource, dockerfilePath string) string {
	return strings.Join([]string{
		source.Kind,
		filepath.Clean(source.File),
		source.Name,
		filepath.Clean(dockerfilePath),
	}, "\x00")
}

// LabelForSource returns a human-readable invocation label for grouping.
func LabelForSource(source *InvocationSource) string {
	if source == nil || source.Kind == "" {
		return ""
	}
	switch source.Kind {
	case KindBake:
		if source.Name != "" {
			return "bake target: " + source.Name
		}
		return "bake"
	case KindCompose:
		if source.Name != "" {
			return "compose service: " + source.Name
		}
		return "compose"
	case KindDockerfile:
		return ""
	default:
		if source.Name != "" {
			return source.Kind + ": " + source.Name
		}
		return source.Kind
	}
}

// IsDockerfileName reports whether a path follows common Dockerfile naming
// conventions.
func IsDockerfileName(path string) bool {
	base := filepath.Base(path)
	if base == "Dockerfile" || base == "Containerfile" {
		return true
	}
	if strings.HasPrefix(base, "Dockerfile.") || strings.HasPrefix(base, "Containerfile.") {
		return true
	}
	if strings.HasSuffix(base, ".Dockerfile") || strings.HasSuffix(base, ".Containerfile") {
		return true
	}
	return false
}

// IsObviousOrchestratorName reports whether a filename extension implies an
// orchestrator entrypoint candidate.
func IsObviousOrchestratorName(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains([]string{".hcl", ".json", ".yml", ".yaml"}, ext)
}

// ClassifyContextRef normalizes and classifies a declared build context.
func ClassifyContextRef(baseDir, raw string) (ContextRef, error) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "." {
		return normalizeLocalContextValue(baseDir, ".", ContextKindDir)
	}

	switch {
	case value == "-":
		return ContextRef{Kind: ContextKindEmpty}, nil
	case strings.HasPrefix(value, "target:"):
		return ContextRef{Kind: ContextKindTarget, Value: value}, nil
	case strings.HasPrefix(value, "service:"):
		return ContextRef{Kind: ContextKindService, Value: value}, nil
	case strings.HasPrefix(value, "docker-image://"):
		return ContextRef{Kind: ContextKindDockerImage, Value: value}, nil
	case strings.HasPrefix(value, "docker-image:"):
		return ContextRef{Kind: ContextKindDockerImage, Value: "docker-image://" + strings.TrimPrefix(value, "docker-image:")}, nil
	case strings.HasPrefix(value, "oci-layout://"):
		rest := strings.TrimPrefix(value, "oci-layout://")
		return normalizeLocalContextValue(baseDir, rest, ContextKindOCILayout)
	case strings.HasPrefix(value, "git@"):
		return ContextRef{Kind: ContextKindGit, Value: value}, nil
	}

	if u, err := url.Parse(value); err == nil && u.Scheme != "" {
		switch u.Scheme {
		case "git", "ssh":
			return ContextRef{Kind: ContextKindGit, Value: value}, nil
		case "http", "https":
			if looksLikeGitURL(value) {
				return ContextRef{Kind: ContextKindGit, Value: value}, nil
			}
			if looksLikeTar(value) {
				return ContextRef{Kind: ContextKindTar, Value: value}, nil
			}
			return ContextRef{Kind: ContextKindURL, Value: value}, nil
		}
	}

	kind := ContextKindDir
	if looksLikeTar(value) {
		kind = ContextKindTar
	}
	return normalizeLocalContextValue(baseDir, value, kind)
}

// ResolveDockerfilePath resolves a Dockerfile declaration relative to a local
// primary context. Non-local primary contexts cannot produce a stable local
// Dockerfile path in the MVP unless the Dockerfile path is absolute.
func ResolveDockerfilePath(baseDir string, ctx ContextRef, dockerfile string) (string, error) {
	dockerfile = strings.TrimSpace(dockerfile)
	if dockerfile == "" {
		dockerfile = defaultDockerfileName
	}
	if filepath.IsAbs(dockerfile) {
		return CanonicalPath(dockerfile)
	}
	if ctx.Kind != ContextKindDir {
		return "", fmt.Errorf(
			"dockerfile %q uses non-local context %q; remote/non-local Dockerfile paths are not supported",
			dockerfile,
			ctx.Value,
		)
	}
	if ctx.Value == "" {
		base, err := CanonicalPath(baseDir)
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.Join(base, dockerfile)), nil
	}
	return filepath.Clean(filepath.Join(ctx.Value, dockerfile)), nil
}

// ProbeEntrypointKind performs a cheap content-based classification for files
// whose extension alone is ambiguous.
func ProbeEntrypointKind(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".hcl":
		return KindBake, true
	case ".yml", ".yaml":
		return KindCompose, true
	case ".json":
		return probeJSONEntrypointKind(path)
	default:
		return probeTextEntrypointKind(path)
	}
}

// ConcreteBuildArgs returns only build args with concrete values.
func ConcreteBuildArgs(args map[string]*string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		if v != nil {
			out[k] = *v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneStringPtrMap(in map[string]*string) map[string]*string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*string, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}

func dedupePreserveOrder(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func normalizeLocalContextValue(baseDir, value, kind string) (ContextRef, error) {
	base, err := CanonicalPath(baseDir)
	if err != nil {
		return ContextRef{}, err
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return ContextRef{Kind: kind, Value: filepath.Clean(path)}, nil
}

func probeJSONEntrypointKind(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var top map[string]jsontext.Value
	if err := jsonv2.Unmarshal(data, &top); err != nil {
		return "", false
	}
	if _, ok := top["services"]; ok {
		return KindCompose, true
	}
	if _, ok := top["target"]; ok {
		return KindBake, true
	}
	if _, ok := top["targets"]; ok {
		return KindBake, true
	}
	if _, ok := top["group"]; ok {
		return KindBake, true
	}
	return "", false
}

func probeTextEntrypointKind(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	checked := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		checked++
		switch {
		case line == "services:" || strings.HasPrefix(line, "services: "):
			return KindCompose, true
		case strings.HasPrefix(line, "target ") ||
			strings.HasPrefix(line, "group ") ||
			strings.HasPrefix(line, "targets ") ||
			strings.HasPrefix(line, "targets ="):
			return KindBake, true
		}
		if checked >= 64 {
			break
		}
	}
	return "", false
}

func looksLikeGitURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, ".git") || strings.Contains(lower, "#") && strings.Contains(lower, "github.com")
}

func looksLikeTar(value string) bool {
	lower := strings.ToLower(strings.Split(value, "?")[0])
	for _, suffix := range []string{".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func parsePortRange(value string) (int, int, error) {
	startS, endS, hasRange := strings.Cut(value, "-")
	start, err := strconv.Atoi(startS)
	if err != nil {
		return 0, 0, err
	}
	if !hasRange {
		return start, start, nil
	}
	end, err := strconv.Atoi(endS)
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, fmt.Errorf("invalid descending port range %q", value)
	}
	return start, end, nil
}
