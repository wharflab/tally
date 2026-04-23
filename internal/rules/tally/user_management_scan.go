package tally

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// scriptVisit carries a single script exposure for visitStageAndAncestryRunScripts.
// run is nil when the script originated from an observable file.
type scriptVisit struct {
	StageIndex int
	Script     string
	Variant    shell.Variant
	Run        *instructions.RunCommand
}

// visitStageAndAncestryRunScripts iterates every RUN command script and every
// observable script-like file across the given stage and its FROM ancestry
// chain, calling visit in source order (stage first, then ancestors). The
// visitor receives the stage that produced the script so rules can attribute
// evidence correctly; run is nil when the script originated from an
// observable file.
//
// Variant resolution:
//   - RUN scripts use the stage's effective shell variant.
//   - Observable files use shell.VariantFromScriptPath (extension-based).
func visitStageAndAncestryRunScripts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	startStageIdx int,
	visit func(v scriptVisit),
) {
	visitOneStage := func(stageIdx int) {
		sf := fileFacts.Stage(stageIdx)
		if sf == nil {
			return
		}
		for _, run := range sf.Runs {
			visit(scriptVisit{
				StageIndex: stageIdx,
				Script:     run.CommandScript,
				Variant:    run.Shell.Variant,
				Run:        run.Run,
			})
		}
		for _, of := range sf.ObservableFiles {
			if !looksLikeScript(of.Path) {
				continue
			}
			content, ok := of.Content()
			if !ok || content == "" {
				continue
			}
			visit(scriptVisit{
				StageIndex: stageIdx,
				Script:     content,
				Variant:    shell.VariantFromScriptPath(of.Path),
				Run:        nil,
			})
		}
	}

	visitOneStage(startStageIdx)

	model := input.Semantic
	if model == nil {
		return
	}

	visited := map[int]bool{startStageIdx: true}
	for idx := startStageIdx; ; {
		info := model.StageInfo(idx)
		if info == nil || info.BaseImage == nil ||
			!info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			return
		}
		parentIdx := info.BaseImage.StageIndex
		if visited[parentIdx] {
			return
		}
		visited[parentIdx] = true
		visitOneStage(parentIdx)
		idx = parentIdx
	}
}

// findUserCreationCmds finds user-creation commands in a shell script.
// Linux: useradd, adduser.
// Windows cmd: net user <name> /add.
// PowerShell: New-LocalUser.
func findUserCreationCmds(script string, variant shell.Variant) []shell.CommandInfo {
	var result []shell.CommandInfo

	result = append(result, shell.FindCommands(script, variant, "useradd", "adduser")...)

	if variant == shell.VariantCmd {
		for _, cmd := range shell.FindCommands(script, variant, "net") {
			if !strings.EqualFold(cmd.Subcommand, "user") { //nolint:customlint // shell "user" subcommand, not Dockerfile USER
				continue
			}
			if slices.ContainsFunc(cmd.Args, func(a string) bool {
				return strings.EqualFold(a, "/add")
			}) {
				result = append(result, cmd)
			}
		}
	}

	if variant.IsPowerShell() {
		result = append(result, shell.FindCommands(script, variant, "New-LocalUser")...)
	}

	return result
}

// extractCreatedUsername extracts the username from a user-creation command.
// For useradd/adduser: the last non-flag argument (LOGIN is always last positional).
// For net user /add: the argument between "user" and "/add".
// For New-LocalUser: the -Name parameter value.
func extractCreatedUsername(cmd *shell.CommandInfo) string {
	switch {
	case cmd.Name == "useradd" || cmd.Name == "adduser":
		return lastNonFlagArg(cmd.Args)

	case strings.EqualFold(cmd.Name, "net"):
		for _, arg := range cmd.Args {
			if strings.EqualFold(arg, "user") || strings.HasPrefix(arg, "/") { //nolint:customlint // shell subcommand
				continue
			}
			return arg
		}

	case strings.EqualFold(cmd.Name, "New-LocalUser"):
		return cmd.GetArgValue("-Name")
	}

	return ""
}

