package shell

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// PackageArg represents a single package argument with its position in the source text.
type PackageArg struct {
	// Value is the raw source token text (including any quotes), used for
	// round-trip safe edits. The edit span (StartCol..EndCol) covers exactly
	// these bytes.
	Value string
	// Normalized is the unquoted/decoded text used for sorting and comparison.
	Normalized string
	Line       int  // 0-based line within the source text
	StartCol   int  // 0-based start column (byte offset)
	EndCol     int  // 0-based end column (byte offset, exclusive)
	IsVar      bool // true if the argument contains a variable reference ($)
}

// InstallCommand represents a detected package install command with per-argument positions.
type InstallCommand struct {
	Manager    string       // e.g., "apt-get", "npm", "pip"
	Subcommand string       // e.g., "install", "add", "require"
	Packages   []PackageArg // non-flag args after subcommand, with positions
}

// StripPackageVersion returns the package name with any trailing version
// specifier removed. Handles the forms used by the package-manager syntaxes
// tally understands:
//
//   - apt/apk/dnf/yum/zypper: "name=1.2"
//   - pip: "name==1.2"
//   - npm/yarn/pnpm/bun: "name@1.2" — a leading "@" is a scope
//     marker (e.g. "@types/node"), so the version separator is the first
//     "@" past index 0, not the first "@" overall
//   - Debian arch qualifier / rpm epoch: "libfoo:amd64", "foo:1-2"
//
// The caller is expected to pass an already-normalized (unquoted) token.
func StripPackageVersion(name string) string {
	// "==" before "=" so "pkg==1" strips to "pkg" instead of "pkg=" with
	// a trailing "=1" left behind.
	if before, _, ok := strings.Cut(name, "=="); ok {
		return before
	}
	if before, _, ok := strings.Cut(name, "="); ok {
		return before
	}
	// Scoped npm package: leading "@" belongs to the name. Only an "@"
	// past the first character counts as a version separator.
	if idx := strings.Index(name[min(len(name), 1):], "@"); idx >= 0 {
		return name[:idx+min(len(name), 1)]
	}
	if before, _, ok := strings.Cut(name, ":"); ok {
		return before
	}
	return name
}

// installManagerInfo describes how to parse a package manager command for sorting.
type installManagerInfo struct {
	installCommands []string
	// flagsWithValue lists flags that consume the next argument as a value.
	// Long flags with "=" are always self-contained and handled separately.
	flagsWithValue []string
}

// Per-manager flag lists for flags that consume the next token.
var (
	aptFlags    = []string{"-o", "--option", "-t", "--target-release"}
	dnfYumFlags = []string{"--root", "--installroot", "--releasever", "--repo"}
	pipFlags    = []string{
		"-c",
		"--constraint",
		"--trusted-host",
		"--index-url",
		"--extra-index-url",
		"--find-links",
		"--prefix",
		"--target",
		"--progress-bar",
		"--root-user-action",
		"-i",
		"--global-option",
		"--config-settings",
	}
	npmFlags      = []string{"--prefix", "--registry", "--save-prefix"}
	composerFlags = []string{"--working-dir", "-d"}
	chocoFlags    = []string{
		"--source",
		"-s",
		"--params",
		"--package-parameters",
		"--installargs",
		"--install-arguments",
		"--version",
		"-version",
	}
	uvFlags = []string{
		// uv add
		"-r", "--requirements", "-c", "--constraints", "-m", "--marker",
		"--optional", "--group", "--bounds", "--rev", "--tag", "--branch",
		"--extra", "--package", "--script", "--no-install-package",
		// uv pip install (superset of uv add)
		"-e", "--editable", "-b", "--build-constraints",
		"--overrides", "--excludes",
		"-t", "--target", "--prefix",
		"--no-binary", "--only-binary", "--no-build-package", "--no-binary-package",
		"--python-version", "--python-platform", "--torch-backend",
		// shared: index, resolver, installer, build, cache, python, global
		"--index", "--default-index", "-i", "--index-url",
		"--extra-index-url", "-f", "--find-links",
		"--index-strategy", "--keyring-provider",
		"-P", "--upgrade-package", "--resolution", "--prerelease",
		"--fork-strategy", "--exclude-newer", "--exclude-newer-package",
		"--no-sources-package",
		"--reinstall-package", "--link-mode",
		"-C", "--config-setting", "--config-settings-package",
		"--no-build-isolation-package",
		"--cache-dir", "--refresh-package",
		"-p", "--python", "--color",
		"--allow-insecure-host", "--directory", "--project", "--config-file",
	}
	// condaFlags covers conda/mamba/micromamba install flags that consume the
	// next argument as a value. Long --flag=value forms are self-contained and
	// do not need to be listed. Short -cXXX combined short-flag aggregation is
	// not used by conda (flags are always separated).
	//
	// Note: micromamba's install does not implement --revision, --solver, or
	// --repodata-fn, but listing them here is safe because micromamba invocations
	// never reach those flag names.
	condaFlags = []string{
		"-c", "--channel",
		"-n", "--name",
		"-p", "--prefix",
		"-f", "--file",
		"--repodata-fn",
		"--revision",
		"--subdir",
		"--solver",
	}
)

