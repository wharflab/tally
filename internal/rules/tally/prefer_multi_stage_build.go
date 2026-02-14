package tally

import (
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/ai/autofixdata"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
)

// PreferMultiStageBuildConfig configures the prefer-multi-stage-build rule.
type PreferMultiStageBuildConfig struct {
	// MinScore is the minimum heuristic score required to trigger the suggestion.
	MinScore *int `json:"min-score,omitempty" jsonschema:"description=Minimum score required to trigger,default=4,minimum=1" koanf:"min-score"`
}

func defaultPreferMultiStageBuildConfig() PreferMultiStageBuildConfig {
	minScore := 4
	return PreferMultiStageBuildConfig{MinScore: &minScore}
}

// PreferMultiStageBuildRule detects likely single-stage build+runtime Dockerfiles.
type PreferMultiStageBuildRule struct{}

func NewPreferMultiStageBuildRule() *PreferMultiStageBuildRule { return &PreferMultiStageBuildRule{} }

func (r *PreferMultiStageBuildRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "prefer-multi-stage-build",
		Name:            "Prefer Multi-Stage Build",
		Description:     "Suggests converting single-stage builds into multi-stage builds to reduce final image size",
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/tally/prefer-multi-stage-build.md",
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		IsExperimental:  true,
		FixPriority:     150, // Whole-file rewrite should run after other structural transforms.
	}
}

func (r *PreferMultiStageBuildRule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"min-score": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"default":     4,
				"description": "Minimum score required to trigger",
			},
		},
		"additionalProperties": false,
	}
}

func (r *PreferMultiStageBuildRule) DefaultConfig() any { return defaultPreferMultiStageBuildConfig() }

