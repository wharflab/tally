package shell

import (
	"net/url"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/dfgitutil"
	"mvdan.cc/sh/v3/syntax"
)

// GitSourceOpportunity describes a git-clone flow that can be replaced by ADD.
type GitSourceOpportunity struct {
	AddSource         string
	AddDestination    string
	AddChecksum       string
	RepoPath          string
	PrecedingCommands string
	RemainingCommands string
	EnteredRepo       bool
	CheckoutRef       string
	CloneRef          string
	UsesSubmodules    bool
	KeepGitDir        bool
	NormalizedRemote  string
}

type gitCloneSpec struct {
	Remote           string
	NormalizedRemote string
	ShortName        string
	Ref              string
	DestinationArg   string
	Submodules       bool
}

type gitChainStepKind int

const (
	gitChainStepOther gitChainStepKind = iota
	gitChainStepCd
	gitChainStepClone
	gitChainStepCheckout
)

const (
	gitRemoteSchemeHTTP  = "http"
	gitRemoteSchemeHTTPS = "https"
)

type gitChainStep struct {
	Text        string
	Kind        gitChainStepKind
	CDTarget    string
	CheckoutRef string
	Clone       *gitCloneSpec
}

// HasGitCloneRemote reports whether a script contains a git clone of a remote repository.
func HasGitCloneRemote(script string, variant Variant) bool {
	steps, ok := gitTransformSteps(script, variant)
	if !ok {
		return false
	}
	for _, step := range steps {
		if step.Kind == gitChainStepClone {
			return true
		}
	}
	return false
}

// FirstGitSourceOpportunity returns the first git-clone flow that can be safely
// extracted into an ADD git source. The returned fix plan is intentionally
// conservative: it only rewrites simple single-statement && chains.
func FirstGitSourceOpportunity(script string, variant Variant, workdir string) (*GitSourceOpportunity, bool) {
	steps, ok := gitTransformSteps(script, variant)
	if !ok {
		return nil, false
	}

	currentDir, ok := normalizeGitWorkdir(workdir)
	if !ok {
		return nil, false
	}
	baseWorkdir := currentDir

	for i, step := range steps {
		switch step.Kind {
		case gitChainStepOther, gitChainStepCheckout:
			continue
		case gitChainStepCd:
			nextDir, ok := resolveGitPath(currentDir, step.CDTarget)
			if !ok {
				return nil, false
			}
			currentDir = nextDir
		case gitChainStepClone:
			opportunity, ok := buildGitSourceOpportunity(steps, i, currentDir, baseWorkdir, variant)
			if !ok {
				return nil, false
			}
			if opportunity != nil {
				return opportunity, true
			}
		}
	}

	return nil, false
}

// HasActionableGitSourceOpportunity reports whether a script has a git-clone
// flow that prefer-add-git can extract into ADD <git source>.
func HasActionableGitSourceOpportunity(script string, variant Variant, workdir string) bool {
	_, ok := FirstGitSourceOpportunity(script, variant, workdir)
	return ok
}

func buildGitSourceOpportunity(
	steps []gitChainStep,
	cloneIndex int,
	cloneBaseDir string,
	baseWorkdir string,
	variant Variant,
) (*GitSourceOpportunity, bool) {
	if cloneIndex < 0 || cloneIndex >= len(steps) {
		return nil, false
	}

	clone := steps[cloneIndex].Clone
	if clone == nil {
		return nil, true
	}

	repoPath, ok := gitCloneRepoPath(clone, cloneBaseDir)
	if !ok {
		return nil, false
	}

	consumed, enteredRepo, checkoutRef, ok := consumeGitCloneFollowups(steps, cloneIndex, cloneBaseDir, repoPath)
	if !ok {
		return nil, false
	}

	preceding := gitClonePrecedingCommands(steps[:cloneIndex])
	remaining := gitCloneRemainingCommands(steps[consumed:], cloneBaseDir, baseWorkdir, repoPath, enteredRepo)

	return &GitSourceOpportunity{
		AddSource:         buildGitSourceURL(clone, checkoutRef),
		AddDestination:    repoPath,
		AddChecksum:       buildGitSourceChecksum(clone, checkoutRef),
		RepoPath:          repoPath,
		PrecedingCommands: preceding,
		RemainingCommands: remaining,
		EnteredRepo:       enteredRepo,
		CheckoutRef:       checkoutRef,
		CloneRef:          clone.Ref,
		UsesSubmodules:    clone.Submodules,
		KeepGitDir:        hasGitCommands(remaining, variant),
		NormalizedRemote:  clone.NormalizedRemote,
	}, true
}