// installManagers maps command names to their install subcommands and flags.
var installManagers = map[string]installManagerInfo{
	"apt-get":  {installCommands: []string{"install"}, flagsWithValue: aptFlags},
	"apt":      {installCommands: []string{"install"}, flagsWithValue: aptFlags},
	"apk":      {installCommands: []string{"add"}},
	"dnf":      {installCommands: []string{"install"}, flagsWithValue: dnfYumFlags},
	"microdnf": {installCommands: []string{"install"}, flagsWithValue: dnfYumFlags},
	"yum":      {installCommands: []string{"install"}, flagsWithValue: dnfYumFlags},
	"zypper":   {installCommands: []string{"install", "in"}},
	"npm":      {installCommands: []string{"install", "i", "add"}, flagsWithValue: npmFlags},
	"yarn":     {installCommands: []string{"add"}},
	"pnpm":     {installCommands: []string{"add", "install", "i"}, flagsWithValue: npmFlags},
	"pip":      {installCommands: []string{"install"}, flagsWithValue: pipFlags},
	"pip3":     {installCommands: []string{"install"}, flagsWithValue: pipFlags},
	"bun":      {installCommands: []string{"add", "install", "i"}},
	"composer": {installCommands: []string{"require"}, flagsWithValue: composerFlags},
	"uv":       {installCommands: []string{"add", "pip install"}, flagsWithValue: uvFlags},
	"choco":    {installCommands: []string{"install"}, flagsWithValue: chocoFlags},
	// conda-family package managers used as Python package installers.
	// `conda env create`, `conda run`, etc. are not treated as package
	// installs; only the `install` subcommand is recognized here.
	"conda":      {installCommands: []string{"install"}, flagsWithValue: condaFlags},
	"mamba":      {installCommands: []string{"install"}, flagsWithValue: condaFlags},
	"micromamba": {installCommands: []string{"install"}, flagsWithValue: condaFlags},
}

// pipFileArgs are pip arguments that indicate file-based or local install (skip sorting).
var pipFileArgs = map[string]bool{
	"-r":            true,
	"--requirement": true,
	"-e":            true,
	"--editable":    true,
	".":             true,
	"./":            true,
}

// FindInstallPackages parses a shell script and extracts install commands with
// per-argument position information. Positions are 0-based line and column
// offsets within the source text.
//
// Dispatches to a variant-aware path: shells with tree-sitter grammars
// (PowerShell, cmd) are handled via FindCommands so their native tokenization
// and line-continuation rules are honored; POSIX variants go through the
// mvdan.cc/sh AST walker for full flag/wrapper fidelity.
func FindInstallPackages(script string, variant Variant) []InstallCommand {
	if variant.IsPowerShell() || variant == VariantCmd {
		return findInstallPackagesViaFindCommands(script, variant)
	}
	return findInstallPackagesViaPOSIX(script, variant)
}

// findInstallPackagesViaFindCommands builds install commands from FindCommands
// output — the only entry point that honors tree-sitter grammars for cmd and
// PowerShell. Relies on CommandInfo.Args / CommandInfo.ArgRanges so package
// positions map back to the original script correctly.
func findInstallPackagesViaFindCommands(script string, variant Variant) []InstallCommand {
	srcLines := strings.Split(script, "\n")
	managerNames := make([]string, 0, len(installManagers))
	for name := range installManagers {
		managerNames = append(managerNames, name)
	}
	var commands []InstallCommand
	for _, cmd := range FindCommands(script, variant, managerNames...) {
		mgr, found := installManagers[cmd.Name]
		if !found {
			continue
		}
		if ic := extractInstallFromCommandInfo(cmd, mgr, srcLines); ic != nil {
			commands = append(commands, *ic)
		}
	}
	return commands
}

func findInstallPackagesViaPOSIX(script string, variant Variant) []InstallCommand {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}

	// Split source into lines for raw token extraction.
	srcLines := strings.Split(script, "\n")

	var commands []InstallCommand

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		cmdWord := call.Args[0]
		cmdName := cmdWord.Lit()
		if cmdName == "" {
			return true
		}
		cmdName = path.Base(cmdName)

		mgr, found := installManagers[cmdName]
		if !found {
			// Check wrapped commands (env, nice, etc.)
			if commandWrappers[cmdName] {
				commands = append(commands,
					findWrappedInstallPackages(call.Args[1:], variant, cmdName, srcLines)...)
			}
			return true
		}

		cmd := extractInstallCommand(cmdName, mgr, call.Args[1:], srcLines)
		if cmd != nil {
			commands = append(commands, *cmd)
		}

		return true
	})

	return commands
}

