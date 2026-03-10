package tally

import (
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// PreferAddUnpackRuleCode is the full rule code for the prefer-add-unpack rule.
const PreferAddUnpackRuleCode = rules.TallyRulePrefix + "prefer-add-unpack"

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
type PreferAddUnpackRule struct {
	schema map[string]any
}

// NewPreferAddUnpackRule creates a new rule instance.
func NewPreferAddUnpackRule() *PreferAddUnpackRule {
	schema, err := configutil.RuleSchema(PreferAddUnpackRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferAddUnpackRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferAddUnpackRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferAddUnpackRuleCode,
		Name:            "Prefer ADD --unpack for remote archives",
		Description:     "Use `ADD --unpack` instead of downloading and extracting remote archives in `RUN`",
		DocURL:          rules.TallyDocURL(PreferAddUnpackRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		IsExperimental:  false,
		FixPriority:     95,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferAddUnpackRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration.
func (r *PreferAddUnpackRule) DefaultConfig() any {
	return DefaultPreferAddUnpackConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferAddUnpackRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(PreferAddUnpackRuleCode, config)
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
	if variant == shell.VariantCmd {
		cmds, ok := parseNonPOSIXCommands(cmdStr)
		if !ok {
			return false
		}
		return hasRemoteArchiveExtractionNonPOSIX(cmds)
	}
	if !variant.IsPowerShell() && !variant.IsParseable() {
		return false
	}

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
		if url := shell.DownloadURL(&dl); url != "" && shell.IsArchiveURL(url) {
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
	"invoke-webrequest": true, "iwr": true, // PowerShell download
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
	if variant == shell.VariantCmd {
		cmds, ok := parseNonPOSIXCommands(cmdStr)
		if !ok {
			return "", "", false
		}
		return extractFixDataNonPOSIX(cmds, workdir)
	}
	if !variant.IsPowerShell() && !variant.IsParseable() {
		return "", "", false
	}

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

type nonPOSIXCommandInfo struct {
	Name string
	Args []string
}

// parseNonPOSIXCommands tokenizes simple cmd.exe RUN bodies into commands
// separated by '&' or '&&'. It intentionally rejects pipelines because cmd
// piping semantics differ from the POSIX stream model this rule handles via
// shell parsing.
func parseNonPOSIXCommands(script string) ([]nonPOSIXCommandInfo, bool) {
	var (
		token    strings.Builder
		tokens   []string
		commands []nonPOSIXCommandInfo
		quote    rune
	)

	flushToken := func() {
		if token.Len() == 0 {
			return
		}
		tokens = append(tokens, token.String())
		token.Reset()
	}
	flushCommand := func() {
		flushToken()
		if len(tokens) == 0 {
			return
		}
		name := normalizeNonPOSIXCommandName(tokens[0])
		if name == "" {
			tokens = tokens[:0]
			return
		}
		commands = append(commands, nonPOSIXCommandInfo{
			Name: name,
			Args: append([]string(nil), tokens[1:]...),
		})
		tokens = tokens[:0]
	}

	for i := 0; i < len(script); i++ {
		ch := rune(script[i])
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			token.WriteRune(ch)
			continue
		}

		switch ch {
		case '"':
			quote = ch
		case ' ', '\t', '\r', '\n':
			flushToken()
		case '|':
			return nil, false
		case '&':
			flushCommand()
			if i+1 < len(script) && script[i+1] == '&' {
				i++
			}
		default:
			token.WriteRune(ch)
		}
	}
	if quote != 0 {
		return nil, false
	}
	flushCommand()
	return commands, len(commands) > 0
}

func normalizeNonPOSIXCommandName(name string) string {
	name = strings.ToLower(path.Base(strings.ReplaceAll(dropCmdQuotes(name), `\`, "/")))
	return strings.TrimSuffix(name, ".exe")
}

func hasRemoteArchiveExtractionNonPOSIX(cmds []nonPOSIXCommandInfo) bool {
	dlCmds := findNonPOSIXDownloadCommands(cmds)
	if len(dlCmds) == 0 || !hasArchiveDownloadNonPOSIX(dlCmds) {
		return false
	}
	return findSingleExtractTarNonPOSIX(cmds) != nil
}

func extractFixDataNonPOSIX(cmds []nonPOSIXCommandInfo, workdir string) (string, string, bool) {
	for _, cmd := range cmds {
		if !allowedFixCommands[cmd.Name] {
			return "", "", false
		}
	}

	dlCmds := findNonPOSIXDownloadCommands(cmds)
	archiveURL := findArchiveURLNonPOSIX(dlCmds)
	if archiveURL == "" {
		return "", "", false
	}

	outFile := findDownloadOutputFileNonPOSIX(dlCmds)
	extractTar := findSingleExtractTarNonPOSIX(cmds)
	if extractTar == nil || hasTarSemanticFlags(extractTar) {
		return "", "", false
	}

	if outFile != "" &&
		!slices.Contains(extractTar.Args, outFile) &&
		!slices.Contains(extractTar.Args, shell.Basename(outFile)) {
		return "", "", false
	}

	dest := workdir
	if d := shell.TarDestination(extractTar); d != "" {
		dest = d
	}

	return archiveURL, dest, true
}

func findNonPOSIXDownloadCommands(cmds []nonPOSIXCommandInfo) []nonPOSIXCommandInfo {
	return slices.DeleteFunc(append([]nonPOSIXCommandInfo(nil), cmds...), func(cmd nonPOSIXCommandInfo) bool {
		return cmd.Name != "curl" && cmd.Name != "wget"
	})
}

func hasArchiveDownloadNonPOSIX(dlCmds []nonPOSIXCommandInfo) bool {
	return slices.ContainsFunc(dlCmds, func(dl nonPOSIXCommandInfo) bool {
		if url := nonPOSIXDownloadURL(dl, true); url != "" {
			return true
		}
		if outFile := nonPOSIXDownloadOutputFile(dl); outFile != "" {
			return shell.IsArchiveFilename(cmdBasename(outFile)) && nonPOSIXDownloadURL(dl, false) != ""
		}
		return false
	})
}

func findArchiveURLNonPOSIX(dlCmds []nonPOSIXCommandInfo) string {
	var archiveURL string
	for _, dl := range dlCmds {
		if url := nonPOSIXDownloadURL(dl, true); url != "" {
			if archiveURL != "" && url != archiveURL {
				return ""
			}
			archiveURL = url
		}
	}
	if archiveURL != "" {
		return archiveURL
	}

	for _, dl := range dlCmds {
		outFile := nonPOSIXDownloadOutputFile(dl)
		if outFile == "" || !shell.IsArchiveFilename(cmdBasename(outFile)) {
			continue
		}
		if url := nonPOSIXDownloadURL(dl, false); url != "" {
			if archiveURL != "" && url != archiveURL {
				return ""
			}
			archiveURL = url
		}
	}
	return archiveURL
}

func findDownloadOutputFileNonPOSIX(dlCmds []nonPOSIXCommandInfo) string {
	var outFile string
	for _, dl := range dlCmds {
		if f := nonPOSIXDownloadOutputFile(dl); f != "" {
			if outFile != "" && f != outFile {
				return ""
			}
			outFile = f
		}
	}
	return outFile
}

func findSingleExtractTarNonPOSIX(cmds []nonPOSIXCommandInfo) *shell.CommandInfo {
	var extractTar *shell.CommandInfo
	for _, cmd := range cmds {
		if cmd.Name != "tar" {
			continue
		}
		tarCmd := shell.CommandInfo{Name: "tar", Args: append([]string(nil), cmd.Args...)}
		if !shell.IsTarExtract(&tarCmd) {
			continue
		}
		if extractTar != nil {
			return nil
		}
		extractTar = &tarCmd
	}
	return extractTar
}

func nonPOSIXDownloadOutputFile(cmd nonPOSIXCommandInfo) string {
	var short, long string
	switch cmd.Name {
	case "curl":
		short, long = "-o", "--output"
	case "wget":
		short, long = "-O", "--output-document"
	default:
		return ""
	}
	for i, arg := range cmd.Args {
		if after, found := strings.CutPrefix(arg, long+"="); found {
			if after == "-" {
				return ""
			}
			return after
		}
		if after, found := strings.CutPrefix(arg, short); found && after != "" {
			if after == "-" {
				return ""
			}
			return after
		}
		if (arg == short || arg == long) && i+1 < len(cmd.Args) {
			val := cmd.Args[i+1]
			if val == "-" {
				return ""
			}
			return val
		}
	}
	return ""
}

func nonPOSIXDownloadURL(cmd nonPOSIXCommandInfo, archiveOnly bool) string {
	url := ""
	if i := slices.IndexFunc(cmd.Args, func(arg string) bool { return shell.IsURL(dropCmdQuotes(arg)) }); i >= 0 {
		url = dropCmdQuotes(cmd.Args[i])
	}
	if !archiveOnly || shell.IsArchiveURL(url) {
		return url
	}
	return ""
}

func dropCmdQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func cmdBasename(p string) string {
	p = dropCmdQuotes(p)
	if i := strings.LastIndexByte(p, '\\'); i >= 0 {
		p = p[i+1:]
	}
	return path.Base(p)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferAddUnpackRule())
}
