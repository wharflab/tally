package semantic

import (
	"runtime"
	"slices"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	dfshell "github.com/moby/buildkit/frontend/dockerfile/shell"
	"github.com/moby/buildkit/util/suggest"
)

// UndefinedVarRef represents a variable reference (e.g., $FOO) used in a stage
// command that was not defined at the point of use.
type UndefinedVarRef struct {
	// Name is the referenced variable name without $ or ${}.
	Name string
	// Suggest is an optional suggested variable name.
	Suggest string
	// Location is where the undefined variable was used.
	Location []parser.Range
}

var undefinedVarNonEnvArgs = map[string]struct{}{
	"BUILDKIT_SBOM_SCAN_CONTEXT": {},
	"BUILDKIT_SBOM_SCAN_STAGE":   {},
}

func defaultExternalImageEnv() map[string]string {
	// Approximate a minimal baseline environment for external images. BuildKit
	// resolves real base image configs; tally is static so it can only infer a
	// small, commonly-present subset to reduce false positives.
	return map[string]string{
		"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
}

func rawLexForUndefinedVar() *dfshell.Lex {
	lex := dfshell.NewLex('\\')
	lex.SkipProcessQuotes = true
	return lex
}

func addUnmatched(dst, unmatched map[string]struct{}) {
	for k := range unmatched {
		dst[k] = struct{}{}
	}
}

func undefinedVarRefsFromUnmatched(
	location []parser.Range,
	unmatched map[string]struct{},
	declaredArgs map[string]struct{},
	options []string,
) []UndefinedVarRef {
	if len(unmatched) == 0 {
		return nil
	}

	// Make a copy since we mutate the set.
	cp := make(map[string]struct{}, len(unmatched))
	for k := range unmatched {
		cp[k] = struct{}{}
	}

	// Match BuildKit behavior: ARG declared without a value is considered
	// "defined" for linting (so don't warn).
	for k := range declaredArgs {
		delete(cp, k)
	}

	// Ignore internal non-env args that are not part of the environment.
	for k := range undefinedVarNonEnvArgs {
		delete(cp, k)
	}

	if len(cp) == 0 {
		return nil
	}

	names := make([]string, 0, len(cp))
	for name := range cp {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]UndefinedVarRef, 0, len(names))
	for _, name := range names {
		match, _ := suggest.Search(name, options, runtime.GOOS != "windows")
		out = append(out, UndefinedVarRef{
			Name:     name,
			Suggest:  match,
			Location: location,
		})
	}
	return out
}

func undefinedVarsInCommand(
	cmd instructions.Command,
	shlex *dfshell.Lex,
	env *fromEnv,
	declaredArgs map[string]struct{},
) []UndefinedVarRef {
	if cmd == nil || shlex == nil || env == nil {
		return nil
	}

	unmatched := make(map[string]struct{})

	// Match BuildKit: ARG defaults are handled specially (see dispatchArg), so
	// skip them here.
	if _, isArg := cmd.(*instructions.ArgCommand); !isArg {
		if ex, ok := cmd.(instructions.SupportsSingleWordExpansion); ok {
			if err := ex.Expand(func(word string) (string, error) {
				_, um, err := shlex.ProcessWord(word, env)
				if err == nil {
					addUnmatched(unmatched, um)
				}
				return word, nil
			}); err != nil {
				// Best-effort linting: ignore expansion errors (BuildKit would fail the build).
				_ = err
			}
		}
	}

	if ex, ok := cmd.(instructions.SupportsSingleWordExpansionRaw); ok {
		rawLex := rawLexForUndefinedVar()
		if err := ex.ExpandRaw(func(word string) (string, error) {
			_, um, err := rawLex.ProcessWord(word, env)
			if err == nil {
				addUnmatched(unmatched, um)
			}
			return word, nil
		}); err != nil {
			// Best-effort linting: ignore expansion errors (BuildKit would fail the build).
			_ = err
		}
	}

	return undefinedVarRefsFromUnmatched(cmd.Location(), unmatched, declaredArgs, env.Keys())
}

func applyArgCommandToEnv(
	cmd *instructions.ArgCommand,
	shlex *dfshell.Lex,
	env *fromEnv,
	declaredArgs map[string]struct{},
	buildArgs map[string]string,
	globalScope *VariableScope,
) []UndefinedVarRef {
	if cmd == nil || shlex == nil || env == nil {
		return nil
	}

	var out []UndefinedVarRef

	for _, arg := range cmd.Args {
		_, hasOverride := buildArgs[arg.Key]

		// Expand default only when it is used (no --build-arg override).
		if !hasOverride && arg.Value != nil {
			_, um, err := shlex.ProcessWord(*arg.Value, env)
			if err == nil {
				out = append(out, undefinedVarRefsFromUnmatched(cmd.Location(), um, declaredArgs, env.Keys())...)
			}
		}

		// Declare ARG name for future undefined-var filtering.
		if declaredArgs != nil {
			declaredArgs[arg.Key] = struct{}{}
		}

		// Compute the effective value (override > default expansion > inherited global).
		var effective *string
		if hasOverride {
			vv := buildArgs[arg.Key]
			effective = &vv
		}

		if effective == nil && !hasOverride && arg.Value != nil {
			v, _, err := shlex.ProcessWord(*arg.Value, env)
			if err == nil {
				vv := v
				effective = &vv
			}
		}

		if effective == nil && arg.Value == nil && globalScope != nil {
			if parent := globalScope.GetArg(arg.Key); parent != nil && parent.Value != nil {
				vv := *parent.Value
				effective = &vv
			}
		}

		if effective != nil {
			if _, skip := undefinedVarNonEnvArgs[arg.Key]; !skip {
				env.Set(arg.Key, *effective)
			}
		}
	}

	return out
}

func applyEnvCommandToEnv(cmd *instructions.EnvCommand, shlex *dfshell.Lex, env *fromEnv) {
	if cmd == nil || shlex == nil || env == nil {
		return
	}

	// Match BuildKit expansion semantics: all expansions in a single ENV
	// instruction are evaluated against the pre-instruction environment.
	pre := newFromEnv(env.vars)

	for _, kv := range cmd.Env {
		key, _, err := shlex.ProcessWord(kv.Key, pre)
		if err != nil {
			continue
		}
		val, _, err := shlex.ProcessWord(kv.Value, pre)
		if err != nil {
			continue
		}
		env.Set(key, val)
	}
}