// extractInstallFromCommandInfo builds an InstallCommand from a CommandInfo
// produced by FindCommands. Mirrors extractInstallCommand but works with the
// string/ArgRange representation instead of syntax.Word nodes.
func extractInstallFromCommandInfo(
	cmd CommandInfo,
	mgr installManagerInfo,
	srcLines []string,
) *InstallCommand {
	if len(cmd.ArgRanges) != len(cmd.Args) {
		// Parser didn't preserve arg ranges; falling back would produce
		// wrong-offset edits, so skip rather than guess.
		return nil
	}
	installIdx, subcommand := findInstallSubcommandInStrings(cmd.Args, mgr.installCommands)
	if installIdx < 0 {
		return nil
	}

	// Pip file-based installs: skip entirely so they aren't treated as
	// package lists.
	isPip := cmd.Name == "pip" || cmd.Name == "pip3" || strings.HasPrefix(subcommand, "pip")
	if isPip {
		for _, arg := range cmd.Args[installIdx+1:] {
			if arg == "" {
				continue
			}
			if pipFileArgs[arg] {
				return nil
			}
			if !strings.HasPrefix(arg, "-") && (strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "/")) {
				return nil
			}
		}
	}

	packages := make([]PackageArg, 0, len(cmd.Args)-installIdx)
	skipNext := false
	for i := installIdx + 1; i < len(cmd.Args); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		arg := cmd.Args[i]
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if flagConsumesValue(arg, mgr.flagsWithValue) {
				skipNext = true
			}
			continue
		}
		r := cmd.ArgRanges[i]
		raw := extractRawToken(srcLines, r.Line, r.StartCol, r.EndCol)
		packages = append(packages, PackageArg{
			Value:      raw,
			Normalized: arg,
			Line:       r.Line,
			StartCol:   r.StartCol,
			EndCol:     r.EndCol,
			IsVar:      strings.Contains(arg, "$"),
		})
	}

	if len(packages) == 0 {
		return nil
	}

	return &InstallCommand{
		Manager:    cmd.Name,
		Subcommand: subcommand,
		Packages:   packages,
	}
}

