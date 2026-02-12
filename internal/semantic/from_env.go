package semantic

import (
	"maps"
	"os"
	"slices"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"github.com/moby/buildkit/util/suggest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const defaultTargetStageName = "default"

// fromEnv is a minimal EnvGetter implementation for BuildKit's shell lexer.
// It is used for evaluating ARG expansions in FROM.
type fromEnv struct {
	vars map[string]string
}

func newFromEnv(vars map[string]string) *fromEnv {
	cp := make(map[string]string, len(vars))
	maps.Copy(cp, vars)
	return &fromEnv{vars: cp}
}

func (e *fromEnv) Get(key string) (string, bool) {
	v, ok := e.vars[key]
	return v, ok
}

func (e *fromEnv) Keys() []string {
	keys := make([]string, 0, len(e.vars))
	for k := range e.vars {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (e *fromEnv) Set(key, value string) {
	e.vars[key] = value
}

// defaultFromArgs returns BuildKit-like automatic platform ARGs used for
// expanding variables in FROM. Values can be overridden by build args.
// TARGETPLATFORM respects DOCKER_DEFAULT_PLATFORM when set, matching BuildKit
// behavior where --platform overrides the target platform.
func defaultFromArgs(targetStage string, overrides map[string]string) map[string]string {
	bp := platforms.DefaultSpec()
	tp := targetPlatformSpec()
	if targetStage == "" {
		targetStage = defaultTargetStageName
	}

	kvs := [...][2]string{
		{"BUILDPLATFORM", platforms.Format(bp)},
		{"BUILDOS", bp.OS},
		{"BUILDOSVERSION", bp.OSVersion},
		{"BUILDARCH", bp.Architecture},
		{"BUILDVARIANT", bp.Variant},
		{"TARGETPLATFORM", platforms.FormatAll(tp)},
		{"TARGETOS", tp.OS},
		{"TARGETOSVERSION", tp.OSVersion},
		{"TARGETARCH", tp.Architecture},
		{"TARGETVARIANT", tp.Variant},
		{"TARGETSTAGE", targetStage},
	}

	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		key := kv[0]
		val := kv[1]
		if overrides != nil {
			if ov, ok := overrides[key]; ok {
				val = ov
			}
		}
		out[key] = val
	}
	return out
}

// targetPlatformSpec returns the target platform spec, checking
// DOCKER_DEFAULT_PLATFORM first, then falling back to host platform.
func targetPlatformSpec() ocispec.Platform {
	if dp := os.Getenv("DOCKER_DEFAULT_PLATFORM"); dp != "" {
		if p, err := platforms.Parse(dp); err == nil {
			return p
		}
	}
	return platforms.DefaultSpec()
}

func scopeArgKeys(scope *VariableScope) ([]string, map[string]struct{}) {
	if scope == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(scope.args))
	set := make(map[string]struct{}, len(scope.args))
	for k := range scope.args {
		keys = append(keys, k)
		set[k] = struct{}{}
	}
	slices.Sort(keys)
	return keys, set
}

func undefinedFromArgs(word string, shlex *shell.Lex, env shell.EnvGetter, knownSet map[string]struct{}, options []string) []FromArgRef {
	if shlex == nil || env == nil {
		return nil
	}
	res, err := shlex.ProcessWordWithMatches(word, env)
	if err != nil || len(res.Unmatched) == 0 {
		return nil
	}

	undefined := make([]string, 0, len(res.Unmatched))
	for name := range res.Unmatched {
		if _, ok := knownSet[name]; ok {
			continue
		}
		undefined = append(undefined, name)
	}
	if len(undefined) == 0 {
		return nil
	}
	slices.Sort(undefined)

	out := make([]FromArgRef, 0, len(undefined))
	for _, name := range undefined {
		s, _ := suggest.Search(name, options, true)
		out = append(out, FromArgRef{Name: name, Suggest: s})
	}
	return out
}

func invalidDefaultBaseName(baseName string, shlex *shell.Lex, env shell.EnvGetter) (bool, error) {
	if shlex == nil || env == nil {
		return false, nil
	}
	res, err := shlex.ProcessWordWithMatches(baseName, env)
	if err != nil {
		return false, err
	}
	_, err = reference.ParseNormalizedNamed(res.Result)
	return err != nil, nil
}
