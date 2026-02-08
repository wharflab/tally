package hadolint

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
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

				cmdStr := dockerfile.RunCommandString(c)
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
	destBase := shell.Basename(c.DestPath)
	if shell.IsArchiveFilename(destBase) {
		return append(archives, copiedArchive{line: copyLine, basename: destBase})
	}

	// Check source paths for archive files
	for _, src := range c.SourcePaths {
		srcBase := shell.Basename(src)
		if shell.IsArchiveFilename(srcBase) {
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
	unzipCmds := shell.FindCommands(cmdStr, variant, shell.ExtractionCommands...)

	if len(tarCmds) == 0 && len(unzipCmds) == 0 {
		return nil
	}

	// Collect all arguments (without flags) from extraction commands
	var extractionArgs []string

	for _, tc := range tarCmds {
		if !shell.IsTarExtract(&tc) {
			continue
		}
		// Collect non-flag arguments as potential archive filenames
		for _, arg := range tc.Args {
			if !strings.HasPrefix(arg, "-") {
				extractionArgs = append(extractionArgs, shell.Basename(arg))
			}
		}
	}

	for _, uc := range unzipCmds {
		// All non-flag arguments could be archive filenames
		for _, arg := range uc.Args {
			if !strings.HasPrefix(arg, "-") {
				extractionArgs = append(extractionArgs, shell.Basename(arg))
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

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3010Rule())
}
