//go:build ignore

// This program generates the JSON schema for tally configuration.
// Run with: go run gen/jsonschema.go > schema.json
package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/invopop/jsonschema"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"

	// Import all rules to register them
	_ "github.com/wharflab/tally/internal/rules/all"
)

func main() {
	r := &jsonschema.Reflector{
		ExpandedStruct: true,
	}

	// Generate base config schema
	schema := r.Reflect(&config.Config{})
	schema.ID = "https://json.schemastore.org/tally.json"
	schema.Title = "tally configuration"
	schema.Description = "Configuration schema for tally Dockerfile linter"

	// Add rule-specific config schemas
	addRuleConfigSchemas(r, schema)

	// Enhance OutputConfig with enums/defaults (too long for struct tags)
	enhanceOutputConfigSchema(schema)

	// Enhance AIConfig with descriptions (kept short in struct tags for lll).
	enhanceAIConfigSchema(schema)

	// Fix required fields - all config fields should be optional
	fixRequiredFields(schema)

	// Add generation timestamp as comment
	schema.Comments = fmt.Sprintf("Auto-generated on %s. Do not edit manually.",
		time.Now().Format("2006-01-02"))

	// Output as pretty-printed JSON
	data, err := json.Marshal(
		schema,
		jsontext.EscapeForHTML(true),
		jsontext.WithIndentPrefix(""),
		jsontext.WithIndent("  "),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling schema: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

// addRuleConfigSchemas adds JSON schemas for configurable rules.
// Dynamically discovers rules from the registry instead of hardcoding.
func addRuleConfigSchemas(r *jsonschema.Reflector, schema *jsonschema.Schema) {
	// Get the rules property from the schema
	rulesSchema, ok := schema.Properties.Get("rules")
	if !ok || rulesSchema == nil {
		return
	}

	if schema.Definitions == nil {
		schema.Definitions = make(jsonschema.Definitions)
	}

	// Iterate over all registered rules
	for _, rule := range rules.All() {
		// Check if rule implements ConfigurableRule
		confRule, ok := rule.(rules.ConfigurableRule)
		if !ok {
			continue
		}

		// Get the default config from the rule
		cfg := confRule.DefaultConfig()
		if cfg == nil {
			continue
		}

		// Generate schema from the config type
		ruleSchema := r.Reflect(cfg)

		// Extract short rule name from the full code (e.g., "tally/max-lines" -> "max-lines")
		meta := rule.Metadata()
		ruleName := meta.Code
		if idx := strings.LastIndex(meta.Code, "/"); idx >= 0 {
			ruleName = meta.Code[idx+1:]
		}

		ruleSchema.Description = fmt.Sprintf("Configuration for %s rule", ruleName)

		// Store the schema definition
		defName := fmt.Sprintf("%sConfig", ruleName)
		schema.Definitions[defName] = ruleSchema
	}
}

// fixRequiredFields removes the required array from schemas where all fields should be optional.
// This is needed because the omitempty tag doesn't work on nested struct fields.
func fixRequiredFields(schema *jsonschema.Schema) {
	// Top-level config fields are all optional
	schema.Required = nil

	// RuleConfig fields should be optional with descriptions
	if ruleDef, ok := schema.Definitions["RuleConfig"]; ok {
		ruleDef.Required = nil
		if severity, ok := ruleDef.Properties.Get("severity"); ok {
			severity.Description = "Override the rule's default severity"
		}
		if exclude, ok := ruleDef.Properties.Get("exclude"); ok {
			exclude.Description = "Path patterns where this rule should not run"
		}
	}

	// InlineDirectivesConfig - add back descriptions that were removed for lll
	if inlineDef, ok := schema.Definitions["InlineDirectivesConfig"]; ok {
		if validateRules, ok := inlineDef.Properties.Get("validate-rules"); ok {
			validateRules.Description = "Warn about unknown rule codes in directives"
		}
		if requireReason, ok := inlineDef.Properties.Get("require-reason"); ok {
			requireReason.Description = "Require reason= on all ignore directives"
		}
	}

	// AIConfig fields should be optional (AI is opt-in).
	if aiDef, ok := schema.Definitions["AIConfig"]; ok {
		aiDef.Required = nil
	}
}

// enhanceOutputConfigSchema adds enum/default/description to OutputConfig fields.
// This is done programmatically to avoid long struct tag lines.
func enhanceOutputConfigSchema(schema *jsonschema.Schema) {
	outputDef, ok := schema.Definitions["OutputConfig"]
	if !ok || outputDef == nil {
		return
	}

	// Enhance format field
	if format, ok := outputDef.Properties.Get("format"); ok {
		format.Enum = []any{"text", "json", "sarif", "github-actions", "markdown"}
		format.Default = "text"
		format.Description = "Output format"
	}

	// Enhance path field
	if path, ok := outputDef.Properties.Get("path"); ok {
		path.Default = "stdout"
		path.Description = "Output destination: stdout, stderr, or file path"
	}

	// Enhance show-source field
	if showSource, ok := outputDef.Properties.Get("show-source"); ok {
		showSource.Default = true
		showSource.Description = "Show source code snippets in text output"
	}

	// Enhance fail-level field
	if failLevel, ok := outputDef.Properties.Get("fail-level"); ok {
		failLevel.Enum = []any{"error", "warning", "info", "style", "none"}
		failLevel.Default = "style"
		failLevel.Description = "Minimum severity for non-zero exit code"
	}
}

// enhanceAIConfigSchema adds descriptions to AIConfig fields.
// This is done programmatically to avoid long struct tag lines.
func enhanceAIConfigSchema(schema *jsonschema.Schema) {
	aiDef, ok := schema.Definitions["AIConfig"]
	if !ok || aiDef == nil {
		return
	}

	if maxInputBytes, ok := aiDef.Properties.Get("max-input-bytes"); ok && maxInputBytes.Description == "" {
		maxInputBytes.Description = "Maximum input bytes sent to agent"
	}
	if redactSecrets, ok := aiDef.Properties.Get("redact-secrets"); ok && redactSecrets.Description == "" {
		redactSecrets.Description = "Redact secrets before sending content to agent"
	}
}
