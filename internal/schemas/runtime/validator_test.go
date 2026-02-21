package runtime_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/schemas/runtime"
)

func TestValidateRootConfig(t *testing.T) {
	t.Parallel()

	validator, err := runtime.DefaultValidator()
	if err != nil {
		t.Fatalf("DefaultValidator() error = %v", err)
	}

	valid := map[string]any{
		"rules": map[string]any{
			"tally": map[string]any{
				"max-lines": map[string]any{
					"max": 100,
				},
			},
			"hadolint": map[string]any{
				"DL3026": map[string]any{
					"trusted-registries": []any{"docker.io"},
				},
			},
		},
		"output": map[string]any{
			"format": "json",
		},
	}
	if err := validator.ValidateRootConfig(valid); err != nil {
		t.Fatalf("ValidateRootConfig(valid) error = %v", err)
	}

	invalid := map[string]any{
		"output": map[string]any{
			"format": "xml",
		},
	}
	err = validator.ValidateRootConfig(invalid)
	if err == nil {
		t.Fatal("ValidateRootConfig(invalid) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "root config schema validation failed") {
		t.Fatalf("ValidateRootConfig(invalid) error = %v, want root validation prefix", err)
	}
}

func TestValidateRuleOptions(t *testing.T) {
	t.Parallel()

	validator, err := runtime.DefaultValidator()
	if err != nil {
		t.Fatalf("DefaultValidator() error = %v", err)
	}

	if err := validator.ValidateRuleOptions("tally/max-lines", map[string]any{"max": 10}); err != nil {
		t.Fatalf("ValidateRuleOptions(valid) error = %v", err)
	}

	err = validator.ValidateRuleOptions("tally/max-lines", map[string]any{
		"max":     10,
		"unknown": true,
	})
	if err == nil {
		t.Fatal("ValidateRuleOptions(invalid) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rule tally/max-lines schema validation failed") {
		t.Fatalf("ValidateRuleOptions(invalid) error = %v, want rule validation prefix", err)
	}
}

func TestValidateRuleOptionsUnknownRule(t *testing.T) {
	t.Parallel()

	validator, err := runtime.DefaultValidator()
	if err != nil {
		t.Fatalf("DefaultValidator() error = %v", err)
	}

	err = validator.ValidateRuleOptions("tally/does-not-exist", map[string]any{"foo": "bar"})
	if err == nil {
		t.Fatal("ValidateRuleOptions(unknown) expected error, got nil")
	}
	if !errors.Is(err, runtime.ErrUnknownRuleSchema) {
		t.Fatalf("ValidateRuleOptions(unknown) error = %v, want ErrUnknownRuleSchema", err)
	}
}
