package facts

import (
	"net/http"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// CommandOperationFamily identifies a lifted command family.
type CommandOperationFamily string

const (
	// CommandOperationFamilyHTTPTransfer covers simple curl/wget-style remote transfers.
	CommandOperationFamilyHTTPTransfer CommandOperationFamily = "http-transfer"

	httpTransferToolCurl = "curl"
	httpTransferToolWget = "wget"
)

// CommandOperationStatus reports whether a command was deterministically lifted.
type CommandOperationStatus string

const (
	// CommandOperationLifted means the command was normalized into a family operation.
	CommandOperationLifted CommandOperationStatus = "lifted"
	// CommandOperationBlocked means the command matched a family but failed closed.
	CommandOperationBlocked CommandOperationStatus = "blocked"
)

// CommandOperationBlocker records why deterministic lifting failed.
type CommandOperationBlocker struct {
	Code   string
	Reason string
}

// CommandSourceRange is a source-mapped command replacement window.
// Lines are 1-based Dockerfile lines, columns are 0-based.
type CommandSourceRange struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// HTTPTransferSinkKind describes where a transfer writes its payload.
type HTTPTransferSinkKind string

const (
	HTTPTransferSinkStdout     HTTPTransferSinkKind = "stdout"
	HTTPTransferSinkFile       HTTPTransferSinkKind = "file"
	HTTPTransferSinkRemoteFile HTTPTransferSinkKind = "remote-file"
)

// HTTPTransferOperation is the normalized shape for a simple curl/wget transfer.
type HTTPTransferOperation struct {
	Tool               string
	URL                string
	Method             string
	SinkKind           HTTPTransferSinkKind
	OutputPath         string
	FollowsRedirects   bool
	FailOnHTTPStatus   bool
	Quiet              bool
	ShowErrors         bool
	ProgressSuppressed bool
}

// LiftHTTPTransferOperation normalizes a curl/wget command into an HTTP transfer operation when safe.
func LiftHTTPTransferOperation(cmd shell.CommandInfo) (*HTTPTransferOperation, []CommandOperationBlocker, bool) {
	return liftHTTPTransferOperation(cmd)
}

// HTTPTransferLowerOptions tunes how a lifted transfer is serialized.
type HTTPTransferLowerOptions struct {
	// WindowsTarget signals that the replacement executes on a Windows container,
	// so tool names should be emitted with a .exe suffix (curl.exe, wget.exe).
	WindowsTarget bool
}

// LowerToTool serializes a lifted transfer into a target tool command when safe.
// Additional options may adjust the output for a specific target platform.
func (op *HTTPTransferOperation) LowerToTool(targetTool string, opts ...HTTPTransferLowerOptions) (string, bool) {
	if op == nil {
		return "", false
	}
	var lower HTTPTransferLowerOptions
	if len(opts) > 0 {
		lower = opts[0]
	}
	switch targetTool {
	case httpTransferToolCurl:
		return op.lowerToCurl(lower)
	case httpTransferToolWget:
		return op.lowerToWget(lower)
	default:
		return "", false
	}
}

func httpTransferToolBinary(tool string, opts HTTPTransferLowerOptions) string {
	if opts.WindowsTarget {
		return tool + ".exe"
	}
	return tool
}

// CommandOperationFact stores one family-normalized view of a command.
type CommandOperationFact struct {
	Family       CommandOperationFamily
	Tool         string
	Status       CommandOperationStatus
	Command      shell.CommandInfo
	SourceRange  *CommandSourceRange
	HTTPTransfer *HTTPTransferOperation
	Blockers     []CommandOperationBlocker
}

func buildCommandOperationFacts(
	run *instructions.RunCommand,
	sm *sourcemap.SourceMap,
	shellFacts ShellFacts,
) []CommandOperationFact {
	if run == nil {
		return nil
	}

	script, startLine, hasMappedSource := commandOperationScript(run, sm)
	if script == "" {
		return nil
	}

	commands := shell.FindCommands(script, shellFacts.Variant, httpTransferToolCurl, httpTransferToolWget)
	if len(commands) == 0 {
		return nil
	}

	facts := make([]CommandOperationFact, 0, len(commands))
	for i := range commands {
		cmd := commands[i]
		op, blockers, ok := liftHTTPTransferOperation(cmd)
		fact := CommandOperationFact{
			Family:   CommandOperationFamilyHTTPTransfer,
			Tool:     cmd.Name,
			Command:  cmd,
			Blockers: blockers,
		}
		if ok {
			fact.Status = CommandOperationLifted
			fact.HTTPTransfer = op
		} else {
			fact.Status = CommandOperationBlocked
		}
		if hasMappedSource && cmd.SourceKind == shell.CommandSourceKindDirect && cmd.HasCommandRange {
			fact.SourceRange = &CommandSourceRange{
				StartLine: startLine + cmd.Line,
				StartCol:  cmd.StartCol,
				EndLine:   startLine + cmd.CommandEndLine,
				EndCol:    cmd.CommandEndCol,
			}
		}
		facts = append(facts, fact)
	}

	return facts
}

func commandOperationScript(run *instructions.RunCommand, sm *sourcemap.SourceMap) (string, int, bool) {
	if run == nil {
		return "", 0, false
	}
	if run.PrependShell && len(run.Files) == 0 && sm != nil {
		if script, startLine := dockerfile.RunSourceScript(run, sm); script != "" && startLine > 0 {
			return script, startLine, true
		}
	}
	if len(run.Files) > 0 {
		return run.Files[0].Data, 0, false
	}
	return strings.Join(run.CmdLine, " "), 0, false
}

func liftHTTPTransferOperation(cmd shell.CommandInfo) (*HTTPTransferOperation, []CommandOperationBlocker, bool) {
	switch cmd.Name {
	case httpTransferToolCurl:
		return liftCurlTransfer(cmd)
	case httpTransferToolWget:
		return liftWgetTransfer(cmd)
	default:
		return nil, nil, false
	}
}

type curlLiftState struct {
	url              string
	urlCount         int
	outputPath       string
	expectOutputPath bool
	sinkKind         HTTPTransferSinkKind
	hasRemoteName    bool
	followsRedirects bool
	failOnHTTPStatus bool
	quiet            bool
	showErrorsFlag   bool
	progressSupp     bool
	blockers         []CommandOperationBlocker
}

type wgetLiftState struct {
	url              string
	urlCount         int
	outputPath       string
	expectOutputPath bool
	sinkKind         HTTPTransferSinkKind
	quiet            bool
	progressSupp     bool
	blockers         []CommandOperationBlocker
}

func liftCurlTransfer(cmd shell.CommandInfo) (*HTTPTransferOperation, []CommandOperationBlocker, bool) {
	state := curlLiftState{}
	for i, arg := range cmd.Args {
		curlConsumeArg(&state, arg, commandArgIsLiteral(cmd, i))
	}
	return finalizeCurlLift(state)
}

func curlConsumeArg(state *curlLiftState, arg string, isLiteral bool) {
	if state.expectOutputPath {
		state.expectOutputPath = false
		assignSinkOutput(
			&state.outputPath,
			&state.sinkKind,
			arg,
			isLiteral,
			"curl output path is not a plain shell literal",
			&state.blockers,
		)
		return
	}
	if !isLiteral {
		state.blockers = append(state.blockers, blocker("dynamic-arg", "curl contains a non-literal shell argument"))
		return
	}

	switch {
	case arg == "":
		state.blockers = append(state.blockers, blocker("dynamic-arg", "curl contains a non-literal shell argument"))
	case shell.IsURL(arg):
		state.url = arg
		state.urlCount++
	case arg == "-o" || arg == "--output":
		state.expectOutputPath = true
	case strings.HasPrefix(arg, "--output="):
		assignSinkOutput(
			&state.outputPath,
			&state.sinkKind,
			strings.TrimPrefix(arg, "--output="),
			true,
			"curl output path is not a plain shell literal",
			&state.blockers,
		)
	case strings.HasPrefix(arg, "-o") && len(arg) > 2:
		assignSinkOutput(
			&state.outputPath,
			&state.sinkKind,
			arg[2:],
			true,
			"curl output path is not a plain shell literal",
			&state.blockers,
		)
	case arg == "-O" || arg == "--remote-name":
		state.hasRemoteName = true
		state.sinkKind = HTTPTransferSinkRemoteFile
	case arg == "-L" || arg == "--location":
		state.followsRedirects = true
	case arg == "-s" || arg == "--silent":
		state.quiet = true
		state.progressSupp = true
	case arg == "-S" || arg == "--show-error":
		state.showErrorsFlag = true
	case strings.HasPrefix(arg, "--"):
		state.blockers = append(
			state.blockers,
			blocker("unsupported-flag", "curl uses an unsupported long flag for deterministic conversion"),
		)
	case strings.HasPrefix(arg, "-"):
		applyCurlShortFlags(state, arg)
	default:
		state.blockers = append(state.blockers, blocker("unsupported-arg", "curl has a non-URL positional argument"))
	}
}

func applyCurlShortFlags(state *curlLiftState, arg string) {
	for i := 1; i < len(arg); i++ {
		switch arg[i] {
		case 'f':
			state.failOnHTTPStatus = true
		case 'L':
			state.followsRedirects = true
		case 's':
			state.quiet = true
			state.progressSupp = true
		case 'S':
			state.showErrorsFlag = true
		case 'O':
			state.hasRemoteName = true
			state.sinkKind = HTTPTransferSinkRemoteFile
		case 'o':
			if i+1 < len(arg) {
				assignSinkOutput(
					&state.outputPath,
					&state.sinkKind,
					arg[i+1:],
					true,
					"curl output path is not a plain shell literal",
					&state.blockers,
				)
			} else {
				state.expectOutputPath = true
			}
			return
		default:
			state.blockers = append(
				state.blockers,
				blocker("unsupported-flag", "curl uses an unsupported short flag for deterministic conversion"),
			)
			return
		}
	}
}

func finalizeCurlLift(state curlLiftState) (*HTTPTransferOperation, []CommandOperationBlocker, bool) {
	if state.expectOutputPath {
		state.blockers = append(state.blockers, blocker("missing-output", "curl output flag is missing its value"))
	}
	if state.urlCount != 1 {
		state.blockers = append(state.blockers, blocker("url-count", "curl must have exactly one literal URL"))
	}
	if state.sinkKind == HTTPTransferSinkFile && state.hasRemoteName {
		state.blockers = append(state.blockers, blocker("conflicting-output", "curl mixes explicit output and remote-name output"))
	}
	if state.sinkKind == "" {
		state.sinkKind = HTTPTransferSinkStdout
	}
	if len(state.blockers) > 0 {
		return nil, state.blockers, false
	}

	return &HTTPTransferOperation{
		Tool:               httpTransferToolCurl,
		URL:                state.url,
		Method:             http.MethodGet,
		SinkKind:           state.sinkKind,
		OutputPath:         state.outputPath,
		FollowsRedirects:   state.followsRedirects,
		FailOnHTTPStatus:   state.failOnHTTPStatus,
		Quiet:              state.quiet,
		ShowErrors:         !state.quiet || state.showErrorsFlag,
		ProgressSuppressed: state.progressSupp,
	}, nil, true
}

func liftWgetTransfer(cmd shell.CommandInfo) (*HTTPTransferOperation, []CommandOperationBlocker, bool) {
	state := wgetLiftState{}
	for i, arg := range cmd.Args {
		wgetConsumeArg(&state, arg, commandArgIsLiteral(cmd, i))
	}
	return finalizeWgetLift(state)
}

func wgetConsumeArg(state *wgetLiftState, arg string, isLiteral bool) {
	if state.expectOutputPath {
		state.expectOutputPath = false
		assignWgetOutput(state, arg, isLiteral)
		return
	}
	if !isLiteral {
		state.blockers = append(state.blockers, blocker("dynamic-arg", "wget contains a non-literal shell argument"))
		return
	}

	switch {
	case arg == "":
		state.blockers = append(state.blockers, blocker("dynamic-arg", "wget contains a non-literal shell argument"))
	case shell.IsURL(arg):
		state.url = arg
		state.urlCount++
	case arg == "-O" || arg == "--output-document":
		state.expectOutputPath = true
	case strings.HasPrefix(arg, "--output-document="):
		assignWgetOutput(state, strings.TrimPrefix(arg, "--output-document="), true)
	case strings.HasPrefix(arg, "-O") && len(arg) > 2:
		assignWgetOutput(state, arg[2:], true)
	case arg == "-q" || arg == "--quiet":
		state.quiet = true
		state.progressSupp = true
	case arg == "-nv" || arg == "--no-verbose":
		state.progressSupp = true
	case strings.HasPrefix(arg, "--progress="):
		state.progressSupp = true
	case strings.HasPrefix(arg, "--"):
		state.blockers = append(
			state.blockers,
			blocker("unsupported-flag", "wget uses an unsupported long flag for deterministic conversion"),
		)
	case strings.HasPrefix(arg, "-"):
		applyWgetShortFlags(state, arg)
	default:
		state.blockers = append(state.blockers, blocker("unsupported-arg", "wget has a non-URL positional argument"))
	}
}

func applyWgetShortFlags(state *wgetLiftState, arg string) {
	for i := 1; i < len(arg); i++ {
		switch arg[i] {
		case 'q':
			state.quiet = true
			state.progressSupp = true
		case 'O':
			if i+1 < len(arg) {
				assignWgetOutput(state, arg[i+1:], true)
			} else {
				state.expectOutputPath = true
			}
			return
		default:
			state.blockers = append(
				state.blockers,
				blocker("unsupported-flag", "wget uses an unsupported short flag for deterministic conversion"),
			)
			return
		}
	}
}

func assignWgetOutput(state *wgetLiftState, value string, isLiteral bool) {
	if value == "-" {
		state.outputPath = ""
		state.sinkKind = HTTPTransferSinkStdout
		return
	}
	assignSinkOutput(
		&state.outputPath,
		&state.sinkKind,
		value,
		isLiteral,
		"wget output path is not a plain shell literal",
		&state.blockers,
	)
}

func finalizeWgetLift(state wgetLiftState) (*HTTPTransferOperation, []CommandOperationBlocker, bool) {
	if state.expectOutputPath {
		state.blockers = append(state.blockers, blocker("missing-output", "wget output flag is missing its value"))
	}
	if state.urlCount != 1 {
		state.blockers = append(state.blockers, blocker("url-count", "wget must have exactly one literal URL"))
	}
	if state.sinkKind == "" {
		state.sinkKind = HTTPTransferSinkRemoteFile
	}
	if len(state.blockers) > 0 {
		return nil, state.blockers, false
	}

	return &HTTPTransferOperation{
		Tool:               httpTransferToolWget,
		URL:                state.url,
		Method:             http.MethodGet,
		SinkKind:           state.sinkKind,
		OutputPath:         state.outputPath,
		FollowsRedirects:   true,
		FailOnHTTPStatus:   true,
		Quiet:              state.quiet,
		ShowErrors:         !state.quiet,
		ProgressSuppressed: state.progressSupp,
	}, nil, true
}

func assignSinkOutput(
	outputPath *string,
	sinkKind *HTTPTransferSinkKind,
	value string,
	isLiteral bool,
	reason string,
	blockers *[]CommandOperationBlocker,
) {
	if !isLiteral || !isPlainShellLiteralToken(value) {
		*blockers = append(*blockers, blocker("dynamic-output", reason))
		return
	}
	*outputPath = value
	*sinkKind = HTTPTransferSinkFile
}

func commandArgIsLiteral(cmd shell.CommandInfo, index int) bool {
	if index < 0 || index >= len(cmd.ArgLiteral) {
		return false
	}
	return cmd.ArgLiteral[index]
}

func (op *HTTPTransferOperation) lowerToCurl(opts HTTPTransferLowerOptions) (string, bool) {
	if op == nil || op.Method != http.MethodGet || !op.FollowsRedirects || !op.FailOnHTTPStatus || !isPlainShellLiteralToken(op.URL) {
		return "", false
	}
	if op.SinkKind == HTTPTransferSinkFile && !isPlainShellLiteralToken(op.OutputPath) {
		return "", false
	}

	flags := "-fL"
	if op.Quiet || op.ProgressSuppressed {
		flags = "-fsSL"
	}

	parts := []string{httpTransferToolBinary(httpTransferToolCurl, opts), flags}
	switch op.SinkKind {
	case HTTPTransferSinkFile:
		parts = append(parts, "-o", op.OutputPath)
	case HTTPTransferSinkRemoteFile:
		parts = append(parts, "-O")
	case HTTPTransferSinkStdout:
		// default
	default:
		return "", false
	}
	parts = append(parts, op.URL)
	return strings.Join(parts, " "), true
}

func (op *HTTPTransferOperation) lowerToWget(opts HTTPTransferLowerOptions) (string, bool) {
	if op == nil || op.Method != http.MethodGet || !op.FollowsRedirects || !op.FailOnHTTPStatus || !isPlainShellLiteralToken(op.URL) {
		return "", false
	}
	if op.SinkKind == HTTPTransferSinkFile && !isPlainShellLiteralToken(op.OutputPath) {
		return "", false
	}

	parts := []string{httpTransferToolBinary(httpTransferToolWget, opts)}
	switch {
	case op.Quiet && op.ShowErrors:
		parts = append(parts, "-nv")
	case op.Quiet:
		parts = append(parts, "-q")
	case op.ProgressSuppressed:
		parts = append(parts, "-nv")
	}

	switch op.SinkKind {
	case HTTPTransferSinkFile:
		parts = append(parts, "-O", op.OutputPath)
	case HTTPTransferSinkStdout:
		parts = append(parts, "-O-")
	case HTTPTransferSinkRemoteFile:
		// default remote-name behavior
	default:
		return "", false
	}

	parts = append(parts, op.URL)
	return strings.Join(parts, " "), true
}

func blocker(code, reason string) CommandOperationBlocker {
	return CommandOperationBlocker{Code: code, Reason: reason}
}

func isPlainShellLiteralToken(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	return !strings.ContainsAny(s, " \t\r\n'\"`$&|;()<>\\*?[]{}!")
}
