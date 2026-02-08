package tally

import (
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
)

// PreferAddUnpackConfig is the configuration for the prefer-add-unpack rule.
type PreferAddUnpackConfig struct {
	// Enabled controls whether the rule is active. True by default.
	Enabled *bool `json:"enabled,omitempty" koanf:"enabled"`
}

// DefaultPreferAddUnpackConfig returns the default configuration.
func DefaultPreferAddUnpackConfig() PreferAddUnpackConfig {
	t := true
	return PreferAddUnpackConfig{Enabled: &t}
}

// PreferAddUnpackRule flags RUN commands that download and extract remote
// archives (via curl/wget piped to tar, or downloaded then extracted),
// suggesting `ADD --unpack <url> <dest>` instead.
//
// ADD --unpack is a BuildKit feature that downloads and extracts a remote
// archive in a single layer, reducing image size and build complexity.
type PreferAddUnpackRule struct{}

// NewPreferAddUnpackRule creates a new rule instance.
func NewPreferAddUnpackRule() *PreferAddUnpackRule {
	return &PreferAddUnpackRule{}
}

// Metadata returns the rule metadata.
func (r *PreferAddUnpackRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "prefer-add-unpack",
		Name:            "Prefer ADD --unpack for remote archives",
		Description:     "Use `ADD --unpack` instead of downloading and extracting remote archives in `RUN`",
		DocURL:          "",
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		IsExperimental:  false,
		FixPriority:     95,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferAddUnpackRule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"enabled": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "Enable or disable the rule",
			},
		},
		"additionalProperties": false,
	}
}

// DefaultConfig returns the default configuration.
func (r *PreferAddUnpackRule) DefaultConfig() any {
	return DefaultPreferAddUnpackConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferAddUnpackRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// downloadCommands lists commands that download remote files.
var downloadCommands = []string{"curl", "wget"}

// archiveExtractionCommands lists commands that extract archive files
// (excluding tar, which needs separate flag checking).
var archiveExtractionCommands = []string{
	"unzip", "gunzip", "bunzip2", "unlzma", "unxz",
	"zgz", "uncompress", "zcat", "gzcat",
}

// addUnpackArchiveExtensions lists file extensions recognized by ADD --unpack.
// These match common compressed tarball and archive formats.
var addUnpackArchiveExtensions = []string{
	".tar",
	".tar.gz", ".tgz",
	".tar.bz2", ".tbz2", ".tbz",
	".tar.xz", ".txz",
	".tar.lz", ".tlz",
	".tar.lzma",
	".tar.Z", ".tZ",
	".tar.zst", ".tzst",
	".gz",
	".bz2",
	".xz",
	".lz",
	".lzma",
	".Z",
}

// Check runs the prefer-add-unpack rule.
func (r *PreferAddUnpackRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	if cfg.Enabled != nil && !*cfg.Enabled {
		return nil
	}

	meta := r.Metadata()

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				if shellVariant.IsNonPOSIX() {
					continue
				}
			}
		}

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			cmdStr := GetRunCommandString(run)
			if hasRemoteArchiveExtraction(cmdStr, shellVariant) {
				loc := rules.NewLocationFromRanges(input.File, run.Location())
				v := rules.NewViolation(
					loc, meta.Code,
					"use `ADD --unpack <url> <dest>` instead of downloading and extracting in `RUN`",
					meta.DefaultSeverity,
				).WithDetail(
					"Instead of using curl/wget to download an archive and extracting it in a `RUN` command, "+
						"use `ADD --unpack <url> <dest>` which downloads and extracts in a single layer. "+
						"This reduces image size and build complexity. Requires BuildKit.",
				)

				if fix := buildAddUnpackFix(input.File, run, cmdStr, shellVariant, meta); fix != nil {
					v = v.WithSuggestedFix(fix)
				}

				violations = append(violations, v)
			}
		}
	}

	return violations
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *PreferAddUnpackRule) resolveConfig(config any) PreferAddUnpackConfig {
	return configutil.Coerce(config, DefaultPreferAddUnpackConfig())
}

// hasRemoteArchiveExtraction checks if a shell command downloads a remote archive
// and extracts it, which could be replaced with ADD --unpack.
// Detects patterns like:
//   - curl -fsSL https://example.com/app.tar.gz | tar -xz
//   - wget -qO- https://example.com/app.tar.gz | tar -xz
//   - curl -o /tmp/app.tar.gz https://example.com/app.tar.gz && tar -xf /tmp/app.tar.gz
func hasRemoteArchiveExtraction(cmdStr string, variant shell.Variant) bool {
	dlCmds := shell.FindCommands(cmdStr, variant, downloadCommands...)
	if len(dlCmds) == 0 {
		return false
	}

	// Check if any download command has a URL argument with an archive extension
	if !hasArchiveURLArg(dlCmds) {
		return false
	}

	// Check if the same RUN contains an extraction command
	tarCmds := shell.FindCommands(cmdStr, variant, "tar")
	for i := range tarCmds {
		if isTarExtract(&tarCmds[i]) {
			return true
		}
	}

	return len(shell.FindCommands(cmdStr, variant, archiveExtractionCommands...)) > 0
}

