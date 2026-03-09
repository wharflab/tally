package tally

import (
	"net"
	"net/url"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/hadolint"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// CurlMissingLocationRuleCode is the full rule code for the curl-missing-location rule.
const CurlMissingLocationRuleCode = rules.TallyRulePrefix + "curl-missing-location"

// CurlMissingLocationRule detects curl commands in RUN instructions that are
// missing the -L/--location flag to follow HTTP redirects.
//
// Other Dockerfile download mechanisms (ADD, wget) follow redirects by default.
// Without -L, curl will not follow redirects, which can cause downloads to
// silently fail when URLs are relocated.
type CurlMissingLocationRule struct{}

// NewCurlMissingLocationRule creates a new rule instance.
func NewCurlMissingLocationRule() *CurlMissingLocationRule {
	return &CurlMissingLocationRule{}
}

// Metadata returns the rule metadata.
func (r *CurlMissingLocationRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            CurlMissingLocationRuleCode,
		Name:            "curl should use --location to follow redirects",
		Description:     "curl commands should include -L/--location to follow HTTP redirects",
		DocURL:          rules.TallyDocURL(CurlMissingLocationRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the curl-missing-location rule.
func (r *CurlMissingLocationRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	return hadolint.ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			// Use RunSourceScript for shell-form RUNs so that FindCommands
			// positions map directly to source columns (enables accurate fixes).
			// Fall back to RunCommandString for exec-form (detection only).
			var cmds []shell.CommandInfo
			var runStartLine int

			if run.PrependShell && sm != nil {
				script, startLine := dockerfile.RunSourceScript(run, sm)
				if script == "" {
					return nil
				}
				runStartLine = startLine
				cmds = shell.FindCommands(script, shellVariant, "curl")
			} else {
				cmdStr := dockerfile.RunCommandString(run)
				cmds = shell.FindCommands(cmdStr, shellVariant, "curl")
			}

			var violations []rules.Violation
			for i := range cmds {
				cmd := &cmds[i]

				if cmd.HasAnyFlag("-L", "--location", "--location-trusted") {
					continue
				}

				if curlIsNonTransfer(cmd) || curlTargetsOnlyIPs(cmd) {
					continue
				}

				// Anchor the violation to the specific curl command when source
				// positions are available, not the whole RUN instruction.
				var loc rules.Location
				if runStartLine > 0 {
					cmdLine := runStartLine + cmd.Line
					loc = rules.NewRangeLocation(file, cmdLine, cmd.StartCol, cmdLine, cmd.EndCol)
				} else {
					loc = rules.NewLocationFromRanges(file, run.Location())
				}
				v := rules.NewViolation(
					loc, meta.Code,
					"curl command is missing --location flag to follow HTTP redirects",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"Without -L/--location, curl will not follow HTTP redirects (301, 302, 307, 308). " +
						"This can cause downloads to silently fail when URLs are relocated. " +
						"Other Dockerfile download mechanisms (ADD, wget) follow redirects by default.",
				)

				if fix := buildCurlLocationFix(file, run, *cmd, runStartLine, sm); fix != nil {
					v = v.WithSuggestedFix(fix)
				}

				violations = append(violations, v)
			}

			return violations
		},
	)
}

// curlIsNonTransfer returns true if the curl command is a non-transfer invocation
// (e.g., --help, --version, --manual) where --location has no effect.
func curlIsNonTransfer(cmd *shell.CommandInfo) bool {
	return cmd.HasAnyFlag("-h", "--help", "-V", "--version", "-M", "--manual")
}

// curlTargetsOnlyIPs returns true if every URL argument in the curl command
// points to an IP address (e.g. http://127.0.0.1, http://10.0.0.1:8080).
// Returns false when no URLs are found, causing the rule to still fire.
func curlTargetsOnlyIPs(cmd *shell.CommandInfo) bool {
	hasURL := false
	for _, arg := range cmd.Args {
		u, err := url.Parse(arg)
		if err != nil {
			continue
		}
		switch u.Scheme {
		case "http", "https", "ftp":
			// ok
		default:
			continue
		}
		if u.Host == "" {
			continue
		}
		hasURL = true
		if !isIPHost(u.Host) {
			return false
		}
	}
	return hasURL
}

// isIPHost checks if a host (with optional port) is an IP address.
func isIPHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	// Strip surrounding brackets for IPv6 addresses like [::1]
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return net.ParseIP(h) != nil
}

// buildCurlLocationFix creates a SuggestedFix that inserts " --location" after
// the curl command name. Uses cmd.EndCol from FindCommands on RunSourceScript,
// which maps directly to source columns. Only works for shell-form RUN
// instructions parsed via RunSourceScript (runStartLine > 0).
func buildCurlLocationFix(
	file string,
	run *instructions.RunCommand,
	cmd shell.CommandInfo,
	runStartLine int,
	sm *sourcemap.SourceMap,
) *rules.SuggestedFix {
	if sm == nil || !run.PrependShell || runStartLine == 0 {
		return nil
	}

	editLine := runStartLine + cmd.Line
	insertCol := cmd.EndCol

	// Validate the position points to the end of "curl" in the source.
	lineIdx := editLine - 1
	if lineIdx < 0 || lineIdx >= sm.LineCount() {
		return nil
	}
	sourceLine := sm.Line(lineIdx)
	if insertCol < 0 || insertCol > len(sourceLine) {
		return nil
	}

	return &rules.SuggestedFix{
		Description: "Add --location flag to follow redirects",
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, editLine, insertCol, editLine, insertCol),
			NewText:  " --location",
		}},
	}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewCurlMissingLocationRule())
}
