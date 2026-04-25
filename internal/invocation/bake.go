package invocation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/util/buildflags"
)

// BakeProvider discovers build invocations from Docker Buildx Bake files.
type BakeProvider struct{}

// Discover implements Provider.
func (p BakeProvider) Discover(_ context.Context, opts ResolveOptions) (*DiscoveryResult, error) {
	entrypoint, err := CanonicalPath(opts.Path)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(entrypoint)
	data, err := os.ReadFile(entrypoint)
	if err != nil {
		return nil, fmt.Errorf("read Bake file %s: %w", entrypoint, err)
	}
	if err := rejectUnsupportedMultiFileBake(entrypoint, baseDir); err != nil {
		return nil, err
	}

	files := []bake.File{{Name: entrypoint, Data: data}}
	cfg, _, err := bake.ParseFiles(files, bakeDefaults(baseDir), nil)
	if err != nil {
		return nil, fmt.Errorf("parse Bake file %s: %w", entrypoint, err)
	}

	targetNames, err := bakeTargetNames(cfg, opts.Targets)
	if err != nil {
		return nil, err
	}

	result := &DiscoveryResult{
		Kind:           KindBake,
		EntrypointPath: entrypoint,
		Invocations:    make([]BuildInvocation, 0, len(targetNames)),
	}
	for _, name := range targetNames {
		target, err := cfg.ResolveTarget(name, nil, &bake.EntitlementConf{})
		if err != nil {
			return nil, fmt.Errorf("resolve Bake target %q: %w", name, err)
		}
		inv, err := bakeInvocation(entrypoint, baseDir, name, target)
		if err != nil {
			return nil, err
		}
		result.Invocations = append(result.Invocations, inv)
	}
	if len(result.Invocations) == 0 {
		result.ZeroLintableReason = "Bake file does not define any lintable targets"
	}
	return result, nil
}

func bakeDefaults(baseDir string) map[string]string {
	spec := platforms.DefaultSpec()
	spec.OS = "linux"
	return map[string]string{
		"BAKE_CMD_CONTEXT":    baseDir,
		"BAKE_LOCAL_PLATFORM": platforms.Format(spec),
	}
}

func rejectUnsupportedMultiFileBake(entrypoint, baseDir string) error {
	files, err := additionalBakeFiles(entrypoint, baseDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	return fmt.Errorf(
		"bake file %s appears to be part of a multi-file Bake setup; found additional Bake file(s): %s. "+
			"tally currently supports one explicit Bake entrypoint file, so merge the files or lint the resolved Dockerfiles directly",
		entrypoint,
		strings.Join(files, ", "),
	)
}

func additionalBakeFiles(entrypoint, baseDir string) ([]string, error) {
	candidates, err := bakeFileCandidates(baseDir)
	if err != nil {
		return nil, err
	}
	entrypoint = filepath.Clean(entrypoint)
	files := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if candidate == entrypoint {
			continue
		}
		if !isBakeFileCandidate(candidate, baseDir) {
			continue
		}
		files = append(files, filepath.Base(candidate))
	}
	return files, nil
}

func bakeFileCandidates(baseDir string) ([]string, error) {
	var candidates []string
	for _, pattern := range []string{"*.hcl", "*.json"} {
		matches, err := filepath.Glob(filepath.Join(baseDir, pattern))
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, matches...)
	}
	slices.Sort(candidates)
	return slices.Compact(candidates), nil
}

func isBakeFileCandidate(path, baseDir string) bool {
	if _, ok := defaultBakeFileNames[filepath.Base(path)]; ok {
		return true
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	cfg, _, err := bake.ParseFiles([]bake.File{{Name: path, Data: data}}, bakeDefaults(baseDir), nil)
	if err != nil || cfg == nil {
		return false
	}
	return len(cfg.Groups) > 0 || len(cfg.Targets) > 0
}

var defaultBakeFileNames = map[string]struct{}{
	"docker-bake.json":          {},
	"docker-bake.hcl":           {},
	"docker-bake.override.json": {},
	"docker-bake.override.hcl":  {},
}

func bakeTargetNames(cfg *bake.Config, requested []string) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}
	groupNames, targetNames := bakeNameSets(cfg)

	if len(requested) > 0 {
		ordered := make([]string, 0, len(requested))
		seen := map[string]struct{}{}
		var indirect []string
		for _, name := range dedupePreserveOrder(requested) {
			targets, _ := cfg.ResolveGroup(name)
			_, groupExists := groupNames[name]
			_, targetExists := targetNames[name]
			if len(targets) == 0 || (!groupExists && !targetExists) {
				return nil, fmt.Errorf("unknown bake target %q", name)
			}
			for _, target := range targets {
				if _, ok := targetNames[target]; !ok {
					return nil, fmt.Errorf("unknown bake target %q", target)
				}
				if _, ok := seen[target]; ok {
					continue
				}
				if target == name {
					ordered = append(ordered, target)
				} else {
					indirect = append(indirect, target)
				}
				seen[target] = struct{}{}
			}
		}
		slices.Sort(indirect)
		return append(ordered, indirect...), nil
	}

	targets, _ := cfg.ResolveGroup("default")
	targets = dedupePreserveOrder(targets)
	slices.Sort(targets)
	return targets, nil
}