func gitCloneRepoPath(clone *gitCloneSpec, cloneBaseDir string) (string, bool) {
	if clone == nil {
		return "", false
	}

	repoName := clone.ShortName
	if clone.DestinationArg != "" {
		repoName = clone.DestinationArg
	}
	return resolveGitPath(cloneBaseDir, repoName)
}

func consumeGitCloneFollowups(
	steps []gitChainStep,
	cloneIndex int,
	cloneBaseDir string,
	repoPath string,
) (consumed int, enteredRepo bool, checkoutRef string, ok bool) {
	consumed = cloneIndex + 1

	if consumed < len(steps) && steps[consumed].Kind == gitChainStepCd &&
		cdTargetsRepo(cloneBaseDir, steps[consumed].CDTarget, repoPath) {
		enteredRepo = true
		consumed++
	}

	if enteredRepo && consumed < len(steps) && steps[consumed].Kind == gitChainStepCheckout {
		if !canEncodeGitCheckoutRef(steps[consumed].CheckoutRef) {
			return 0, false, "", false
		}
		checkoutRef = steps[consumed].CheckoutRef
		consumed++
	}

	return consumed, enteredRepo, checkoutRef, true
}

func gitClonePrecedingCommands(steps []gitChainStep) string {
	if gitStepsAreCdOnly(steps) {
		return ""
	}
	return joinGitSteps(steps)
}

func gitCloneRemainingCommands(
	steps []gitChainStep,
	cloneBaseDir string,
	baseWorkdir string,
	repoPath string,
	enteredRepo bool,
) string {
	remaining := joinGitSteps(steps)
	if remaining == "" {
		return ""
	}

	switch {
	case enteredRepo:
		return "cd " + repoPath + " && " + remaining
	case cloneBaseDir != baseWorkdir:
		return "cd " + cloneBaseDir + " && " + remaining
	default:
		return remaining
	}
}

func gitStepsAreCdOnly(steps []gitChainStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if step.Kind != gitChainStepCd {
			return false
		}
	}
	return true
}

func gitTransformSteps(script string, variant Variant) ([]gitChainStep, bool) {
	if !variant.SupportsPOSIXShellAST() {
		return nil, false
	}

	prog, err := parseScript(script, variant)
	if err != nil || prog == nil || len(prog.Stmts) != 1 {
		return nil, false
	}
	if !isSimpleStatement(prog.Stmts[0]) {
		return nil, false
	}

	texts := ExtractChainedCommands(script, variant)
	if len(texts) == 0 {
		return nil, false
	}

	steps := make([]gitChainStep, 0, len(texts))
	for _, text := range texts {
		steps = append(steps, classifyGitChainStep(text, variant))
	}
	return steps, true
}

func classifyGitChainStep(text string, variant Variant) gitChainStep {
	step := gitChainStep{Text: strings.TrimSpace(text)}
	if step.Text == "" {
		return step
	}

	cmd, ok := parseRawSimpleCommand(step.Text, variant)
	if !ok {
		return step
	}

	switch {
	case cmd.Name == "cd":
		for _, arg := range cmd.Args {
			if arg == "" || strings.HasPrefix(arg, "-") {
				continue
			}
			step.Kind = gitChainStepCd
			step.CDTarget = arg
			return step
		}
	case cmd.Name == "git" && cmd.Subcommand == "clone" && !strings.Contains(step.Text, "||"):
		if clone, ok := parseGitCloneSpec(cmd.Args); ok {
			step.Kind = gitChainStepClone
			step.Clone = clone
			return step
		}
	case cmd.Name == "git" && cmd.Subcommand == "checkout" && !strings.Contains(step.Text, "||"):
		if ref, ok := parseGitCheckoutRef(cmd.Args); ok {
			step.Kind = gitChainStepCheckout
			step.CheckoutRef = ref
			return step
		}
	}

	return step
}

func joinGitSteps(steps []gitChainStep) string {
	if len(steps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.Text != "" {
			parts = append(parts, step.Text)
		}
	}
	return strings.Join(parts, " && ")
}

func normalizeGitWorkdir(workdir string) (string, bool) {
	if workdir == "" {
		return "/", true
	}
	return resolveGitPath("/", workdir)
}