func (r *PreferMultiStageBuildRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

func (r *PreferMultiStageBuildRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	minScore := 4
	if cfg.MinScore != nil {
		minScore = *cfg.MinScore
	}
	if minScore < 1 {
		minScore = 1
	}

	// Trigger only on exactly one real FROM.
	if len(input.Stages) != 1 {
		// Stages should be 1 for single-FROM, but keep a defensive check.
		return nil
	}
	if strings.TrimSpace(input.Stages[0].SourceCode) == "" {
		// Parse may synthesize a dummy stage (FROM scratch) to continue linting;
		// require a real stage definition.
		return nil
	}

	score, signals := scoreStage(input.Stages[0])
	if score < minScore {
		return nil
	}

	meta := r.Metadata()
	loc := rules.NewFileLocation(input.File)

	detail := buildDetail(signals)

	return []rules.Violation{
		rules.NewViolation(
			loc,
			meta.Code,
			"This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size.",
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL).WithDetail(detail).WithSuggestedFix(&rules.SuggestedFix{
			Description:  "AI AutoFix: convert to multi-stage build",
			Safety:       rules.FixUnsafe,
			NeedsResolve: true,
			ResolverID:   autofixdata.ResolverID,
			ResolverData: &autofixdata.MultiStageResolveData{
				File:    input.File,
				Score:   score,
				Signals: signals,
			},
			Priority: meta.FixPriority,
		}),
	}
}

func (r *PreferMultiStageBuildRule) resolveConfig(config any) PreferMultiStageBuildConfig {
	return configutil.Coerce(config, defaultPreferMultiStageBuildConfig())
}

func buildDetail(signals []autofixdata.Signal) string {
	if len(signals) == 0 {
		return "Detected build-time behavior that could be moved into a builder stage."
	}
	var b strings.Builder
	b.WriteString("Signals:\n")
	limit := min(len(signals), 3)
	for i := range limit {
		s := signals[i]
		b.WriteString("- ")
		b.WriteString(string(s.Kind))
		if s.Line > 0 {
			b.WriteString(" (line ")
			b.WriteString(strconv.Itoa(s.Line))
			b.WriteString(")")
		}
		if s.Evidence != "" {
			b.WriteString(": ")
			b.WriteString(s.Evidence)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func scoreStage(stage instructions.Stage) (int, []autofixdata.Signal) {
	score := 0
	signals := make([]autofixdata.Signal, 0, 8)

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok {
			continue
		}
		script := runScript(run)
		if script == "" {
			continue
		}
		line := 0
		if loc := run.Location(); len(loc) > 0 {
			line = loc[0].Start.Line
		}
		evidence := strings.TrimSpace(run.String())

		if s, pts, ok := detectPackageInstall(script, evidence, line); ok {
			signals = append(signals, s)
			score += pts
			continue
		}
		if s, pts, ok := detectBuildStep(script, evidence, line); ok {
			signals = append(signals, s)
			score += pts
			continue
		}
		if s, pts, ok := detectDownloadInstall(script, evidence, line); ok {
			signals = append(signals, s)
			score += pts
			continue
		}
	}

	return score, signals
}

func runScript(run *instructions.RunCommand) string {
	if len(run.Files) > 0 {
		return run.Files[0].Data
	}
	return strings.Join(run.CmdLine, " ")
}

func detectPackageInstall(script, evidence string, line int) (autofixdata.Signal, int, bool) {
	lower := strings.ToLower(script)

	type mgr struct {
		name  string
		kw    string
		score int
	}
	managers := []mgr{
		{name: "apt-get", kw: "apt-get install", score: 4},
		{name: "apt", kw: "apt install", score: 3},
		{name: "apk", kw: "apk add", score: 4},
		{name: "dnf", kw: "dnf install", score: 4},
		{name: "yum", kw: "yum install", score: 4},
	}

	for _, m := range managers {
		if !strings.Contains(lower, m.kw) {
			continue
		}
		pkgs := extractPackagesAfter(lower, m.kw)
		pts := m.score
		pts += buildToolBonus(pkgs)
		return autofixdata.Signal{
			Kind:     autofixdata.SignalKindPackageInstall,
			Manager:  m.name,
			Packages: pkgs,
			Evidence: evidence,
			Line:     line,
		}, pts, true
	}
	return autofixdata.Signal{}, 0, false
}

func detectBuildStep(script, evidence string, line int) (autofixdata.Signal, int, bool) {
	lower := strings.ToLower(script)
	type tool struct {
		name  string
		kw    string
		score int
	}
	tools := []tool{
		{name: "go", kw: "go build", score: 4},
		{name: "cargo", kw: "cargo build", score: 4},
		{name: "npm", kw: "npm run build", score: 3},
		{name: "yarn", kw: "yarn build", score: 3},
		{name: "pnpm", kw: "pnpm build", score: 3},
		{name: "dotnet", kw: "dotnet publish", score: 4},
		{name: "mvn", kw: "mvn package", score: 4},
		{name: "gradle", kw: "gradle build", score: 4},
		{name: "make", kw: "make ", score: 2},
		{name: "cmake", kw: "cmake ", score: 2},
		{name: "ninja", kw: "ninja ", score: 2},
	}
	for _, t := range tools {
		if !strings.Contains(lower, t.kw) {
			continue
		}
		return autofixdata.Signal{
			Kind:     autofixdata.SignalKindBuildStep,
			Tool:     t.name,
			Evidence: evidence,
			Line:     line,
		}, t.score, true
	}
	return autofixdata.Signal{}, 0, false
}

func detectDownloadInstall(script, evidence string, line int) (autofixdata.Signal, int, bool) {
	lower := strings.ToLower(script)
	if !strings.Contains(lower, "curl ") && !strings.Contains(lower, "wget ") {
		return autofixdata.Signal{}, 0, false
	}
	// Common "download and install" patterns.
	if !strings.Contains(lower, "| tar") && !strings.Contains(lower, "| sh") && !strings.Contains(lower, "| bash") {
		return autofixdata.Signal{}, 0, false
	}
	return autofixdata.Signal{
		Kind:     autofixdata.SignalKindDownloadInstall,
		Evidence: evidence,
		Line:     line,
	}, 2, true
}

func extractPackagesAfter(lower, kw string) []string {
	_, after, ok := strings.Cut(lower, kw)
	if !ok {
		return nil
	}
	rest := after
	// Stop at shell control operators.
	for _, stop := range []string{"&&", ";", "|"} {
		if sidx := strings.Index(rest, stop); sidx >= 0 {
			rest = rest[:sidx]
		}
	}
	fields := strings.Fields(rest)
	pkgs := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "-") {
			continue
		}
		// Common package-manager glue tokens.
		if f == "\\" || f == "--no-cache" {
			continue
		}
		pkgs = append(pkgs, strings.TrimSpace(f))
		if len(pkgs) >= 12 {
			break
		}
	}
	return pkgs
}

func buildToolBonus(pkgs []string) int {
	if len(pkgs) == 0 {
		return 0
	}
	buildish := map[string]struct{}{
		"build-essential": {},
		"gcc":             {},
		"g++":             {},
		"make":            {},
		"cmake":           {},
		"musl-dev":        {},
		"libc-dev":        {},
		"pkg-config":      {},
		"python3-dev":     {},
		"openssl-dev":     {},
	}
	bonus := 0
	for _, p := range pkgs {
		if _, ok := buildish[p]; ok {
			bonus += 2
		}
	}
	// Treat git as a weaker signal.
	if bonus == 0 {
		if slices.Contains(pkgs, "git") {
			bonus = 1
		}
	}
	if bonus > 6 {
		bonus = 6
	}
	return bonus
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferMultiStageBuildRule())
}
