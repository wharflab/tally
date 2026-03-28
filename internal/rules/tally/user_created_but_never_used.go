package tally

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// UserCreatedButNeverUsedRuleCode is the full rule code.
const UserCreatedButNeverUsedRuleCode = rules.TallyRulePrefix + "user-created-but-never-used"

// userCreation captures a single user-creation command found in a stage.
type userCreation struct {
	username   string // extracted username (may be empty if extraction failed)
	stageIndex int    // stage where the creation was found
	run        *instructions.RunCommand
}

// UserCreatedButNeverUsedRule detects when the final stage (or its FROM
// ancestry chain) creates a dedicated user via useradd/adduser but the
// effective runtime identity stays root and no privilege-drop entrypoint
// (gosu, su-exec, suexec, setpriv) is detected.
//
// The rule suppresses when the created user is referenced in ownership
// contexts (COPY/ADD --chown, RUN chown, observable entrypoint scripts),
// because that indicates deliberate permissions work rather than a
// forgotten hardening step.
//
// Cross-rule interaction:
//
//   - hadolint/DL3002 fires when the last USER is explicitly root. This
//     rule fires when a user is created but never switched to. Complementary;
//     no suppression needed.
//   - hadolint/DL3046 checks useradd without -l for high UIDs. Orthogonal.
//   - tally/stateful-root-runtime checks root + stateful signals. Complementary.
type UserCreatedButNeverUsedRule struct{}

// NewUserCreatedButNeverUsedRule creates a new rule instance.
func NewUserCreatedButNeverUsedRule() *UserCreatedButNeverUsedRule {
	return &UserCreatedButNeverUsedRule{}
}

// Metadata returns the rule metadata.
func (r *UserCreatedButNeverUsedRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            UserCreatedButNeverUsedRuleCode,
		Name:            "User Created But Never Used",
		Description:     "Final stage creates a user but never switches to it",
		DocURL:          rules.TallyDocURL(UserCreatedButNeverUsedRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
	}
}

// Check runs the user-created-but-never-used rule.
//
// It fires when ALL of these are true:
//  1. The effective runtime user of the final stage is root (explicit or implicit).
//  2. A user creation command exists in the final stage or its FROM ancestry.
//  3. No privilege-drop entrypoint pattern is detected.
//  4. The created user is not referenced in ownership contexts (--chown, chown).
func (r *UserCreatedButNeverUsedRule) Check(input rules.LintInput) []rules.Violation {
	rc := checkFinalStageRoot(input)
	if rc == nil {
		return nil
	}

	finalIdx := rc.FinalIdx
	sf := rc.StageFacts

	// Step 2: Check for privilege-drop suppression.
	if sf.DropsPrivilegesAtRuntime() {
		return nil
	}

	// Step 3: Collect user creation commands from the final stage and ancestry.
	creations := collectUserCreations(input, rc.FileFacts, finalIdx)
	if len(creations) == 0 {
		return nil
	}

	// Step 4: Suppress users referenced in ownership contexts.
	unreferenced := filterUnreferencedUsers(creations, input, rc.FileFacts, finalIdx)
	if len(unreferenced) == 0 {
		return nil
	}

	// Build violation for the first unreferenced user creation.
	meta := r.Metadata()
	uc := unreferenced[0]

	var rootDesc string
	if rc.ImplicitRoot {
		rootDesc = "no USER instruction (defaults to root)"
	} else {
		rootDesc = "USER is " + sf.EffectiveUser
	}

	var msg string
	if uc.username != "" {
		msg = fmt.Sprintf(
			"user %q is created but the final stage never switches to it (%s)",
			uc.username, rootDesc,
		)
	} else {
		msg = fmt.Sprintf(
			"a user is created but the final stage never switches to it (%s)",
			rootDesc,
		)
	}

	var loc rules.Location
	switch {
	case uc.run != nil:
		loc = rules.NewLocationFromRanges(input.File, uc.run.Location())
	case uc.stageIndex >= 0 && uc.stageIndex < len(input.Stages):
		loc = rules.NewLocationFromRanges(input.File, input.Stages[uc.stageIndex].Location)
	default:
		loc = rules.NewLocationFromRanges(input.File, input.Stages[finalIdx].Location)
	}
	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL)

	if uc.stageIndex != finalIdx {
		v = v.WithDetail(fmt.Sprintf(
			"The user creation originates in ancestor stage %d, which flows "+
				"into the final stage via FROM.", uc.stageIndex,
		))
	}

	v.StageIndex = finalIdx

	// Auto-fix: insert USER <username> before ENTRYPOINT/CMD.
	if fix := r.buildFix(uc, input, finalIdx); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return []rules.Violation{v}
}

