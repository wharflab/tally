package tally

import (
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
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

// secretMountsRunContext bundles per-RUN data needed by both check and fix.
type secretMountsRunContext struct {
	script       string
	shellVariant shell.Variant
	// cacheMountsEnabled is true when prefer-package-cache-mounts is also active.
	// When true, the fix includes cache mounts so both apply in a single --fix pass.
	cacheMountsEnabled bool
}

// Check runs the rule.
func (r *RequireSecretMountsRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	if len(cfg.Commands) == 0 {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()

	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Safe assertion with nil fallback

	commandNames := make([]string, 0, len(cfg.Commands))
	for name := range cfg.Commands {
		commandNames = append(commandNames, name)
	}

	cacheMountsEnabled := input.IsRuleEnabled(PreferPackageCacheMountsRuleCode)

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
			rctx := secretMountsRunContext{
				script:             script,
				shellVariant:       shellVariant,
				cacheMountsEnabled: cacheMountsEnabled,
			}
			vs := r.checkMounts(input.File, meta, sm, run, existing, found, cfg.Commands, rctx)
			violations = append(violations, vs...)
		}
	}

	return violations
}

// checkMounts checks whether existing mounts satisfy the required secrets for found commands.
// It deduplicates: if multiple commands need the same secret id, only one violation is emitted.
func (r *RequireSecretMountsRule) checkMounts(
	file string,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	existing []*instructions.Mount,
	found []shell.CommandInfo,
	commands map[string]SecretMountSpec,
	rctx secretMountsRunContext,
) []rules.Violation {
	// Collect missing mounts, deduplicated by secret id.
	type missingMount struct {
		spec    SecretMountSpec
		cmdName string
	}
	seen := map[string]bool{}
	var missing []missingMount

	for _, cmd := range found {
		spec, ok := commands[cmd.Name]
		if !ok {
			continue
		}
		if seen[spec.ID] {
			continue
		}
		seen[spec.ID] = true

		msg := checkSecretMount(existing, spec, cmd.Name)
		if msg == "" {
			continue
		}
		missing = append(missing, missingMount{spec: spec, cmdName: cmd.Name})
	}

	if len(missing) == 0 {
		return nil
	}

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	violations := make([]rules.Violation, 0, len(missing))
	for _, m := range missing {
		msg := checkSecretMount(existing, m.spec, m.cmdName)
		edits := r.buildFixEdits(file, sm, run, existing, m.spec, rctx)

		v := rules.NewViolation(
			rules.NewLocationFromRanges(file, runLoc),
			meta.Code,
			msg,
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL).WithDetail(
			fmt.Sprintf("Add --mount=type=secret,id=%s,target=%s for '%s'", m.spec.ID, m.spec.Target, m.cmdName),
		).WithSuggestedFix(&rules.SuggestedFix{
			Description: fmt.Sprintf("Add secret mount (id=%s, target=%s)", m.spec.ID, m.spec.Target),
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       edits,
		})
		violations = append(violations, v)
	}

	return violations
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

// buildFixEdits creates TextEdits to add the missing secret mount to the RUN instruction.
// When prefer-package-cache-mounts is also enabled, the fix includes cache mounts so
// both rules' fixes compose in a single --fix pass (this rule wins conflict resolution
// because security outranks performance).
func (r *RequireSecretMountsRule) buildFixEdits(
	file string,
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	existing []*instructions.Mount,
	spec SecretMountSpec,
	rctx secretMountsRunContext,
) []rules.TextEdit {
	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	// Clone existing mounts and add the missing secret mount.
	mergedMounts := make([]*instructions.Mount, len(existing))
	copy(mergedMounts, existing)

	mergedMounts = append(mergedMounts, &instructions.Mount{
		Type:    instructions.MountTypeSecret,
		CacheID: spec.ID,
		Target:  spec.Target,
	})

	// When prefer-package-cache-mounts is also enabled, include cache mounts
	// in this fix so both apply in one pass. This rule's fix wins conflict
	// resolution (security > performance), so the cache mount rule's overlapping
	// fix is skipped — but the cache mounts are already present.
	if rctx.cacheMountsEnabled {
		cacheMounts, _ := detectRequiredCacheMounts(rctx.script, rctx.shellVariant, "/", nil)
		mergedMounts, _ = mergeCacheMounts(mergedMounts, cacheMounts)
	}

	script := getRunScriptFromCmd(run)
	replacement := formatUpdatedRun(run, mergedMounts, script)
	if replacement == "" {
		return nil
	}

	endLine, endCol := resolveRunEndPosition(runLoc, sm, run)

	return []rules.TextEdit{
		{
			Location: rules.NewRangeLocation(
				file,
				runLoc[0].Start.Line,
				runLoc[0].Start.Character,
				endLine,
				endCol,
			),
			NewText: replacement,
		},
	}
}

func init() {
	rules.Register(NewRequireSecretMountsRule())
}