func resolveGitPath(baseDir, target string) (string, bool) {
	if target == "" || strings.Contains(target, "$") || strings.Contains(target, "`") || strings.HasPrefix(target, "~") {
		return "", false
	}

	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "/"
	}
	if !path.IsAbs(baseDir) || strings.Contains(baseDir, "$") || strings.Contains(baseDir, "`") || strings.HasPrefix(baseDir, "~") {
		return "", false
	}

	if path.IsAbs(target) {
		return path.Clean(target), true
	}
	return path.Clean(path.Join(baseDir, target)), true
}

func cdTargetsRepo(currentDir, cdTarget, repoPath string) bool {
	resolved, ok := resolveGitPath(currentDir, cdTarget)
	if !ok {
		return false
	}
	return resolved == path.Clean(repoPath)
}

func parseGitCloneSpec(args []string) (*gitCloneSpec, bool) {
	if len(args) == 0 || args[0] != "clone" {
		return nil, false
	}

	spec := &gitCloneSpec{}
	argv := args[1:]
	afterDoubleDash := false

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "" {
			continue
		}

		nextI, nextAfterDoubleDash, handled, ok := consumeGitCloneOption(spec, argv, i, afterDoubleDash)
		if handled {
			if !ok {
				return nil, false
			}
			i = nextI
			afterDoubleDash = nextAfterDoubleDash
			continue
		}

		if spec.Remote == "" {
			spec.Remote = arg
			continue
		}
		if spec.DestinationArg == "" {
			spec.DestinationArg = arg
			continue
		}
		return nil, false
	}

	if spec.Remote == "" {
		return nil, false
	}

	normalized := normalizeGitRemote(spec.Remote)
	if normalized == "" {
		return nil, false
	}

	gitRef, isGit, err := dfgitutil.ParseGitRef(normalized)
	if err != nil || !isGit || gitRef == nil || gitRef.ShortName == "" {
		return nil, false
	}

	spec.NormalizedRemote = normalized
	spec.ShortName = gitRef.ShortName
	return spec, true
}

func consumeGitCloneOption(
	spec *gitCloneSpec,
	argv []string,
	idx int,
	afterDoubleDash bool,
) (
	nextIdx int,
	nextAfterDoubleDash, handled, ok bool,
) {
	arg := argv[idx]
	if afterDoubleDash {
		return idx, afterDoubleDash, false, true
	}

	switch {
	case arg == "--":
		return idx, true, true, true
	case strings.HasPrefix(arg, "--branch="):
		spec.Ref = strings.TrimPrefix(arg, "--branch=")
		return idx, false, true, true
	case arg == "-b" || arg == "--branch":
		if idx+1 >= len(argv) {
			return idx, afterDoubleDash, true, false
		}
		spec.Ref = argv[idx+1]
		return idx + 1, false, true, true
	case strings.HasPrefix(arg, "--depth="):
		return idx, false, true, true
	case arg == "--depth" || arg == "-j" || arg == "--jobs":
		if idx+1 >= len(argv) {
			return idx, afterDoubleDash, true, false
		}
		return idx + 1, false, true, true
	case arg == "--single-branch" || arg == "--no-single-branch" ||
		arg == "--quiet" || arg == "-q" || arg == "--verbose" || arg == "-v" ||
		arg == "--progress":
		return idx, false, true, true
	case arg == "--recursive" || arg == "--recurse-submodules" || arg == "--shallow-submodules":
		spec.Submodules = true
		return idx, false, true, true
	case strings.HasPrefix(arg, "-"):
		return idx, afterDoubleDash, true, false
	default:
		return idx, afterDoubleDash, false, true
	}
}

func parseGitCheckoutRef(args []string) (string, bool) {
	if len(args) == 0 || args[0] != "checkout" {
		return "", false
	}

	argv := args[1:]
	var ref string

	for i := range argv {
		arg := argv[i]
		if arg == "" {
			continue
		}
		switch {
		case strings.HasPrefix(arg, "--"):
			switch arg {
			case "--quiet", "--detach", "--force":
				continue
			default:
				return "", false
			}
		case strings.HasPrefix(arg, "-"):
			switch arg {
			case "-q", "-d", "-f":
				continue
			default:
				return "", false
			}
		default:
			if ref != "" {
				return "", false
			}
			ref = arg
		}
	}

	return ref, ref != ""
}