// buildFix creates an unsafe fix that inserts USER <username> before the
// first ENTRYPOINT or CMD in the final stage, or at the end of the stage.
func (r *UserCreatedButNeverUsedRule) buildFix(
	uc userCreation,
	input rules.LintInput,
	finalIdx int,
) *rules.SuggestedFix {
	if uc.username == "" {
		return nil
	}

	stage := input.Stages[finalIdx]
	sm := input.SourceMap()

	// Find the first ENTRYPOINT or CMD to insert before.
	var insertLine int
	found := false
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.EntrypointCommand:
			if loc := c.Location(); len(loc) > 0 {
				insertLine = loc[0].Start.Line
				found = true
			}
		case *instructions.CmdCommand:
			if loc := c.Location(); len(loc) > 0 {
				insertLine = loc[0].Start.Line
				found = true
			}
		}
		if found {
			break
		}
	}

	if !found {
		// No ENTRYPOINT/CMD: insert at end of file.
		insertLine = sm.LineCount() + 1
	}

	userInstruction := fmt.Sprintf("USER %s\n", uc.username)

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Add USER %s before runtime command", uc.username),
		Safety:      rules.FixUnsafe,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
			NewText:  userInstruction,
		}},
	}
}

// collectUserCreations scans the final stage and its FROM ancestry chain
// for user creation commands (useradd, adduser, net user /add, New-LocalUser).
func collectUserCreations(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	finalIdx int,
) []userCreation {
	var creations []userCreation

	// Scan the final stage itself.
	creations = append(creations, findStageUserCreations(fileFacts, finalIdx)...)

	// Walk the FROM ancestry chain.
	model, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // nil-safe assertion
	if model == nil {
		return creations
	}

	visited := map[int]bool{finalIdx: true}
	for idx := finalIdx; ; {
		info := model.StageInfo(idx)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			break
		}

		parentIdx := info.BaseImage.StageIndex
		if visited[parentIdx] {
			break
		}
		visited[parentIdx] = true

		creations = append(creations, findStageUserCreations(fileFacts, parentIdx)...)
		idx = parentIdx
	}

	return creations
}

// findStageUserCreations scans a single stage's RUN commands and observable
// files for user creation commands.
func findStageUserCreations(fileFacts *facts.FileFacts, stageIdx int) []userCreation {
	sf := fileFacts.Stage(stageIdx)
	if sf == nil {
		return nil
	}

	var creations []userCreation

	// Scan direct RUN commands.
	for _, run := range sf.Runs {
		cmds := findUserCreationCmds(run.CommandScript, run.Shell.Variant)
		for i := range cmds {
			creations = append(creations, userCreation{
				username:   extractCreatedUsername(&cmds[i]),
				stageIndex: stageIdx,
				run:        run.Run,
			})
		}
	}

	// Scan observable files (COPY heredoc scripts, build context scripts).
	for _, of := range sf.ObservableFiles {
		if !looksLikeScript(of.Path) {
			continue
		}
		content, ok := of.Content()
		if !ok || content == "" {
			continue
		}
		variant := shell.VariantFromScriptPath(of.Path)
		cmds := findUserCreationCmds(content, variant)
		if len(cmds) > 0 {
			// Attribute to the first RUN in the stage as a best-effort location.
			var run *instructions.RunCommand
			if len(sf.Runs) > 0 {
				run = sf.Runs[0].Run
			}
			for i := range cmds {
				creations = append(creations, userCreation{
					username:   extractCreatedUsername(&cmds[i]),
					stageIndex: stageIdx,
					run:        run,
				})
			}
		}
	}

	return creations
}

// findUserCreationCmds finds user creation commands in a shell script.
func findUserCreationCmds(script string, variant shell.Variant) []shell.CommandInfo {
	var result []shell.CommandInfo

	// Linux: useradd, adduser
	result = append(result, shell.FindCommands(script, variant, "useradd", "adduser")...)

	// Windows cmd: net user <name> /add
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

	// Windows PowerShell: New-LocalUser
	if variant.IsPowerShell() {
		result = append(result, shell.FindCommands(script, variant, "New-LocalUser")...)
	}

	return result
}