// membershipInfo is a single user-to-supplementary-group-assignment record
// extracted from a shell command such as `useradd -G`, `usermod -aG`,
// `gpasswd -a`, `net localgroup /add`, or `Add-LocalGroupMember`.
type membershipInfo struct {
	User   string
	Groups []string
}

// findUserMembershipCmds returns every supplementary-group assignment found
// in a shell script. Each returned membershipInfo records the assignment's
// user and the groups it establishes.
//
// Linux (POSIX-family):
//   - useradd -G g1,g2 user  / useradd --groups=g user  (NOT -g/--gid alone)
//   - usermod -aG g user / usermod -a -G g user / usermod -G g user
//   - gpasswd -a user group
//   - adduser USER GROUP  (membership form: 2 positionals, no creation flags)
//   - addgroup USER GROUP (membership form: 2 positionals, no creation flags)
//
// Windows cmd:
//   - net localgroup <GROUP> <USER> /add
//
// Windows PowerShell:
//   - Add-LocalGroupMember -Group <G> -Member <U>
//   - Add-LocalGroupMember <G> <U>   (positional fallback)
//
// PowerShell -Member values that look like arrays (start with "@(") or
// contain commas are conservatively skipped — false positives on those
// shapes are more costly than the occasional missed catch.
func findUserMembershipCmds(script string, variant shell.Variant) []membershipInfo {
	var out []membershipInfo

	if variant.SupportsPOSIXShellAST() {
		out = append(out, findPOSIXMembershipCmds(script, variant)...)
	}

	if variant == shell.VariantCmd {
		out = append(out, findCmdMembershipCmds(script, variant)...)
	}

	if variant.IsPowerShell() {
		out = append(out, findPowerShellMembershipCmds(script, variant)...)
	}

	return out
}

func findPOSIXMembershipCmds(script string, variant shell.Variant) []membershipInfo {
	var out []membershipInfo

	for _, cmd := range shell.FindCommands(script, variant, "useradd") {
		groups := commaSplitNonEmpty(firstNonEmptyArgValue(&cmd, "-G", "--groups"))
		if len(groups) == 0 {
			continue
		}
		user := lastNonFlagArg(cmd.Args)
		if user == "" {
			continue
		}
		out = append(out, membershipInfo{User: user, Groups: groups})
	}

	for _, cmd := range shell.FindCommands(script, variant, "usermod") {
		rawGroups := usermodGroupsValue(&cmd)
		groups := commaSplitNonEmpty(rawGroups)
		if len(groups) == 0 {
			continue
		}
		user := lastNonFlagArg(cmd.Args)
		if user == "" {
			continue
		}
		out = append(out, membershipInfo{User: user, Groups: groups})
	}

	for _, cmd := range shell.FindCommands(script, variant, "gpasswd") {
		if !cmd.HasFlag("-a") && !cmd.HasFlag("--add") {
			continue
		}
		user, group := gpasswdAddTarget(&cmd)
		if user == "" || group == "" {
			continue
		}
		out = append(out, membershipInfo{User: user, Groups: []string{group}})
	}

	for _, cmd := range shell.FindCommands(script, variant, "adduser") {
		user, group, ok := twoPositionalMembership(&cmd, adduserCreationFlags)
		if !ok {
			continue
		}
		out = append(out, membershipInfo{User: user, Groups: []string{group}})
	}

	for _, cmd := range shell.FindCommands(script, variant, "addgroup") {
		user, group, ok := twoPositionalMembership(&cmd, addgroupCreationFlags)
		if !ok {
			continue
		}
		out = append(out, membershipInfo{User: user, Groups: []string{group}})
	}

	return out
}

