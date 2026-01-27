package semantic

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// ArgEntry represents a single ARG declaration.
type ArgEntry struct {
	// Name is the argument name.
	Name string
	// Value is the default value (nil means no default).
	Value *string
	// Location is where the ARG was declared.
	Location []parser.Range
}

// EnvEntry represents a single ENV declaration.
type EnvEntry struct {
	// Name is the environment variable name.
	Name string
	// Value is the environment variable value.
	Value string
	// Location is where the ENV was declared.
	Location []parser.Range
}

// VariableScope manages ARG and ENV variable resolution for a stage.
// It implements Docker's variable precedence rules.
type VariableScope struct {
	// parent is the global scope (for global ARGs), nil for global scope itself.
	parent *VariableScope

	// args maps variable names to their ARG entries.
	args map[string]*ArgEntry

	// envs maps variable names to their ENV entries.
	envs map[string]*EnvEntry

	// argOrder preserves ARG declaration order.
	argOrder []string

	// envOrder preserves ENV declaration order.
	envOrder []string
}

// NewGlobalScope creates a new global variable scope.
func NewGlobalScope() *VariableScope {
	return &VariableScope{
		args: make(map[string]*ArgEntry),
		envs: make(map[string]*EnvEntry),
	}
}

// NewStageScope creates a new stage scope with the given parent (global scope).
func NewStageScope(parent *VariableScope) *VariableScope {
	return &VariableScope{
		parent: parent,
		args:   make(map[string]*ArgEntry),
		envs:   make(map[string]*EnvEntry),
	}
}

// AddArg adds an ARG declaration to the scope.
func (s *VariableScope) AddArg(name string, value *string, location []parser.Range) {
	entry := &ArgEntry{
		Name:     name,
		Value:    value,
		Location: location,
	}
	if _, exists := s.args[name]; !exists {
		s.argOrder = append(s.argOrder, name)
	}
	s.args[name] = entry
}

// AddEnv adds an ENV declaration to the scope.
func (s *VariableScope) AddEnv(name, value string, location []parser.Range) {
	entry := &EnvEntry{
		Name:     name,
		Value:    value,
		Location: location,
	}
	if _, exists := s.envs[name]; !exists {
		s.envOrder = append(s.envOrder, name)
	}
	s.envs[name] = entry
}

// AddArgCommand adds all arguments from an ARG instruction.
func (s *VariableScope) AddArgCommand(cmd *instructions.ArgCommand) {
	for _, arg := range cmd.Args {
		s.AddArg(arg.Key, arg.Value, cmd.Location())
	}
}

// AddEnvCommand adds all variables from an ENV instruction.
func (s *VariableScope) AddEnvCommand(cmd *instructions.EnvCommand) {
	for _, env := range cmd.Env {
		s.AddEnv(env.Key, env.Value, cmd.Location())
	}
}

// Resolve looks up a variable by name using Docker's precedence rules.
// Precedence (highest first):
//  1. Stage ENV (environment variables always take precedence)
//  2. Stage ARG with build-arg override
//  3. Stage ARG with default value (inheriting from global if no local default)
//
// Note: Global ARGs are ONLY visible in a stage if the stage explicitly declares
// them with `ARG NAME`. A global `ARG FOO=1` is NOT automatically available in
// stage instructions until the stage redeclares it.
//
// Returns the value and true if found, or empty string and false if not.
func (s *VariableScope) Resolve(name string, buildArgs map[string]string) (string, bool) {
	// 1. Check stage ENV first (highest priority in Docker)
	if env, found := s.envs[name]; found {
		return env.Value, true
	}

	// 2. Check stage ARG (with build-arg override support)
	if arg, found := s.args[name]; found {
		// Build arg override applies to declared ARGs
		if buildArgs != nil {
			if val, found := buildArgs[name]; found {
				return val, true
			}
		}
		if arg.Value != nil {
			return *arg.Value, true
		}
		// ARG declared but no default - check parent for inherited default
		// This handles: ARG VERSION (in stage) inheriting from ARG VERSION=1.0 (global)
		if s.parent != nil {
			if parentArg := s.parent.GetArg(name); parentArg != nil && parentArg.Value != nil {
				return *parentArg.Value, true
			}
		}
		// ARG declared but no default anywhere - not set
		return "", false
	}

	// 3. For stage scopes (has parent), do NOT fall through to parent
	// Global ARGs are only visible in stages that explicitly declare them
	if s.parent != nil {
		return "", false
	}

	// 4. For global scope (no parent), we're done
	return "", false
}

// HasArg returns true if the variable is declared as an ARG anywhere in the
// scope chain (this scope or any parent). This checks existence across the
// entire scope chain, not resolvability - a global ARG will return true even
// if the stage hasn't redeclared it.
//
// To check if a variable is actually resolvable in this stage's context,
// use Resolve(name, nil) and check the boolean result.
func (s *VariableScope) HasArg(name string) bool {
	if _, found := s.args[name]; found {
		return true
	}
	if s.parent != nil {
		return s.parent.HasArg(name)
	}
	return false
}

// GetArg returns the ARG entry for the given name, searching up the scope
// chain. Like HasArg, this checks existence across the entire scope chain,
// not resolvability - use Resolve to check if a variable is actually
// accessible in this stage's context.
func (s *VariableScope) GetArg(name string) *ArgEntry {
	if arg, found := s.args[name]; found {
		return arg
	}
	if s.parent != nil {
		return s.parent.GetArg(name)
	}
	return nil
}

// GetEnv returns the ENV entry for the given name, or nil if not found.
func (s *VariableScope) GetEnv(name string) *EnvEntry {
	if env, found := s.envs[name]; found {
		return env
	}
	return nil
}

// Args returns all ARG entries in declaration order.
func (s *VariableScope) Args() []*ArgEntry {
	result := make([]*ArgEntry, 0, len(s.argOrder))
	for _, name := range s.argOrder {
		result = append(result, s.args[name])
	}
	return result
}

// Envs returns all ENV entries in declaration order.
func (s *VariableScope) Envs() []*EnvEntry {
	result := make([]*EnvEntry, 0, len(s.envOrder))
	for _, name := range s.envOrder {
		result = append(result, s.envs[name])
	}
	return result
}

// Parent returns the parent scope (nil for global scope).
func (s *VariableScope) Parent() *VariableScope {
	return s.parent
}
