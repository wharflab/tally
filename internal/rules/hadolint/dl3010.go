package hadolint

import (
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL3010Rule implements the DL3010 linting rule.
// It flags COPY instructions that copy archive files which are subsequently
// extracted in a RUN instruction, suggesting ADD instead (which handles
// extraction automatically).
type DL3010Rule struct{}

// NewDL3010Rule creates a new DL3010 rule instance.
func NewDL3010Rule() *DL3010Rule {
	return &DL3010Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3010Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3010",
		Name:            "Use ADD for extracting archives into an image",
		Description:     "Use `ADD` for extracting archives into an image instead of `COPY` + `RUN tar/unzip`",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3010",
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		IsExperimental:  false,
	}
}

// archiveExtensions lists file extensions that indicate an archive file.
// Matches hadolint's archiveFileFormatExtensions.
var archiveExtensions = []string{
	".tar",
	".Z",
	".bz2",
	".gz",
	".lz",
	".lzma",
	".tZ",
	".tb2",
	".tbz",
	".tbz2",
	".tgz",
	".tlz",
	".tpz",
	".txz",
	".xz",
}

// extractionCommands lists commands that extract archive files.
var extractionCommands = []string{
	"unzip",
	"gunzip",
	"bunzip2",
	"unlzma",
	"unxz",
	"zgz",
	"uncompress",
	"zcat",
	"gzcat",
}

// copiedArchive tracks an archive file copied via COPY instruction.
type copiedArchive struct {
	line     int    // line of the COPY instruction
	basename string // basename of the archive file (without path, without quotes)
}

// Check runs the DL3010 rule.
// For each stage, it tracks COPY instructions that copy archive files (without --from),
// then checks if subsequent RUN instructions extract those archives. If so, the COPY
// instruction is flagged because ADD could handle extraction automatically.
func (r *DL3010Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	// Get semantic model for shell variant info
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	for stageIdx, stage := range input.Stages {
		// Get shell variant for this stage
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
			}
		}

		// Track archives copied in this stage
		var archives []copiedArchive

		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.CopyCommand:
				archives = collectCopiedArchives(c, archives)

			case *instructions.RunCommand:
				if len(archives) == 0 || shellVariant.IsNonPOSIX() {
					continue
				}

				cmdStr := GetRunCommandString(c)
				for _, arch := range findExtractedArchives(archives, cmdStr, shellVariant) {
					loc := rules.NewLineLocation(input.File, arch.line)
					violations = append(violations, rules.NewViolation(
						loc, meta.Code,
						"use `ADD` for extracting archives into an image",
						meta.DefaultSeverity,
					).WithDocURL(meta.DocURL).WithDetail(
						"When copying an archive that is immediately extracted, use `ADD` instead of `COPY` + `RUN tar/unzip`. "+
							"`ADD` automatically extracts recognized archive formats during the build.",
					))
				}
			}
		}
	}

	return violations
}

// collectCopiedArchives extracts archive file references from a COPY instruction.
// Only processes COPY from build context (no --from flag).
func collectCopiedArchives(c *instructions.CopyCommand, archives []copiedArchive) []copiedArchive {
	if c.From != "" {
		return archives
	}

	copyLine := 0
	if locs := c.Location(); len(locs) > 0 {
		copyLine = locs[0].Start.Line
	}

	// Check if the target path itself is an archive
	// (e.g., COPY foo.tar /foo.tar → target basename is "foo.tar")
	destBase := basename(c.DestPath)
	if isArchive(destBase) {
		return append(archives, copiedArchive{line: copyLine, basename: destBase})
	}

	// Check source paths for archive files
	for _, src := range c.SourcePaths {
		srcBase := basename(src)
		if isArchive(srcBase) {
			archives = append(archives, copiedArchive{line: copyLine, basename: srcBase})
		}
	}
	return archives
}

// findExtractedArchives returns the subset of archives that are extracted by the given shell command.
func findExtractedArchives(archives []copiedArchive, cmdStr string, variant shell.Variant) []copiedArchive {
	// Find tar commands with extract flags
	tarCmds := shell.FindCommands(cmdStr, variant, "tar")
	// Find unzip-like commands
	unzipCmds := shell.FindCommands(cmdStr, variant, extractionCommands...)

	if len(tarCmds) == 0 && len(unzipCmds) == 0 {
		return nil
	}

	// Collect all arguments (without flags) from extraction commands
	var extractionArgs []string

	for _, tc := range tarCmds {
		if !isTarExtractCommand(&tc) {
			continue
		}
		// Collect non-flag arguments as potential archive filenames
		for _, arg := range tc.Args {
			if !strings.HasPrefix(arg, "-") {
				extractionArgs = append(extractionArgs, basename(arg))
			}
		}
	}

	for _, uc := range unzipCmds {
		// All non-flag arguments could be archive filenames
		for _, arg := range uc.Args {
			if !strings.HasPrefix(arg, "-") {
				extractionArgs = append(extractionArgs, basename(arg))
			}
		}
	}

	// Check which copied archives appear in extraction arguments
	var matched []copiedArchive
	seen := make(map[int]bool) // deduplicate by COPY line
	for _, arch := range archives {
		if seen[arch.line] {
			continue
		}
		if slices.Contains(extractionArgs, arch.basename) {
			matched = append(matched, arch)
			seen[arch.line] = true
		}
	}

	return matched
}

// isTarExtractCommand checks if a tar command has extraction flags.
func isTarExtractCommand(cmd *shell.CommandInfo) bool {
	for _, arg := range cmd.Args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		// Long flags
		if arg == "--extract" || arg == "--get" {
			return true
		}
		// Short flags: any flag starting with - that contains 'x'
		if !strings.HasPrefix(arg, "--") && strings.Contains(arg, "x") {
			return true
		}
	}
	return false
}

// basename extracts the filename from a path, handling both Unix and Windows separators.
// Also strips surrounding quotes.
func basename(p string) string {
	p = dropQuotes(p)
	// Handle Windows backslash paths
	if i := strings.LastIndexByte(p, '\\'); i >= 0 {
		p = p[i+1:]
	}
	return path.Base(p)
}

// dropQuotes removes surrounding single or double quotes from a string.
func dropQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// isArchive checks if a filename has an archive extension.
// Extensions are case-sensitive to match hadolint behavior
// (e.g., .Z and .tZ use uppercase Z for Unix compress format).
func isArchive(name string) bool {
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3010Rule())
}
