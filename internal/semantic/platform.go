package semantic

import (
	"os"
	"strings"

	"github.com/containerd/platforms"
	dfshell "github.com/moby/buildkit/frontend/dockerfile/shell"
)

// ExpectedPlatform determines the expected platform for a stage.
//
// Resolution order:
//  1. FROM --platform if present and resolvable via the semantic model's fromArgEval
//  2. DOCKER_DEFAULT_PLATFORM environment variable
//  3. runtime.GOOS/runtime.GOARCH (host platform)
//
// Returns the platform string (e.g., "linux/amd64") and any unresolved ARG names.
func ExpectedPlatform(info *StageInfo, model *Model) (string, []string) {
	if info == nil || info.Stage == nil {
		return defaultPlatform(), nil
	}

	// If the stage has an explicit --platform, try to resolve it.
	if info.Stage.Platform != "" {
		resolved, unresolvedArgs := resolvePlatformExpr(info.Stage.Platform, model)
		if len(unresolvedArgs) == 0 && resolved != "" {
			return resolved, nil
		}
		// If there are unresolved ARGs, fall back to default but report them.
		if len(unresolvedArgs) > 0 {
			return defaultPlatform(), unresolvedArgs
		}
		// If resolved is non-empty even with some unresolved, use it.
		if resolved != "" {
			return resolved, unresolvedArgs
		}
	}

	return defaultPlatform(), nil
}

// resolvePlatformExpr expands ARG references in a --platform expression.
func resolvePlatformExpr(expr string, model *Model) (string, []string) {
	if model == nil {
		return expr, nil
	}

	// Build an environment from meta ARGs + build args + automatic platform args.
	env := newFromEnv(defaultFromArgs(targetStageName(model.stages), model.buildArgs))

	// Add meta ARGs.
	for _, ma := range model.metaArgs {
		for _, kv := range ma.Args {
			val := kv.Value
			if model.buildArgs != nil {
				if ov, ok := model.buildArgs[kv.Key]; ok {
					v := ov
					val = &v
				}
			}
			if val != nil {
				env.Set(kv.Key, *val)
			}
		}
	}

	escapeToken := rune('\\')
	shlex := dfshell.NewLex(escapeToken)

	res, err := shlex.ProcessWordWithMatches(expr, env)
	if err != nil {
		return expr, nil
	}

	var unresolved []string
	for name := range res.Unmatched {
		unresolved = append(unresolved, name)
	}

	return res.Result, unresolved
}

// defaultPlatform returns the default target platform.
// Checks DOCKER_DEFAULT_PLATFORM first, then falls back to host platform.
func defaultPlatform() string {
	if dp := os.Getenv("DOCKER_DEFAULT_PLATFORM"); dp != "" {
		return dp
	}
	spec := platforms.DefaultSpec()
	p := spec.OS + "/" + spec.Architecture
	if spec.Variant != "" {
		p += "/" + spec.Variant
	}
	return p
}

// ParsePlatform splits a platform string into OS, Arch, Variant.
func ParsePlatform(platform string) (string, string, string) {
	parts := strings.SplitN(platform, "/", 3)
	var pOS, arch, variant string
	if len(parts) >= 1 {
		pOS = parts[0]
	}
	if len(parts) >= 2 {
		arch = parts[1]
	}
	if len(parts) >= 3 {
		variant = parts[2]
	}
	return pOS, arch, variant
}