// findInstallSubcommandInStrings locates the install subcommand in a []string
// arg list. Compound subcommands (e.g. "pip install") match consecutive args.
// Returns the index of the last token of the matched subcommand (so packages
// start at index+1) and the full subcommand string.
func findInstallSubcommandInStrings(args, installCommands []string) (int, string) {
	bestStart := -1
	bestEndIdx := -1
	bestCmd := ""
	for _, cmd := range installCommands {
		parts := strings.Split(cmd, " ")
		for j := 0; j+len(parts) <= len(args); j++ {
			match := true
			for k, part := range parts {
				if args[j+k] != part {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if bestStart < 0 || j < bestStart {
				bestStart = j
				bestEndIdx = j + len(parts) - 1
				bestCmd = cmd
			}
			break
		}
	}
	return bestEndIdx, bestCmd
}

// extractInstallCommand extracts package arguments with positions from a call expression.
// srcLines are the source text lines used to extract raw token text for round-trip safe edits.
func extractInstallCommand(
	cmdName string, mgr installManagerInfo, args []*syntax.Word, srcLines []string,
) *InstallCommand {
	// Find the install subcommand. Supports compound subcommands like
	// "pip install" (space-separated) by matching consecutive tokens.
	installIdx, subcommand := findInstallSubcommand(args, mgr.installCommands)
	if installIdx < 0 {
		return nil
	}

	// Check for pip file-based installs (also covers "uv pip install")
	isPip := cmdName == "pip" || cmdName == "pip3" || strings.HasPrefix(subcommand, "pip")
	if isPip {
		for _, arg := range args[installIdx+1:] {
			lit := arg.Lit()
			if lit == "" {
				continue
			}
			if pipFileArgs[lit] {
				return nil // file-based install, skip
			}
			// Check for paths like ".", "./foo", "/path/to/pkg"
			if !strings.HasPrefix(lit, "-") && (strings.HasPrefix(lit, ".") || strings.HasPrefix(lit, "/")) {
				return nil
			}
		}
	}

	// Extract package arguments (non-flags) with positions
	packages := make([]PackageArg, 0, len(args)-installIdx)
	skipNext := false
	for _, arg := range args[installIdx+1:] {
		if skipNext {
			skipNext = false
			continue
		}

		// Get the normalized (unquoted) text for comparison and flag detection
		normalized := wordText(arg)
		if normalized == "" {
			continue
		}

		// Skip flags
		if strings.HasPrefix(normalized, "-") {
			// Some flags take a following argument
			if flagConsumesValue(normalized, mgr.flagsWithValue) {
				skipNext = true
			}
			continue
		}

		pos := arg.Pos()
		endPos := arg.End()
		line := int(pos.Line()) - 1
		startCol := int(pos.Col()) - 1
		endCol := int(endPos.Col()) - 1

		// Extract raw source token for round-trip safe edits
		raw := extractRawToken(srcLines, line, startCol, endCol)

		packages = append(packages, PackageArg{
			Value:      raw,
			Normalized: normalized,
			Line:       line,
			StartCol:   startCol,
			EndCol:     endCol,
			IsVar:      strings.Contains(normalized, "$"),
		})
	}

	if len(packages) == 0 {
		return nil
	}

	return &InstallCommand{
		Manager:    cmdName,
		Subcommand: subcommand,
		Packages:   packages,
	}
}

// extractRawToken extracts the raw source text for a token at the given position.
// This preserves quoting (e.g., "flask==2.0" stays as "flask==2.0") for round-trip safe edits.
func extractRawToken(srcLines []string, line, startCol, endCol int) string {
	if line < 0 || line >= len(srcLines) {
		return ""
	}
	l := srcLines[line]
	if startCol < 0 {
		startCol = 0
	}
	if endCol > len(l) {
		endCol = len(l)
	}
	if startCol >= endCol {
		return ""
	}
	return l[startCol:endCol]
}

// findWrappedInstallPackages finds install commands within wrapper arguments.
func findWrappedInstallPackages(
	args []*syntax.Word, variant Variant, wrapperName string, srcLines []string,
) []InstallCommand {
	var commands []InstallCommand

	IterateWrapperArgs(args, wrapperName, func(wa WrapperArg) bool {
		mgr, found := installManagers[wa.Name]
		if found {
			cmd := extractInstallCommand(wa.Name, mgr, wa.RemainingArgs, srcLines)
			if cmd != nil {
				commands = append(commands, *cmd)
			}
		}

		// Recurse for nested wrappers
		if commandWrappers[wa.Name] {
			commands = append(commands,
				findWrappedInstallPackages(wa.RemainingArgs, variant, wa.Name, srcLines)...)
		}
		return true
	})

	return commands
}

// findInstallSubcommand locates the install subcommand in args.
// Supports compound subcommands (e.g., "pip install") by matching consecutive
// literal tokens. Returns the index of the last matched token and the full
// subcommand string, or (-1, "") if not found.
func findInstallSubcommand(args []*syntax.Word, installCommands []string) (int, string) {
	// Build a list of literal tokens for matching.
	type litToken struct {
		text string
		idx  int
	}
	lits := make([]litToken, 0, len(args))
	for i, arg := range args {
		if lit := arg.Lit(); lit != "" {
			lits = append(lits, litToken{text: lit, idx: i})
		}
	}

	bestStart := -1 // earliest token start position
	bestEndIdx := -1
	bestCmd := ""
	for _, cmd := range installCommands {
		parts := strings.Split(cmd, " ")
		for j := range lits {
			if j+len(parts) > len(lits) {
				break
			}
			match := true
			for k, part := range parts {
				if lits[j+k].text != part {
					match = false
					break
				}
			}
			if match {
				if bestStart < 0 || lits[j].idx < bestStart {
					bestStart = lits[j].idx
					bestEndIdx = lits[j+len(parts)-1].idx
					bestCmd = cmd
				}
				break // first positional match for this command
			}
		}
	}

	return bestEndIdx, bestCmd
}

// wordText extracts the full text representation of a shell word,
// including variable references like ${PKG}.
func wordText(w *syntax.Word) string {
	// For simple literals, use Lit()
	if lit := w.Lit(); lit != "" {
		return lit
	}

	// Build text from parts, including variable expansions
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dpart := range p.Parts {
				switch dp := dpart.(type) {
				case *syntax.Lit:
					sb.WriteString(dp.Value)
				case *syntax.ParamExp:
					sb.WriteByte('$')
					if dp.Short {
						sb.WriteString(dp.Param.Value)
					} else {
						sb.WriteByte('{')
						sb.WriteString(dp.Param.Value)
						sb.WriteByte('}')
					}
				}
			}
		case *syntax.ParamExp:
			sb.WriteByte('$')
			if p.Short {
				sb.WriteString(p.Param.Value)
			} else {
				sb.WriteByte('{')
				sb.WriteString(p.Param.Value)
				sb.WriteByte('}')
			}
		}
	}
	return sb.String()
}

// flagConsumesValue returns true if flag consumes the next argument as its value.
// Long flags containing "=" are always self-contained.
func flagConsumesValue(flag string, managerFlags []string) bool {
	if strings.Contains(flag, "=") {
		return false
	}
	return slices.Contains(managerFlags, flag)
}
