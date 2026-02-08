package tally

import (
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
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
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/tally/prefer-add-unpack.md",
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

		// Track the effective WORKDIR as we walk through the stage.
		// Docker default is "/" when no WORKDIR is set.
		workdir := "/"

		for _, cmd := range stage.Commands {
			if wd, ok := cmd.(*instructions.WorkdirCommand); ok {
				if path.IsAbs(wd.Path) {
					workdir = path.Clean(wd.Path)
				} else {
					workdir = path.Clean(path.Join(workdir, wd.Path))
				}
				continue
			}

			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			cmdStr := dockerfile.RunCommandString(run)
			if hasRemoteArchiveExtraction(cmdStr, shellVariant) {
				loc := rules.NewLocationFromRanges(input.File, run.Location())
				v := rules.NewViolation(
					loc, meta.Code,
					"use `ADD --unpack <url> <dest>` instead of downloading and extracting in `RUN`",
					meta.DefaultSeverity,
				).WithDetail(
					"Instead of using curl/wget to download an archive and extracting it in a `RUN` command, " +
						"use `ADD --unpack <url> <dest>` which downloads and extracts in a single layer. " +
						"This reduces image size and build complexity. Requires BuildKit.",
				)

				if fix := buildAddUnpackFix(input.File, run, cmdStr, shellVariant, meta, workdir); fix != nil {
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
// and extracts it with tar, which can be replaced with ADD --unpack.
// Only tar-based extractions are detected since ADD --unpack does not handle
// single-file decompressors (gunzip, bunzip2, etc.).
func hasRemoteArchiveExtraction(cmdStr string, variant shell.Variant) bool {
	dlCmds := shell.FindCommands(cmdStr, variant, shell.DownloadCommands...)
	if len(dlCmds) == 0 {
		return false
	}

	// Check if any download command has a URL argument with an archive extension
	if !hasArchiveURLArg(dlCmds) {
		return false
	}

	// Only detect tar extraction — ADD --unpack only handles tar archives
	tarCmds := shell.FindCommands(cmdStr, variant, "tar")
	for i := range tarCmds {
		if shell.IsTarExtract(&tarCmds[i]) {
			return true
		}
	}

	return false
}

// hasArchiveURLArg checks if any download command is downloading an archive.
// An archive download is identified by either:
//   - A URL argument with a recognized archive extension, or
//   - An output filename (-o/-O) with a recognized archive extension.
func hasArchiveURLArg(dlCmds []shell.CommandInfo) bool {
	return slices.ContainsFunc(dlCmds, func(dl shell.CommandInfo) bool {
		if slices.ContainsFunc(dl.Args, shell.IsArchiveURL) {
			return true
		}
		if outFile := shell.DownloadOutputFile(&dl); outFile != "" {
			return shell.IsArchiveFilename(shell.Basename(outFile))
		}
		return false
	})
}

// allowedFixCommands is the set of command names that are allowed in a "simple"
// download+extract RUN instruction eligible for auto-fix. Only commands whose
// semantics are fully captured by ADD --unpack are included; other extractors
// (gunzip, unzip, etc.) would be silently dropped by the fix.
var allowedFixCommands = map[string]bool{
	"curl": true, "wget": true, // download
	"tar": true, // archive extraction (the only extractor ADD --unpack replaces)
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
	workdir string,
) *rules.SuggestedFix {
	url, dest, ok := extractFixData(cmdStr, variant, workdir)
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
// When tar has no explicit -C/--directory, workdir (the effective WORKDIR
// from the Dockerfile) is used as the extraction destination.
// Returns ("", "", false) if the command contains non-download/extract commands.
func extractFixData(cmdStr string, variant shell.Variant, workdir string) (string, string, bool) {
	// Check that ALL commands in the script are download or extraction commands
	for _, name := range shell.CommandNamesWithVariant(cmdStr, variant) {
		if !allowedFixCommands[name] {
			return "", "", false
		}
	}

	dlCmds := shell.FindCommands(cmdStr, variant, shell.DownloadCommands...)
	archiveURL := findArchiveURL(dlCmds)
	if archiveURL == "" {
		return "", "", false
	}

	outFile := findDownloadOutputFile(dlCmds)
	extractTar := findSingleExtractTar(cmdStr, variant)
	if extractTar == nil {
		return "", "", false
	}

	// Bail out if tar has flags that alter extraction semantics in ways
	// ADD --unpack cannot replicate (e.g. --strip-components, --transform).
	if hasTarSemanticFlags(extractTar) {
		return "", "", false
	}

	// When a download output file is present, verify the tar command
	// references it (by full path or basename) to avoid matching a tar
	// that operates on an unrelated file.
	if outFile != "" &&
		!slices.Contains(extractTar.Args, outFile) &&
		!slices.Contains(extractTar.Args, shell.Basename(outFile)) {
		return "", "", false
	}

	// Default to the effective WORKDIR; tar without -C extracts into cwd.
	dest := workdir
	if d := shell.TarDestination(extractTar); d != "" {
		dest = d
	}

	return archiveURL, dest, true
}

// findArchiveURL finds a single archive URL from download commands.
// Returns "" if none found or if multiple distinct archive URLs are present.
// Checks URL arguments first, then falls back to output filenames.
func findArchiveURL(dlCmds []shell.CommandInfo) string {
	var archiveURL string
	for _, dl := range dlCmds {
		for _, arg := range dl.Args {
			if shell.IsArchiveURL(arg) {
				if archiveURL != "" && arg != archiveURL {
					return "" // multiple distinct archive URLs
				}
				archiveURL = arg
			}
		}
	}
	// Fall back to output filenames: e.g. curl https://example.com/latest -o app.tar.gz
	if archiveURL == "" {
		for i := range dlCmds {
			outFile := shell.DownloadOutputFile(&dlCmds[i])
			if outFile == "" || !shell.IsArchiveFilename(shell.Basename(outFile)) {
				continue
			}
			if url := shell.DownloadURL(&dlCmds[i]); url != "" {
				if archiveURL != "" && url != archiveURL {
					return ""
				}
				archiveURL = url
			}
		}
	}
	return archiveURL
}

// findDownloadOutputFile returns the single output filename from download
// commands. Returns "" if no output file is specified or if multiple distinct
// output files are present.
func findDownloadOutputFile(dlCmds []shell.CommandInfo) string {
	var outFile string
	for i := range dlCmds {
		if f := shell.DownloadOutputFile(&dlCmds[i]); f != "" {
			if outFile != "" && f != outFile {
				return ""
			}
			outFile = f
		}
	}
	return outFile
}

// hasTarSemanticFlags checks if a tar command has flags that alter extraction
// semantics in ways ADD --unpack cannot replicate.
func hasTarSemanticFlags(cmd *shell.CommandInfo) bool {
	return slices.ContainsFunc(cmd.Args, func(arg string) bool {
		return strings.HasPrefix(arg, "--strip-components") ||
			strings.HasPrefix(arg, "--strip=") ||
			strings.HasPrefix(arg, "--transform") ||
			strings.HasPrefix(arg, "--xform") ||
			strings.HasPrefix(arg, "--exclude") ||
			strings.HasPrefix(arg, "--include") ||
			strings.HasPrefix(arg, "--wildcards")
	})
}

// findSingleExtractTar finds exactly one tar extraction command.
// Returns nil if there are zero or multiple extract tars.
func findSingleExtractTar(cmdStr string, variant shell.Variant) *shell.CommandInfo {
	tarCmds := shell.FindCommands(cmdStr, variant, "tar")
	var extractTar *shell.CommandInfo
	for i := range tarCmds {
		if shell.IsTarExtract(&tarCmds[i]) {
			if extractTar != nil {
				return nil // multiple extract tars — ambiguous
			}
			extractTar = &tarCmds[i]
		}
	}
	return extractTar
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferAddUnpackRule())
}