func normalizeGitRemote(remote string) string {
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "$") && !strings.Contains(remote, ".git") {
		return ""
	}

	u, err := url.Parse(remote)
	if err == nil && (u.Scheme == gitRemoteSchemeHTTP || u.Scheme == gitRemoteSchemeHTTPS) {
		if u.Host == "" || u.Path == "" {
			return ""
		}
		u.Path = strings.TrimSuffix(u.Path, "/")
		if !strings.HasSuffix(u.Path, ".git") {
			u.Path += ".git"
		}
		return u.String()
	}

	return remote
}

func buildGitSourceURL(spec *gitCloneSpec, checkoutRef string) string {
	if spec == nil {
		return ""
	}

	queryParts := make([]string, 0, 3)
	ref := spec.Ref
	if checkoutRef != "" {
		ref = checkoutRef
	}

	if ref != "" {
		// Use `ref=` as the selector. Docker documents `ref` as the generic git
		// query form, and our GitLab HTTP builds rely on that shape (see
		// _tools/shellcheck-wasm/Dockerfile).
		queryParts = append(queryParts, "ref="+ref)
	}
	if spec.Submodules {
		queryParts = append(queryParts, "submodules=true")
	}
	if len(queryParts) > 0 {
		return spec.NormalizedRemote + "?" + strings.Join(queryParts, "&")
	}
	return spec.NormalizedRemote
}

func buildGitSourceChecksum(spec *gitCloneSpec, checkoutRef string) string {
	if spec == nil {
		return ""
	}

	ref := spec.Ref
	if checkoutRef != "" {
		ref = checkoutRef
	}
	if looksLikeGitCommitChecksum(ref) {
		return ref
	}
	return ""
}

func hasGitCommands(script string, variant Variant) bool {
	if strings.TrimSpace(script) == "" {
		return false
	}
	return len(FindCommands(script, variant, "git")) > 0
}

type rawCommandInfo struct {
	Name       string
	Subcommand string
	Args       []string
}

func parseRawSimpleCommand(text string, variant Variant) (rawCommandInfo, bool) {
	if !variant.SupportsPOSIXShellAST() {
		return rawCommandInfo{}, false
	}

	prog, err := parseScript(text, variant)
	if err != nil || prog == nil || len(prog.Stmts) != 1 {
		return rawCommandInfo{}, false
	}

	stmt := prog.Stmts[0]
	if !isSimpleStatement(stmt) {
		return rawCommandInfo{}, false
	}

	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return rawCommandInfo{}, false
	}

	name, ok := renderGitWord(call.Args[0])
	if !ok || name == "" {
		return rawCommandInfo{}, false
	}

	info := rawCommandInfo{
		Name: path.Base(name),
		Args: make([]string, 0, len(call.Args)-1),
	}
	for _, arg := range call.Args[1:] {
		rendered, ok := renderGitWord(arg)
		if !ok || rendered == "" {
			return rawCommandInfo{}, false
		}
		info.Args = append(info.Args, rendered)
		if info.Subcommand == "" && !strings.HasPrefix(rendered, "-") {
			info.Subcommand = rendered
		}
	}

	return info, true
}

func renderGitWord(word *syntax.Word) (string, bool) {
	if word == nil || len(word.Parts) == 0 {
		return "", false
	}

	var sb strings.Builder
	for _, part := range word.Parts {
		rendered, ok := renderGitWordPart(part)
		if !ok {
			return "", false
		}
		sb.WriteString(rendered)
	}
	return sb.String(), true
}

func renderGitWordPart(part syntax.WordPart) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit:
		return p.Value, true
	case *syntax.SglQuoted:
		return p.Value, true
	case *syntax.DblQuoted:
		var sb strings.Builder
		for _, dp := range p.Parts {
			rendered, ok := renderGitWordPart(dp)
			if !ok {
				return "", false
			}
			sb.WriteString(rendered)
		}
		return sb.String(), true
	case *syntax.ParamExp:
		var sb strings.Builder
		printer := syntax.NewPrinter()
		if err := printer.Print(&sb, p); err != nil {
			return "", false
		}
		return sb.String(), true
	default:
		return "", false
	}
}

func canEncodeGitCheckoutRef(ref string) bool {
	if ref == "" {
		return false
	}
	return !looksLikeAmbiguousGitCommitPrefix(ref)
}

func looksLikeGitCommitChecksum(ref string) bool {
	if len(ref) != 40 && len(ref) != 64 {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func looksLikeAmbiguousGitCommitPrefix(ref string) bool {
	if len(ref) < 7 || looksLikeGitCommitChecksum(ref) {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}