func findCmdMembershipCmds(script string, variant shell.Variant) []membershipInfo {
	var out []membershipInfo

	for _, cmd := range shell.FindCommands(script, variant, "net") {
		if !strings.EqualFold(cmd.Subcommand, "localgroup") {
			continue
		}
		if !slices.ContainsFunc(cmd.Args, func(a string) bool {
			return strings.EqualFold(a, "/add")
		}) {
			continue
		}
		group, user := netLocalgroupAddTargets(&cmd)
		if group == "" || user == "" {
			continue
		}
		out = append(out, membershipInfo{User: user, Groups: []string{group}})
	}

	return out
}

func findPowerShellMembershipCmds(script string, variant shell.Variant) []membershipInfo {
	var out []membershipInfo

	for _, cmd := range shell.FindCommands(script, variant, "Add-LocalGroupMember") {
		group := cmd.GetArgValue("-Group")
		member := cmd.GetArgValue("-Member")

		if group == "" || member == "" {
			g, m, ok := twoPositionalValues(&cmd)
			if !ok {
				continue
			}
			if group == "" {
				group = g
			}
			if member == "" {
				member = m
			}
		}

		if group == "" || member == "" {
			continue
		}
		if strings.HasPrefix(member, "@(") || strings.Contains(member, ",") {
			continue
		}

		out = append(out, membershipInfo{User: member, Groups: []string{group}})
	}

	return out
}

// twoPositionalMembership applies the "exactly 2 positionals, no creation
// flags" heuristic used to distinguish adduser/addgroup membership form from
// creation form.
func twoPositionalMembership(cmd *shell.CommandInfo, creationFlags map[string]bool) (user, group string, ok bool) {
	var positionals []string
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "-") {
			if creationFlagsMatch(arg, creationFlags) {
				return "", "", false
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	if len(positionals) != 2 {
		return "", "", false
	}
	return positionals[0], positionals[1], true
}

// creationFlagsMatch reports whether a flag token is a creation-ish flag
// that disqualifies the membership interpretation.
func creationFlagsMatch(flag string, creationFlags map[string]bool) bool {
	bare, _, _ := strings.Cut(flag, "=")
	if creationFlags[bare] {
		return true
	}
	if strings.HasPrefix(bare, "--") || !strings.HasPrefix(bare, "-") {
		return false
	}
	for _, r := range bare[1:] {
		if creationFlags["-"+string(r)] {
			return true
		}
	}
	return false
}

// twoPositionalValues extracts exactly two positional arguments from a
// PowerShell command, using the tree-sitter-powershell-provided ArgKinds to
// skip parameters (`-Verbose`, `-Group`, etc.) without guessing at their
// value-arity. Returns ok=false if fewer or more than two positionals exist.
//
// Using the grammar's classification is strictly more correct than a textual
// HasPrefix("-") check: the grammar recognises quoted tokens like `"-app"`
// as value expressions rather than parameter tokens, and it never consumes
// the arg after a switch (e.g. `-Verbose`) as its value.
func twoPositionalValues(cmd *shell.CommandInfo) (first, second string, ok bool) {
	var positionals []string
	for i, arg := range cmd.Args {
		if isFlagArg(cmd, i) {
			continue
		}
		positionals = append(positionals, arg)
	}
	if len(positionals) != 2 {
		return "", "", false
	}
	return positionals[0], positionals[1], true
}

// isFlagArg reports whether the i-th argument of cmd is a flag/parameter.
// If the parser populated ArgKinds (PowerShell today), that classification
// is authoritative. Otherwise the function falls back to a textual "-" prefix
// check so POSIX callers still get the historical behavior.
func isFlagArg(cmd *shell.CommandInfo, i int) bool {
	if i < len(cmd.ArgKinds) && cmd.ArgKinds[i] != shell.ArgKindUnknown {
		return cmd.ArgKinds[i] == shell.ArgKindFlag
	}
	return strings.HasPrefix(cmd.Args[i], "-")
}

// usermodGroupsValue returns the value of usermod's -G / --groups flag,
// handling the combined short-flag form (e.g., `usermod -aG docker app`)
// which cmd.GetArgValue does not understand because "-aG" != "-G".
func usermodGroupsValue(cmd *shell.CommandInfo) string {
	if v := firstNonEmptyArgValue(cmd, "-G", "--groups"); v != "" {
		return v
	}
	for i, arg := range cmd.Args {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
			continue
		}
		// Short combined form: a single leading "-" followed by a run of
		// boolean short flags, optionally ending with -G. The value for -G
		// is the next positional arg.
		body := arg[1:]
		if !strings.ContainsRune(body, 'G') {
			continue
		}
		for _, r := range body {
			if r != 'G' && !isUsermodBooleanShortFlag(r) {
				goto nextArg
			}
		}
		if i+1 < len(cmd.Args) {
			next := cmd.Args[i+1]
			if !strings.HasPrefix(next, "-") {
				return next
			}
		}
	nextArg:
	}
	return ""
}

