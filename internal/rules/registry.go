package rules

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages rule registration and lookup.
type Registry struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

// NewRegistry creates a new empty registry.
func NewRegistry() *Registry {
	return &Registry{
		rules: make(map[string]Rule),
	}
}

// Register adds a rule to the registry.
// Panics if a rule with the same code is already registered.
func (r *Registry) Register(rule Rule) {
	r.mu.Lock()
	defer r.mu.Unlock()

	code := rule.Metadata().Code
	if _, exists := r.rules[code]; exists {
		panic(fmt.Sprintf("rule %q already registered", code))
	}
	r.rules[code] = rule
}

// Get retrieves a rule by its code.
// Returns nil if no rule is found.
func (r *Registry) Get(code string) Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rules[code]
}

// Has returns true if a rule with the given code is registered.
func (r *Registry) Has(code string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.rules[code]
	return exists
}

// All returns all registered rules sorted by code.
func (r *Registry) All() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0, len(r.rules))
	for _, rule := range r.rules {
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata().Code < result[j].Metadata().Code
	})
	return result
}

// Codes returns all registered rule codes sorted alphabetically.
func (r *Registry) Codes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	codes := make([]string, 0, len(r.rules))
	for code := range r.rules {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// EnabledByDefault returns rules that are enabled by default.
func (r *Registry) EnabledByDefault() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0)
	for _, rule := range r.rules {
		if rule.Metadata().EnabledByDefault {
			result = append(result, rule)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata().Code < result[j].Metadata().Code
	})
	return result
}

// ByCategory returns rules filtered by category.
func (r *Registry) ByCategory(category string) []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0)
	for _, rule := range r.rules {
		if rule.Metadata().Category == category {
			result = append(result, rule)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata().Code < result[j].Metadata().Code
	})
	return result
}

// BySeverity returns rules filtered by default severity.
func (r *Registry) BySeverity(severity Severity) []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0)
	for _, rule := range r.rules {
		if rule.Metadata().DefaultSeverity == severity {
			result = append(result, rule)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata().Code < result[j].Metadata().Code
	})
	return result
}

// Experimental returns rules marked as experimental.
func (r *Registry) Experimental() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0)
	for _, rule := range r.rules {
		if rule.Metadata().IsExperimental {
			result = append(result, rule)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata().Code < result[j].Metadata().Code
	})
	return result
}

// defaultRegistry is the global default registry.
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the global default registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Register adds a rule to the default registry.
func Register(rule Rule) {
	defaultRegistry.Register(rule)
}

// Get retrieves a rule from the default registry.
func Get(code string) Rule {
	return defaultRegistry.Get(code)
}

// All returns all rules from the default registry.
func All() []Rule {
	return defaultRegistry.All()
}

// Codes returns all rule codes from the default registry.
func Codes() []string {
	return defaultRegistry.Codes()
}

// EnabledDefault returns rules enabled by default from the default registry.
func EnabledDefault() []Rule {
	return defaultRegistry.EnabledByDefault()
}