// extractCreatedUsername extracts the username from a user creation command.
// For useradd/adduser: the last non-flag argument (LOGIN is always last positional).
// For net user /add: the argument between "user" and "/add".
// For New-LocalUser: the -Name parameter value.
func extractCreatedUsername(cmd *shell.CommandInfo) string {
	switch {
	case cmd.Name == "useradd" || cmd.Name == "adduser":
		return lastNonFlagArg(cmd.Args)

	case strings.EqualFold(cmd.Name, "net"):
		// net user <name> /add — find the positional arg that is the username.
		for _, arg := range cmd.Args {
			if strings.EqualFold(arg, "user") || strings.HasPrefix(arg, "/") { //nolint:customlint // shell subcommand
				continue
			}
			// First non-"user", non-flag arg is the username.
			return arg
		}

	case strings.EqualFold(cmd.Name, "New-LocalUser"):
		return cmd.GetArgValue("-Name")
	}

	return ""
}

// lastNonFlagArg returns the last argument that doesn't start with "-".
// For useradd/adduser, the LOGIN name is always the last positional argument.
func lastNonFlagArg(args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

// filterUnreferencedUsers removes user creations where the username appears
// in ownership contexts: COPY/ADD --chown, RUN chown, or observable
// entrypoint scripts.
func filterUnreferencedUsers(
	creations []userCreation,
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	finalIdx int,
) []userCreation {
	referenced := collectReferencedUsers(input, fileFacts, finalIdx)
	if len(referenced) == 0 {
		return creations
	}

	var unreferenced []userCreation
	for _, uc := range creations {
		if uc.username == "" || !referenced[uc.username] {
			unreferenced = append(unreferenced, uc)
		}
	}
	return unreferenced
}

// collectReferencedUsers collects usernames referenced in ownership contexts
// across the final stage and its ancestry chain.
func collectReferencedUsers(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	finalIdx int,
) map[string]bool {
	refs := map[string]bool{}

	// Collect from the final stage and ancestry chain.
	model, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // nil-safe assertion

	visited := map[int]bool{}
	for idx := finalIdx; !visited[idx]; {
		visited[idx] = true
		collectStageReferencedUsers(input, fileFacts, idx, refs)

		if model == nil {
			break
		}
		info := model.StageInfo(idx)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			break
		}
		idx = info.BaseImage.StageIndex
	}

	return refs
}

// collectStageReferencedUsers finds usernames referenced in a single stage's
// ownership instructions and observable scripts.
func collectStageReferencedUsers(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	stageIdx int,
	refs map[string]bool,
) {
	if stageIdx < 0 || stageIdx >= len(input.Stages) {
		return
	}
	stage := input.Stages[stageIdx]
	sf := fileFacts.Stage(stageIdx)

	// COPY --chown and ADD --chown.
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.CopyCommand:
			if user := chownUser(c.Chown); user != "" {
				refs[user] = true
			}
		case *instructions.AddCommand:
			if user := chownUser(c.Chown); user != "" {
				refs[user] = true
			}
		}
	}

	if sf == nil {
		return
	}

	// RUN chown <user> (Linux).
	for _, run := range sf.Runs {
		for _, cmd := range shell.FindCommands(run.CommandScript, run.Shell.Variant, "chown") {
			if user := chownTarget(&cmd); user != "" {
				refs[user] = true
			}
		}
	}

	// Windows ACL commands: icacls /grant <user>, icacls /setowner <user>,
	// takeown, Set-Acl. These are the Windows equivalent of chown — evidence
	// that the created user is being used for permission management.
	for _, run := range sf.Runs {
		collectWindowsACLRefs(run.CommandScript, run.Shell.Variant, refs)
	}

	// Observable entrypoint/CMD scripts — check both metadata and body.
	for _, of := range sf.ObservableFiles {
		// Metadata chown (e.g. COPY --chown on the observable file itself).
		if user := chownUser(of.Chown); user != "" {
			refs[user] = true
		}

		content, ok := of.Content()
		if !ok || content == "" {
			continue
		}

		// Scan script body for chown / Windows ACL commands.
		variant := shell.VariantFromScriptPath(of.Path)
		for _, cmd := range shell.FindCommands(content, variant, "chown") {
			if user := chownTarget(&cmd); user != "" {
				refs[user] = true
			}
		}
		collectWindowsACLRefs(content, variant, refs)
	}
}