// isUsermodBooleanShortFlag reports whether a single-char flag of usermod
// is a boolean (takes no value) and can therefore appear combined before -G
// in the `-aG` / `-aUG` form.
func isUsermodBooleanShortFlag(r rune) bool {
	switch r {
	case 'a', // --append
		'l', // --lock
		'm', // --move-home
		'o', // --non-unique
		'r', // --remove
		'U': // --unlock
		return true
	}
	return false
}

// gpasswdAddTarget extracts (user, group) from a `gpasswd -a USER GROUP`
// invocation. Handles both `-a USER` and `-a=USER` flag forms.
func gpasswdAddTarget(cmd *shell.CommandInfo) (user, group string) {
	for i, arg := range cmd.Args {
		if arg == "-a" || arg == "--add" {
			if i+1 < len(cmd.Args) {
				user = cmd.Args[i+1]
			}
			group = firstNonFlagAfter(cmd.Args, i+2)
			return user, group
		}
		if value, found := strings.CutPrefix(arg, "-a="); found {
			user = value
			group = firstNonFlagAfter(cmd.Args, i+1)
			return user, group
		}
		if value, found := strings.CutPrefix(arg, "--add="); found {
			user = value
			group = firstNonFlagAfter(cmd.Args, i+1)
			return user, group
		}
	}
	return "", ""
}

// netLocalgroupAddTargets extracts (group, user) from
// `net localgroup <GROUP> <USER> /add`.
func netLocalgroupAddTargets(cmd *shell.CommandInfo) (group, user string) {
	var positionals []string
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "/") || strings.EqualFold(arg, "localgroup") {
			continue
		}
		positionals = append(positionals, arg)
	}
	if len(positionals) < 2 {
		return "", ""
	}
	return positionals[0], positionals[1]
}

// firstNonFlagAfter returns the first non-flag arg in args[from:], or "".
func firstNonFlagAfter(args []string, from int) string {
	for i := from; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

// firstNonEmptyArgValue returns the first non-empty value produced by
// cmd.GetArgValue for any of the given flags.
func firstNonEmptyArgValue(cmd *shell.CommandInfo, flags ...string) string {
	for _, f := range flags {
		if v := cmd.GetArgValue(f); v != "" {
			return v
		}
	}
	return ""
}

// commaSplitNonEmpty splits s on "," and returns trimmed, non-empty entries.
func commaSplitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// adduserCreationFlags lists flags whose presence means adduser is creating
// a user, not adding an existing user to a supplementary group.
var adduserCreationFlags = map[string]bool{
	"-D": true, "-S": true, "-m": true, "-u": true, "-g": true, "-G": true,
	"-h": true, "-H": true, "-s": true,
	"--system":            true,
	"--disabled-password": true,
	"--disabled-login":    true,
	"--ingroup":           true,
	"--gecos":             true,
	"--home":              true,
	"--shell":             true,
	"--uid":               true,
	"--gid":               true,
}

// addgroupCreationFlags lists flags whose presence means addgroup is creating
// a new group, not adding a user to an existing group.
var addgroupCreationFlags = map[string]bool{
	"-S": true, "-g": true,
	"--system": true,
	"--gid":    true,
}
