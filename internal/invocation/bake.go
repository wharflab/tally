package invocation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

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

	files := []bake.File{{Name: entrypoint, Data: data}}
	cfg, _, err := bake.ParseFiles(files, bakeDefaults(baseDir), nil)
	if err != nil {
		return nil, fmt.Errorf("parse Bake file %s: %w", entrypoint, err)
	}

	targetNames := bakeTargetNames(cfg, opts.Targets)

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

func bakeTargetNames(cfg *bake.Config, requested []string) []string {
	if cfg == nil {
		return nil
	}

	if len(requested) > 0 {
		ordered := make([]string, 0, len(requested))
		seen := map[string]struct{}{}
		var indirect []string
		for _, name := range dedupePreserveOrder(requested) {
			targets, _ := cfg.ResolveGroup(name)
			for _, target := range targets {
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
		sort.Strings(indirect)
		return append(ordered, indirect...)
	}

	targets, _ := cfg.ResolveGroup("default")
	targets = dedupePreserveOrder(targets)
	sort.Strings(targets)
	return targets
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
	targetStage := ""
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
		NamedContexts:  nil,
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
	if len(out) == 0 {
		return map[string]ContextRef{}, nil
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
			source = secret.Env
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
