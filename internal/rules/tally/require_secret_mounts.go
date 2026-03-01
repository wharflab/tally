package tally

import (
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// RequireSecretMountsRuleCode is the full rule code for require-secret-mounts.
const RequireSecretMountsRuleCode = rules.TallyRulePrefix + "require-secret-mounts"

// SecretMountSpec defines the required secret mount for a command.
type SecretMountSpec struct {
	ID     string `json:"id" koanf:"id"`
	Target string `json:"target" koanf:"target"`
}

// RequireSecretMountsConfig defines the configuration for the require-secret-mounts rule.
type RequireSecretMountsConfig struct {
	Commands map[string]SecretMountSpec `json:"commands,omitempty" koanf:"commands"`
}

// DefaultRequireSecretMountsConfig returns the default (empty) configuration.
func DefaultRequireSecretMountsConfig() RequireSecretMountsConfig {
	return RequireSecretMountsConfig{
		Commands: map[string]SecretMountSpec{},
	}
}

// RequireSecretMountsRule enforces that configured commands have matching secret mounts.
type RequireSecretMountsRule struct {
	schema map[string]any
}

// NewRequireSecretMountsRule creates a new rule instance.
func NewRequireSecretMountsRule() *RequireSecretMountsRule {
	schema, err := configutil.RuleSchema(RequireSecretMountsRuleCode)
	if err != nil {
		panic(err)
	}
	return &RequireSecretMountsRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *RequireSecretMountsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            RequireSecretMountsRuleCode,
		Name:            "Require secret mounts for private-registry commands",
		Description:     "Enforce --mount=type=secret for commands that access private registries",
		DocURL:          rules.TallyDocURL(RequireSecretMountsRuleCode),
		DefaultSeverity: rules.SeverityOff,
		Category:        "security",
		IsExperimental:  false,
		FixPriority:     85, // Before prefer-package-cache-mounts (90)
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *RequireSecretMountsRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *RequireSecretMountsRule) DefaultConfig() any {
	return DefaultRequireSecretMountsConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *RequireSecretMountsRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(RequireSecretMountsRuleCode, config)
}

// resolveConfig extracts the RequireSecretMountsConfig from input, falling back to defaults.
func (r *RequireSecretMountsRule) resolveConfig(config any) RequireSecretMountsConfig {
	return configutil.Coerce(config, DefaultRequireSecretMountsConfig())
}

// Check runs the rule.
func (r *RequireSecretMountsRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	if len(cfg.Commands) == 0 {
		return nil
	}

	meta := r.Metadata()

	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Safe assertion with nil fallback

	commandNames := make([]string, 0, len(cfg.Commands))
	for name := range cfg.Commands {
		commandNames = append(commandNames, name)
	}

	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				if !shellVariant.IsParseable() {
					continue
				}
			}
		}

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok || !run.PrependShell {
				continue
			}

			script := getRunScriptFromCmd(run)
			if script == "" {
				continue
			}

			found := shell.FindCommands(script, shellVariant, commandNames...)
			if len(found) == 0 {
				continue
			}

			existing := runmount.GetMounts(run)
			vs := r.checkMounts(input.File, meta, run, existing, found, cfg.Commands)
			violations = append(violations, vs...)
		}
	}

	return violations
}

// checkMounts checks whether existing mounts satisfy the required secrets for found commands.
// All missing secrets for the same RUN are combined into a single violation with one
// zero-length insertion edit (the dedup processor drops same-rule same-line duplicates).
func (r *RequireSecretMountsRule) checkMounts(
	file string,
	meta rules.RuleMetadata,
	run *instructions.RunCommand,
	existing []*instructions.Mount,
	found []shell.CommandInfo,
	commands map[string]SecretMountSpec,
) []rules.Violation {
	seen := map[string]bool{}
	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	// Collect all missing specs for this RUN.
	var missingSpecs []SecretMountSpec
	var missingCmds []string

	for _, cmd := range found {
		spec, ok := commands[cmd.Name]
		if !ok {
			continue
		}
		if seen[spec.ID] {
			continue
		}
		seen[spec.ID] = true

		if checkSecretMount(existing, spec, cmd.Name) == "" {
			continue
		}
		missingSpecs = append(missingSpecs, spec)
		missingCmds = append(missingCmds, cmd.Name)
	}

	if len(missingSpecs) == 0 {
		return nil
	}

	// Build a single zero-length insertion containing ALL missing secret mounts.
	edit := buildSecretMountInsertEdit(file, runLoc, missingSpecs)

	// Build a combined message listing all missing secrets.
	msg := checkSecretMount(existing, missingSpecs[0], missingCmds[0])

	var detail string
	for i, spec := range missingSpecs {
		if i > 0 {
			detail += "; "
		}
		detail += fmt.Sprintf("--mount=type=secret,id=%s,target=%s for '%s'", spec.ID, spec.Target, missingCmds[i])
	}

	v := rules.NewViolation(
		rules.NewLocationFromRanges(file, runLoc),
		meta.Code,
		msg,
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"Add " + detail,
	).WithSuggestedFix(&rules.SuggestedFix{
		Description: "Add missing secret mount(s)",
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		Edits:       edit,
	})

	return []rules.Violation{v}
}

// checkSecretMount checks whether existing mounts satisfy a required secret spec.
// Returns an empty string if satisfied, or a violation message.
func checkSecretMount(existing []*instructions.Mount, spec SecretMountSpec, cmdName string) string {
	for _, m := range existing {
		if m.Type != instructions.MountTypeSecret {
			continue
		}
		if m.CacheID == spec.ID && m.Target == spec.Target {
			return "" // Satisfied
		}
		if m.CacheID == spec.ID && m.Target != spec.Target {
			return fmt.Sprintf(
				"secret mount id=%s has target '%s', expected '%s' for '%s'",
				spec.ID, m.Target, spec.Target, cmdName,
			)
		}
	}
	return fmt.Sprintf(
		"missing required secret mount for '%s' (id=%s, target=%s)",
		cmdName, spec.ID, spec.Target,
	)
}

// buildSecretMountInsertEdit creates a zero-length insertion right after "RUN "
// that adds all missing --mount=type=secret flags. The edit is small and targeted:
// it won't conflict with other zero-length insertions at the same point (e.g.,
// cache mount insertion from prefer-package-cache-mounts) or with fixes deeper
// in the command text (e.g., -y insertion, apt→apt-get).
func buildSecretMountInsertEdit(file string, runLoc []parser.Range, specs []SecretMountSpec) []rules.TextEdit {
	var insertText string
	for _, spec := range specs {
		mount := &instructions.Mount{
			Type:    instructions.MountTypeSecret,
			CacheID: spec.ID,
			Target:  spec.Target,
		}
		insertText += runmount.FormatMount(mount) + " "
	}

	// Insert right after "RUN " (keyword is always 3 chars + 1 space).
	insertLine := runLoc[0].Start.Line
	insertCol := runLoc[0].Start.Character + 4 //nolint:mnd // len("RUN ")

	return []rules.TextEdit{
		{
			Location: rules.NewRangeLocation(file, insertLine, insertCol, insertLine, insertCol),
			NewText:  insertText,
		},
	}
}

func init() {
	rules.Register(NewRequireSecretMountsRule())
}