func bakeNameSets(cfg *bake.Config) (map[string]struct{}, map[string]struct{}) {
	groups := make(map[string]struct{}, len(cfg.Groups))
	for _, group := range cfg.Groups {
		if group != nil {
			groups[group.Name] = struct{}{}
		}
	}
	targets := make(map[string]struct{}, len(cfg.Targets))
	for _, target := range cfg.Targets {
		if target != nil {
			targets[target.Name] = struct{}{}
		}
	}
	return groups, targets
}

func bakeInvocation(entrypoint, baseDir, name string, target *bake.Target) (BuildInvocation, error) {
	if target == nil {
		return BuildInvocation{}, fmt.Errorf("bake target %q is empty", name)
	}
	if target.DockerfileInline != nil && *target.DockerfileInline != "" {
		return BuildInvocation{}, fmt.Errorf("bake target %q uses dockerfile-inline, which is not supported", name)
	}

	contextValue := "."
	if target.Context != nil {
		contextValue = *target.Context
	}
	ctxRef, err := ClassifyContextRef(baseDir, contextValue)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("bake target %q has invalid context: %w", name, err)
	}
	dockerfile := defaultDockerfileName
	if target.Dockerfile != nil {
		dockerfile = *target.Dockerfile
	}
	dockerfilePath, err := ResolveDockerfilePath(baseDir, ctxRef, dockerfile)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("bake target %q: %w", name, err)
	}

	source := InvocationSource{
		Kind: KindBake,
		File: entrypoint,
		Name: name,
	}
	var targetStage string
	if target.Target != nil {
		targetStage = *target.Target
	}
	inv := BuildInvocation{
		Source:         source,
		DockerfilePath: dockerfilePath,
		ContextRef:     ctxRef,
		BuildArgs:      cloneStringPtrMap(target.Args),
		Platforms:      cloneStrings(target.Platforms),
		TargetStage:    targetStage,
		Labels:         bakeLabels(target.Labels),
		Secrets:        bakeSecrets(baseDir, target.Secrets),
	}
	namedContexts, err := normalizeNamedContexts(baseDir, target.Contexts)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("bake target %q has invalid named context: %w", name, err)
	}
	inv.NamedContexts = namedContexts
	inv.Key = InvocationKey(source, dockerfilePath)
	return inv, nil
}

func normalizeNamedContexts(baseDir string, contexts map[string]string) (map[string]ContextRef, error) {
	if len(contexts) == 0 {
		return map[string]ContextRef{}, nil
	}
	out := make(map[string]ContextRef, len(contexts))
	keys := make([]string, 0, len(contexts))
	for key := range contexts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		ref, err := ClassifyContextRef(baseDir, contexts[key])
		if err != nil {
			return nil, fmt.Errorf("%s=%q: %w", key, contexts[key], err)
		}
		out[key] = ref
	}
	return out, nil
}

func bakeLabels(labels map[string]*string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		if value != nil {
			out[key] = *value
		}
	}
	return out
}

func bakeSecrets(baseDir string, secrets buildflags.Secrets) []SecretRef {
	if len(secrets) == 0 {
		return nil
	}
	out := make([]SecretRef, 0, len(secrets))
	for _, secret := range secrets {
		if secret == nil {
			continue
		}
		id := secret.ID
		source := secret.FilePath
		if source != "" && !filepath.IsAbs(source) {
			source = filepath.Clean(filepath.Join(baseDir, source))
		}
		if source == "" {
			source = envSecretSource(secret.Env)
		}
		out = append(out, SecretRef{
			Scope:  SecretScopeBuild,
			ID:     id,
			Source: source,
			Target: id,
		})
	}
	return out
}
