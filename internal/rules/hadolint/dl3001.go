package hadolint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/shell"
)

// defaultInvalidCommands are commands that make no sense inside a Docker container.
// This matches the original Hadolint DL3001 list.
var defaultInvalidCommands = []string{
	"free",
	"kill",
	"mount",
	"ps",
	"service",
	"shutdown",
	"ssh",
	"top",
	"vim",
}

// DL3001Config is the configuration for the DL3001 rule.
type DL3001Config struct {
	// InvalidCommands is the list of commands to flag as invalid inside a container.
	// Defaults to the Hadolint list: free, kill, mount, ps, service, shutdown, ssh, top, vim.
	InvalidCommands []string `json:"invalid-commands,omitempty" koanf:"invalid-commands"`
}

// DefaultDL3001Config returns the default configuration.
func DefaultDL3001Config() DL3001Config {
	return DL3001Config{
		InvalidCommands: slices.Clone(defaultInvalidCommands),
	}
}

// DL3001Rule implements the DL3001 linting rule.
// It warns when RUN instructions contain commands that are meaningless
// inside a Docker container (e.g., ssh, vim, shutdown, service, ps, free, top, kill, mount).
type DL3001Rule struct {
	schema map[string]any
}

// NewDL3001Rule creates a new DL3001 rule instance.
func NewDL3001Rule() *DL3001Rule {
	schema, err := configutil.RuleSchema(rules.HadolintRulePrefix + "DL3001")
	if err != nil {
		panic(err)
	}
	return &DL3001Rule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *DL3001Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code: rules.HadolintRulePrefix + "DL3001",
		Name: "Invalid command in container",
		Description: "For some commands it makes no sense running them in a Docker " +
			"container like ssh, vim, shutdown, service, ps, free, top, kill, mount",
		DocURL:          rules.HadolintDocURL("DL3001"),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "style",
		IsExperimental:  false,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *DL3001Rule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *DL3001Rule) DefaultConfig() any {
	return DefaultDL3001Config()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *DL3001Rule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(r.Metadata().Code, config)
}

// Check runs the DL3001 rule.
// It warns when any RUN instruction contains commands that are meaningless
// inside a Docker container.
func (r *DL3001Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	cfg := r.resolveConfig(input.Config)

	if len(cfg.InvalidCommands) == 0 {
		return nil
	}

	// Build a set for O(1) lookup.
	invalid := make(map[string]bool, len(cfg.InvalidCommands))
	for _, cmd := range cfg.InvalidCommands {
		invalid[cmd] = true
	}

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			cmdStr := dockerfile.RunCommandString(run)
			names := shell.CommandNamesWithVariant(cmdStr, shellVariant)

			var found []string
			for _, name := range names {
				if invalid[name] {
					found = append(found, name)
				}
			}

			if len(found) == 0 {
				return nil
			}

			slices.Sort(found)
			found = slices.Compact(found)

			loc := rules.NewLocationFromRanges(file, run.Location())
			msg := fmt.Sprintf(
				"command %s has no purpose in a Docker container; "+
					"avoid commands like %s",
				strings.Join(found, ", "),
				strings.Join(cfg.InvalidCommands, ", "),
			)
			v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
				WithDocURL(meta.DocURL)

			// Offer a comment-out fix when every command in the RUN is invalid
			// and the instruction fits on a single line (no continuation lines).
			ranges := run.Location()
			singleLine := len(ranges) == 1 && ranges[0].Start.Line == ranges[0].End.Line
			if len(found) == len(names) && singleLine {
				sm := input.SourceMap()
				line := sm.Line(loc.Start.Line - 1)
				if line != "" {
					commented := "# [commented out by tally - " +
						"command has no purpose in a container]: " + line
					v = v.WithSuggestedFix(&rules.SuggestedFix{
						Description: "Comment out RUN instruction that " +
							"only runs container-irrelevant commands",
						Safety: rules.FixSuggestion,
						Edits: []rules.TextEdit{{
							Location: rules.NewRangeLocation(
								file, loc.Start.Line, 0,
								loc.Start.Line, len(line),
							),
							NewText: commented,
						}},
					})
				}
			}

			return []rules.Violation{v}
		},
	)
}

// resolveConfig extracts the DL3001Config from input, falling back to defaults.
func (r *DL3001Rule) resolveConfig(config any) DL3001Config {
	return configutil.Coerce(config, DefaultDL3001Config())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3001Rule())
}