// hasArchiveURLArg checks if any download command has a URL pointing to an archive.
func hasArchiveURLArg(dlCmds []shell.CommandInfo) bool {
	return slices.ContainsFunc(dlCmds, func(dl shell.CommandInfo) bool {
		return slices.ContainsFunc(dl.Args, isArchiveURL)
	})
}

// isArchiveURL checks if a string looks like a URL pointing to an archive file.
func isArchiveURL(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") &&
		!strings.HasPrefix(s, "ftp://") {
		return false
	}
	// Strip query string and fragment before checking extension
	u := s
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}
	return isRemoteArchive(path.Base(u))
}

// isRemoteArchive checks if a filename has an archive extension recognized by ADD --unpack.
func isRemoteArchive(name string) bool {
	for _, ext := range addUnpackArchiveExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// isTarExtract checks if a tar command has extraction flags.
func isTarExtract(cmd *shell.CommandInfo) bool {
	for _, arg := range cmd.Args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "--extract" || arg == "--get" {
			return true
		}
		if !strings.HasPrefix(arg, "--") && strings.Contains(arg, "x") {
			return true
		}
	}
	return false
}

// GetRunCommandString extracts the command string from a RUN instruction.
// Re-exported here for use within the tally package.
func GetRunCommandString(run *instructions.RunCommand) string {
	return strings.Join(run.CmdLine, " ")
}

// allowedFixCommands is the set of command names that are allowed in a "simple"
// download+extract RUN instruction eligible for auto-fix.
var allowedFixCommands = map[string]bool{
	"curl": true, "wget": true, // download
	"tar": true, // archive extraction
	"unzip": true, "gunzip": true, "bunzip2": true, "unlzma": true, "unxz": true,
	"zgz": true, "uncompress": true, "zcat": true, "gzcat": true, // other extraction
}

// buildAddUnpackFix creates a SuggestedFix for a RUN instruction that downloads
// and extracts an archive, replacing it with ADD --unpack. Returns nil if the
// RUN contains commands beyond download+extract (not a simple case).
func buildAddUnpackFix(
	file string,
	run *instructions.RunCommand,
	cmdStr string,
	variant shell.Variant,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	url, dest, ok := extractFixData(cmdStr, variant)
	if !ok {
		return nil
	}

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	lastRange := runLoc[len(runLoc)-1]
	endLine := lastRange.End.Line
	endCol := lastRange.End.Character

	// Fallback: if start == end, estimate end from instruction text
	if endLine == runLoc[0].Start.Line && endCol == runLoc[0].Start.Character {
		fullInstr := "RUN " + cmdStr
		endCol = runLoc[0].Start.Character + len(fullInstr)
	}

	return &rules.SuggestedFix{
		Description: "Replace with ADD --unpack " + url + " " + dest,
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(
				file,
				runLoc[0].Start.Line,
				runLoc[0].Start.Character,
				endLine,
				endCol,
			),
			NewText: "ADD --unpack " + url + " " + dest,
		}},
	}
}

// extractFixData checks if a RUN command is a simple download+extract and
// extracts the archive URL and destination directory.
// Returns ("", "", false) if the command contains non-download/extract commands.
func extractFixData(cmdStr string, variant shell.Variant) (string, string, bool) {
	// Check that ALL commands in the script are download or extraction commands
	allNames := shell.CommandNamesWithVariant(cmdStr, variant)
	for _, name := range allNames {
		if !allowedFixCommands[name] {
			return "", "", false
		}
	}

	// Extract the archive URL from download commands
	var archiveURL string
	dlCmds := shell.FindCommands(cmdStr, variant, downloadCommands...)
	for _, dl := range dlCmds {
		for _, arg := range dl.Args {
			if isArchiveURL(arg) {
				archiveURL = arg
				break
			}
		}
		if archiveURL != "" {
			break
		}
	}
	if archiveURL == "" {
		return "", "", false
	}

	// Extract destination from tar -C / --directory flag (default: /)
	dest := "/"
	tarCmds := shell.FindCommands(cmdStr, variant, "tar")
	for i := range tarCmds {
		if d := extractTarDestination(&tarCmds[i]); d != "" {
			dest = d
			break
		}
	}

	return archiveURL, dest, true
}

// extractTarDestination extracts the target directory from a tar command.
// Checks -C <dir>, --directory=<dir>, and --directory <dir>.
func extractTarDestination(cmd *shell.CommandInfo) string {
	for i, arg := range cmd.Args {
		// --directory=<value>
		if after, found := strings.CutPrefix(arg, "--directory="); found {
			return after
		}
		// --directory <value>
		if arg == "--directory" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
		// -C <value> (short flag — must not be a long flag)
		if arg == "-C" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}
	return ""
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferAddUnpackRule())
}
