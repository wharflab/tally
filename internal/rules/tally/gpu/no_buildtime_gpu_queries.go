package gpu

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// NoBuildtimeGPUQueriesRuleCode is the full rule code.
const NoBuildtimeGPUQueriesRuleCode = rules.TallyRulePrefix + "gpu/no-buildtime-gpu-queries"

// gpuQueryCommands are executables that query GPU hardware at runtime.
var gpuQueryCommands = []string{
	"nvidia-smi",
	"nvidia-debugdump",
	"nvidia-persistenced",
}

// gpuQueryPatterns are substrings in script text that indicate runtime GPU
// hardware checks (typically Python expressions passed via -c or heredoc).
var gpuQueryPatterns = []struct {
	pattern string
	label   string
}{
	{"torch.cuda.is_available()", "torch.cuda.is_available()"},
	{"torch.cuda.device_count()", "torch.cuda.device_count()"},
	{"torch.cuda.get_device_name(", "torch.cuda.get_device_name()"},
	{"torch.cuda.current_device()", "torch.cuda.current_device()"},
	{"tf.test.is_gpu_available(", "tf.test.is_gpu_available()"},
	{"tf.config.list_physical_devices(", "tf.config.list_physical_devices()"},
}

// NoBuildtimeGPUQueriesRule flags RUN instructions that query GPU hardware
// at build time. GPU devices are not available during docker build, so
// commands like nvidia-smi or torch.cuda.is_available() will fail or
// return misleading results.
type NoBuildtimeGPUQueriesRule struct{}

// NewNoBuildtimeGPUQueriesRule creates a new rule instance.
func NewNoBuildtimeGPUQueriesRule() *NoBuildtimeGPUQueriesRule {
	return &NoBuildtimeGPUQueriesRule{}
}

// Metadata returns the rule metadata.
func (r *NoBuildtimeGPUQueriesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoBuildtimeGPUQueriesRuleCode,
		Name:            "No build-time GPU queries",
		Description:     "GPU hardware is not available during docker build; runtime GPU queries in RUN will fail or return misleading results",
		DocURL:          rules.TallyDocURL(NoBuildtimeGPUQueriesRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

// Check runs the rule against the given input.
func (r *NoBuildtimeGPUQueriesRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	var fileFacts = input.Facts
	if fileFacts != nil {
		return r.checkWithFacts(input, fileFacts, meta)
	}

	// Fallback: iterate stages directly when facts are unavailable.
	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			script := strings.Join(run.CmdLine, " ")
			if v, ok := r.checkRun(input.File, stageIdx, run, script, shell.VariantUnknown, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

func (r *NoBuildtimeGPUQueriesRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, stageFacts := range fileFacts.Stages() {
		for _, runFacts := range stageFacts.Runs {
			variant := runFacts.Shell.Variant
			script := runFacts.SourceScript
			if script == "" {
				script = strings.Join(runFacts.Run.CmdLine, " ")
			}
			if v, ok := r.checkRun(input.File, stageFacts.Index, runFacts.Run, script, variant, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

func (r *NoBuildtimeGPUQueriesRule) checkRun(
	file string,
	stageIdx int,
	run *instructions.RunCommand,
	script string,
	variant shell.Variant,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	var matched []string
	seen := make(map[string]bool)

	// Check for GPU query commands via shell AST parsing.
	for _, cmd := range gpuQueryCommands {
		if shell.ContainsCommandWithVariant(script, cmd, variant) {
			if !seen[cmd] {
				seen[cmd] = true
				matched = append(matched, cmd)
			}
		}
	}

	// Check framework GPU query patterns only when a Python-capable executor
	// is invoked. This avoids false positives from echo, comments, or docs
	// that mention torch.cuda.is_available() without actually running it.
	if scriptInvokesPython(script, variant) {
		for _, p := range gpuQueryPatterns {
			if strings.Contains(script, p.pattern) {
				if !seen[p.label] {
					seen[p.label] = true
					matched = append(matched, p.label)
				}
			}
		}
	}

	if len(matched) == 0 {
		return rules.Violation{}, false
	}

	loc := rules.NewLocationFromRanges(file, run.Location())
	// Guard against RUN instructions with missing or degenerate source ranges
	// (e.g. synthesized nodes); NewLocationFromRanges returns a file-level
	// location when no usable range is available.
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

	queryList := strings.Join(matched, ", ")
	message := "GPU hardware query at build time will fail: " + queryList
	detail := "GPU devices are not available during docker build. " +
		"Commands like nvidia-smi and runtime framework checks like torch.cuda.is_available() " +
		"will fail or return misleading results. Move these checks to runtime (CMD, ENTRYPOINT, " +
		"or an initialization script)."

	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = stageIdx
	return v, true
}

// pythonExecutors are commands that can execute Python code (directly,
// via inline -c, heredoc, or script file).
var pythonExecutors = []string{
	"python",
	"python3",
	"python2",
	"uv",
}

// scriptInvokesPython returns true if the shell script invokes a Python-capable
// executor, meaning any torch.cuda / tf patterns found in the script text are
// likely to be executed rather than echoed or commented.
func scriptInvokesPython(script string, variant shell.Variant) bool {
	for _, exe := range pythonExecutors {
		if shell.ContainsCommandWithVariant(script, exe, variant) {
			return true
		}
	}
	return false
}

func init() {
	rules.Register(NewNoBuildtimeGPUQueriesRule())
}