// collectWindowsACLRefs scans a shell script for Windows ACL commands
// (icacls, New-Object ...AccessRule) that reference usernames, and adds
// those usernames to the refs map.
func collectWindowsACLRefs(script string, variant shell.Variant, refs map[string]bool) {
	// icacls: /grant <user>:(perms) or /setowner <user>
	for _, cmd := range shell.FindCommands(script, variant, "icacls") {
		for i, arg := range cmd.Args {
			if !strings.HasPrefix(arg, "/") {
				continue
			}
			lower := strings.ToLower(arg)
			if (lower == "/grant" || lower == "/setowner") && i+1 < len(cmd.Args) {
				user := cmd.Args[i+1]
				// icacls /grant user:(OI)(CI)F — strip permissions suffix.
				if idx := strings.IndexByte(user, ':'); idx > 0 {
					user = user[:idx]
				}
				if user != "" {
					refs[user] = true
				}
			}
		}
	}

	// PowerShell New-Object ...AccessRule("user", ...) — the common pattern
	// for building ACLs that are later applied via Set-Acl. The username is
	// the first constructor argument in the parenthesized argument list.
	for _, cmd := range shell.FindCommands(script, variant, "New-Object") {
		if !strings.Contains(cmd.Subcommand, "AccessRule") {
			continue
		}
		if user := extractNewObjectAccessRuleUser(&cmd); user != "" {
			refs[user] = true
		}
	}
}

// extractNewObjectAccessRuleUser extracts the username from a PowerShell
// New-Object System.Security.AccessControl.FileSystemAccessRule("user", ...)
// constructor call. The first argument in the parenthesized list is the
// identity reference (user or group name).
func extractNewObjectAccessRuleUser(cmd *shell.CommandInfo) string {
	for _, arg := range cmd.Args {
		if !strings.HasPrefix(arg, "(") {
			continue
		}
		// Extract first quoted string from the constructor args.
		// Pattern: ("username", "FullControl", "Allow")
		if start := strings.IndexByte(arg, '"'); start >= 0 {
			if end := strings.IndexByte(arg[start+1:], '"'); end >= 0 {
				return arg[start+1 : start+1+end]
			}
		}
		// Also try single quotes.
		if start := strings.IndexByte(arg, '\''); start >= 0 {
			if end := strings.IndexByte(arg[start+1:], '\''); end >= 0 {
				return arg[start+1 : start+1+end]
			}
		}
	}
	return ""
}

// chownUser extracts the user portion from a --chown value (user:group → user).
// Returns empty string if the value is empty or refers to root.
func chownUser(chown string) string {
	if chown == "" {
		return ""
	}
	user, _, _ := strings.Cut(chown, ":")
	user = strings.TrimSpace(user)
	if user == "" || facts.IsRootUser(user) {
		return ""
	}
	return user
}

// chownTarget extracts the user from a chown command invocation.
// chown accepts [OWNER][:[GROUP]] FILE... — the first positional arg is the
// ownership spec.
func chownTarget(cmd *shell.CommandInfo) string {
	// Subcommand is the first non-flag arg, which for chown is the owner spec.
	if cmd.Subcommand == "" {
		return ""
	}
	user, _, _ := strings.Cut(cmd.Subcommand, ":")
	if user == "" || facts.IsRootUser(user) {
		return ""
	}
	return user
}

// looksLikeScript checks if a file path looks like a shell script.
func looksLikeScript(filePath string) bool {
	ext := path.Ext(filePath)
	switch ext {
	case ".sh", ".bash", ".ps1", ".cmd", ".bat":
		return true
	}
	// Also match common entrypoint script names.
	base := path.Base(filePath)
	return strings.Contains(base, "entrypoint") || //nolint:customlint // filename pattern, not Dockerfile instruction
		strings.Contains(base, "setup") ||
		strings.Contains(base, "init") ||
		strings.Contains(base, "start")
}

func init() {
	rules.Register(NewUserCreatedButNeverUsedRule())
}
