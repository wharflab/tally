package cmd

import (
	"bytes"
	"cmp"
	stdcontext "context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/wharflab/tally/internal/ai/autofix"
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/discovery"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/fileval"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/invocation"
	"github.com/wharflab/tally/internal/linter"
	"github.com/wharflab/tally/internal/processor"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/reporter"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/syntax"
	"github.com/wharflab/tally/internal/version"
)

// Exit codes
const (
	ExitSuccess     = 0 // No violations (or below fail-level threshold)
	ExitViolations  = 1 // Violations found at or above fail-level
	ExitConfigError = 2 // Parse or config error
	ExitNoFiles     = 3 // No Dockerfiles found (missing file, empty glob, empty directory)
	ExitSyntaxError = 4 // Dockerfile has fatal syntax issues (unknown instructions, malformed directives)
)

const installPowerShellURL = "https://learn.microsoft.com/en-us/powershell/scripting/install/install-powershell"

func lintCommand() *cobra.Command {
	return newLintCommand(&lintOptions{})
}

func newLintCommand(opts *lintOptions) *cobra.Command {
	if opts == nil {
		opts = &lintOptions{}
	}
	cmd := &cobra.Command{
		Use:   "lint [DOCKERFILE...]",
		Short: "Lint Dockerfile(s) for issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.flags = cmd.Flags()
			if err := finalizeLintOptions(cmd.Flags(), opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return exitWith(ExitConfigError)
			}
			return runLintWithPowerShellReporter(cmd.Context(), opts, args)
		},
	}

	addLintFlags(cmd.Flags(), opts)
	return cmd
}

func runLintWithPowerShellReporter(ctx stdcontext.Context, opts *lintOptions, args []string) error {
	defer installPowerShellUnavailableReporter(os.Stderr)()
	return runLint(ctx, opts, args)
}

func installPowerShellUnavailableReporter(w io.Writer) func() {
	var once sync.Once
	return psanalyzer.SetUnavailableReporter(func(event psanalyzer.UnavailableEvent) {
		once.Do(func() {
			detail := ""
			if event.Err != nil {
				detail = strings.TrimSpace(event.Err.Error())
			}
			if detail != "" {
				detail = ": " + detail
			}
			fmt.Fprintf(
				w,
				"note: PowerShell script linting/formatting skipped%s. "+
					"Requires a usable PowerShell 7+ installation and PSScriptAnalyzer; install PowerShell: %s\n",
				detail,
				installPowerShellURL,
			)
		})
	})
}

// lintResults holds the aggregated results of linting all discovered files.
type lintResults struct {
	violations         []rules.Violation
	asyncPlans         []async.CheckRequest
	fileSources        map[string][]byte
	fileConfigs        map[string]*config.Config
	fileInvocations    map[string]*invocation.BuildInvocation
	firstCfg           *config.Config
	filesScanned       int
	invocationsScanned int
}

type applyFixesInput struct {
	violations      []rules.Violation
	sources         map[string][]byte
	fileConfigs     map[string]*config.Config
	fileInvocations map[string]*invocation.BuildInvocation
	asyncPlans      []async.CheckRequest
	asyncResult     *async.RunResult
}

func collectRegistryInsights(
	plans []async.CheckRequest,
	result *async.RunResult,
) map[string][]autofixdata.RegistryInsight {
	if result == nil || len(result.Resolved) == 0 || len(plans) == 0 {
		return nil
	}

	byFile := make(map[string]map[string]autofixdata.RegistryInsight)

	for _, req := range plans {
		if req.ResolverID != registry.RegistryResolverID() {
			continue
		}

		data, ok := req.Data.(*registry.ResolveRequest)
		if !ok || data == nil {
			continue
		}

		resolved, ok := result.Resolved[async.ResolutionKey{ResolverID: req.ResolverID, Key: req.Key}]
		if !ok {
			continue
		}

		fileKey := filepath.ToSlash(req.File)
		if fileKey == "" {
			continue
		}

		stageKey := strconv.Itoa(req.StageIndex) + "|" + req.Key
		insight := autofixdata.RegistryInsight{
			StageIndex:         req.StageIndex,
			Ref:                data.Ref,
			RequestedPlatform:  data.Platform,
			ResolvedPlatform:   "",
			Digest:             "",
			AvailablePlatforms: nil,
		}

		switch v := resolved.(type) {
		case *registry.ImageConfig:
			if v != nil {
				insight.ResolvedPlatform = formatPlatformParts(v.OS, v.Arch, v.Variant)
				insight.Digest = v.Digest
			}
		case *registry.PlatformMismatchError:
			if v != nil && len(v.Available) > 0 {
				insight.AvailablePlatforms = append([]string(nil), v.Available...)
			}
		}

		m := byFile[fileKey]
		if m == nil {
			m = make(map[string]autofixdata.RegistryInsight)
			byFile[fileKey] = m
		}
		m[stageKey] = insight
	}

	if len(byFile) == 0 {
		return nil
	}

	out := make(map[string][]autofixdata.RegistryInsight, len(byFile))
	for file, m := range byFile {
		list := make([]autofixdata.RegistryInsight, 0, len(m))
		for _, ins := range m {
			list = append(list, ins)
		}
		slices.SortFunc(list, func(a, b autofixdata.RegistryInsight) int {
			if d := cmp.Compare(a.StageIndex, b.StageIndex); d != 0 {
				return d
			}
			if d := strings.Compare(a.Ref, b.Ref); d != 0 {
				return d
			}
			return strings.Compare(a.RequestedPlatform, b.RequestedPlatform)
		})
		out[file] = list
	}

	return out
}

func formatPlatformParts(osName, arch, variant string) string {
	s := osName + "/" + arch
	if variant != "" {
		s += "/" + variant
	}
	return s
}

// stdinPath is the synthetic file path used for violations when reading from stdin.
const stdinPath = "<stdin>"

// runLint is the action handler for the lint command.
func runLint(ctx stdcontext.Context, opts *lintOptions, args []string) error {
	inputs := args
	if len(inputs) == 0 {
		inputs = []string{"."}
	}

	// Detect stdin mode (- as input).
	if err := checkStdinInput(inputs); err != nil {
		return err
	}
	if slices.Contains(inputs, "-") {
		return runLintStdin(ctx, opts)
	}

	orchestrator, classified, err := classifyLintEntrypoint(ctx, inputs, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWith(ExitConfigError)
	}
	if classified {
		if orchestrator != nil {
			return runLintOrchestrator(ctx, opts, orchestrator)
		}
		if hasOrchestratorSelectionFlags(opts) {
			fmt.Fprintf(os.Stderr, "Error: --target and --service are only valid for orchestrator entrypoints\n")
			return exitWith(ExitConfigError)
		}
	} else if err := rejectMixedOrchestratorInputs(inputs, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWith(ExitConfigError)
	}

	// Discover files using the discovery package
	discoveryOpts := discovery.Options{
		Patterns:        discovery.DefaultPatterns(),
		ExcludePatterns: opts.exclude,
		ContextDir:      opts.contextDir,
	}

	discovered, err := discovery.Discover(inputs, discoveryOpts)
	if err != nil {
		var notFound *discovery.FileNotFoundError
		if errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", notFound)
			return exitWith(ExitNoFiles)
		}
		fmt.Fprintf(os.Stderr, "Error: failed to discover files: %v\n", err)
		return exitWith(ExitConfigError)
	}

	if len(discovered) == 0 {
		reportNoFilesFound(inputs)
		return exitWith(ExitNoFiles)
	}

	// Lint all discovered files
	res, err := lintFiles(ctx, discovered, opts)
	if err != nil {
		return handleLintError(err)
	}

	asyncResult, asyncPlans := resolveAsyncChecks(ctx, res)

	allViolations := processViolations(res, res.firstCfg)

	warnFixUnsafe(opts)
	if opts.fix {
		fixResult, fixErr := applyFixes(ctx, opts, applyFixesInput{
			violations:      allViolations,
			sources:         res.fileSources,
			fileConfigs:     res.fileConfigs,
			fileInvocations: res.fileInvocations,
			asyncPlans:      asyncPlans,
			asyncResult:     asyncResult,
		})
		if fixErr != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to apply fixes: %v\n", fixErr)
			return exitWith(ExitConfigError)
		}

		if err := writeFixedFiles(fixResult); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitWith(ExitConfigError)
		}

		if fixResult.TotalApplied() > 0 {
			fmt.Fprintf(os.Stderr, "Fixed %d issues in %d files\n",
				fixResult.TotalApplied(), fixResult.FilesModified())
		}
		if fixResult.TotalSkipped() > 0 {
			fmt.Fprintf(os.Stderr, "Skipped %d fixes\n", fixResult.TotalSkipped())
			reportSkippedFixes(fixResult)
		}

		allViolations = filterFixedViolations(allViolations, fixResult, res.fileConfigs)
	}

	return writeReport(opts, res.firstCfg, allViolations, res.fileSources, len(discovered), 0)
}

// resolveAsyncChecks executes async check plans if enabled and merges the
// results into res.violations. Returns the async result and filtered plans
// needed by the fix pipeline.
func resolveAsyncChecks(ctx stdcontext.Context, res *lintResults) (*async.RunResult, []async.CheckRequest) {
	if len(res.asyncPlans) == 0 {
		return nil, nil
	}
	asyncResult, asyncPlans := runAsyncChecks(ctx, res)
	if asyncResult != nil {
		res.violations = linter.MergeAsyncViolations(res.violations, asyncResult)
	}
	return asyncResult, asyncPlans
}

// warnFixUnsafe emits a warning when --fix-unsafe is set without --fix.
func warnFixUnsafe(opts *lintOptions) {
	if opts.fixUnsafe && !opts.fix {
		fmt.Fprintf(os.Stderr, "Warning: --fix-unsafe has no effect without --fix\n")
	}
}

// checkStdinInput returns an error if stdin (-) is mixed with other file arguments.
func checkStdinInput(inputs []string) error {
	if slices.Contains(inputs, "-") && len(inputs) > 1 {
		fmt.Fprintf(os.Stderr, "Error: cannot mix stdin (-) with file arguments\n")
		return exitWith(ExitConfigError)
	}
	return nil
}

// runLintStdin handles the stdin code path: read from stdin, lint, and either
// report diagnostics (no --fix) or write fixed content to stdout (--fix).
func runLintStdin(ctx stdcontext.Context, opts *lintOptions) error {
	if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
		fmt.Fprintf(os.Stderr, "Warning: reading from terminal; use Ctrl+D to end input or pipe a Dockerfile\n")
	}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to read stdin: %v\n", err)
		return exitWith(ExitConfigError)
	}
	if len(content) == 0 {
		fmt.Fprintf(os.Stderr, "Error: empty input from stdin\n")
		return exitWith(ExitNoFiles)
	}

	res, cfg, err := lintStdinContent(opts, content)
	if err != nil {
		return err
	}

	asyncResult, asyncPlans := resolveAsyncChecks(ctx, res)

	allViolations := processViolations(res, cfg)

	warnFixUnsafe(opts)
	if opts.fix {
		return applyStdinFixes(ctx, opts, content, allViolations, res, asyncPlans, asyncResult)
	}
	return writeReport(opts, cfg, allViolations, res.fileSources, 1, 0)
}

// lintStdinContent parses and lints content read from stdin.
func lintStdinContent(opts *lintOptions, content []byte) (*lintResults, *config.Config, error) {
	// Load config from CWD (stdin has no file path for cascading discovery).
	cfg, err := loadConfigForFile(opts, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		return nil, nil, exitWith(ExitConfigError)
	}
	validateAIConfig(cfg, stdinPath)
	validateDurationConfigs(cfg, stdinPath)

	parseResult, err := dockerfile.Parse(bytes.NewReader(content), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to parse stdin: %v\n", err)
		return nil, nil, exitWith(ExitConfigError)
	}

	if syntaxErrors := syntax.Check(stdinPath, parseResult.AST, parseResult.Source); len(syntaxErrors) > 0 {
		for _, e := range syntaxErrors {
			fmt.Fprintf(os.Stderr, "Error: %s\n", e.Error())
		}
		return nil, nil, exitWith(ExitSyntaxError)
	}

	inv := invocationFromContextFlag(stdinPath, opts.contextDir)
	result, err := linter.LintFile(linter.Input{
		FilePath:    stdinPath,
		Config:      cfg,
		ParseResult: parseResult,
		Invocation:  inv,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to lint stdin: %v\n", err)
		return nil, nil, exitWith(ExitConfigError)
	}

	res := &lintResults{
		violations:  result.Violations,
		asyncPlans:  result.AsyncPlan,
		fileSources: map[string][]byte{stdinPath: result.ParseResult.Source},
		fileConfigs: map[string]*config.Config{stdinPath: cfg},
		firstCfg:    cfg,
	}
	if inv != nil {
		res.fileInvocations = make(map[string]*invocation.BuildInvocation, 1)
		addFileInvocation(res.fileInvocations, inv)
	}
	return res, cfg, nil
}

// processViolations runs the processor chain on raw violations.
func processViolations(res *lintResults, cfg *config.Config) []rules.Violation {
	chain, inlineFilter := linter.CLIProcessors()
	procCtx := processor.NewContext(res.fileConfigs, cfg, res.fileSources)
	allViolations := chain.Process(res.violations, procCtx)

	additionalViolations := inlineFilter.AdditionalViolations()
	if len(additionalViolations) > 0 {
		additionalViolations = processor.NewPathNormalization().Process(additionalViolations, procCtx)
		additionalViolations = processor.NewSnippetAttachment().Process(additionalViolations, procCtx)
		allViolations = append(allViolations, additionalViolations...)
		allViolations = reporter.SortViolations(allViolations)
	}
	return allViolations
}

// applyStdinFixes applies fixes and writes the result to stdout.
func applyStdinFixes(
	ctx stdcontext.Context, opts *lintOptions,
	content []byte, allViolations []rules.Violation,
	res *lintResults, asyncPlans []async.CheckRequest, asyncResult *async.RunResult,
) error {
	fixResult, fixErr := applyFixes(ctx, opts, applyFixesInput{
		violations:      allViolations,
		sources:         res.fileSources,
		fileConfigs:     res.fileConfigs,
		fileInvocations: res.fileInvocations,
		asyncPlans:      asyncPlans,
		asyncResult:     asyncResult,
	})
	if fixErr != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to apply fixes: %v\n", fixErr)
		return exitWith(ExitConfigError)
	}

	// Write fixed content (or original if unchanged) to stdout.
	outputContent := content
	if fc, ok := fixResult.Changes[filepath.Clean(stdinPath)]; ok && fc.HasChanges() {
		outputContent = fc.ModifiedContent
	}
	if _, err := os.Stdout.Write(outputContent); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to write output: %v\n", err)
		return exitWith(ExitConfigError)
	}

	if fixResult.TotalApplied() > 0 {
		fmt.Fprintf(os.Stderr, "Fixed %d issues\n", fixResult.TotalApplied())
	}
	if fixResult.TotalSkipped() > 0 {
		fmt.Fprintf(os.Stderr, "Skipped %d fixes\n", fixResult.TotalSkipped())
		reportSkippedFixes(fixResult)
	}

	allViolations = filterFixedViolations(allViolations, fixResult, res.fileConfigs)

	// With --fix and stdin, stdout carries the fixed Dockerfile content.
	// Redirect the violation report to stderr unless the user explicitly
	// chose a different output destination (--output or config).
	cfg := res.firstCfg
	outCfg := getOutputConfig(opts, cfg)
	reportPath := outCfg.path
	if reportPath == "" || reportPath == "stdout" {
		reportPath = "stderr"
		if opts.flags != nil && opts.flags.Changed("output") {
			fmt.Fprintf(os.Stderr, "note: --output overridden to stderr in stdin fix mode (stdout carries fixed content)\n")
		}
	}
	return writeReportTo(opts, cfg, allViolations, res.fileSources, 1, 0, reportPath)
}

func runLintOrchestrator(ctx stdcontext.Context, opts *lintOptions, discovered *invocation.DiscoveryResult) error {
	if err := validateOrchestratorFlags(opts, discovered.Kind); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWith(ExitConfigError)
	}

	if len(discovered.Invocations) == 0 {
		cfg, err := loadConfigForFile(opts, discovered.EntrypointPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
			return exitWith(ExitConfigError)
		}
		return writeReport(opts, cfg, nil, nil, 0, 0)
	}

	res, err := lintInvocations(ctx, discovered.Invocations, opts)
	if err != nil {
		return handleLintError(err)
	}
	res.filesScanned = len(res.fileSources)
	res.invocationsScanned = len(discovered.Invocations)

	resolveAsyncChecks(ctx, res)

	allViolations := processViolations(res, res.firstCfg)
	warnFixUnsafe(opts)
	return writeReport(opts, res.firstCfg, allViolations, res.fileSources, res.filesScanned, res.invocationsScanned)
}

func classifyLintEntrypoint(ctx stdcontext.Context, inputs []string, opts *lintOptions) (*invocation.DiscoveryResult, bool, error) {
	if len(inputs) != 1 {
		return nil, false, nil
	}
	input := inputs[0]
	if !isRegularFile(input) {
		return nil, false, nil
	}
	if invocation.IsDockerfileName(input) {
		return nil, true, nil
	}

	ext := strings.ToLower(filepath.Ext(input))
	switch ext {
	case ".hcl":
		result, err := discoverBakeEntrypoint(ctx, input, opts)
		return result, true, err
	case ".json":
		if kind, ok := invocation.ProbeEntrypointKind(input); ok {
			result, err := discoverEntrypointByKind(ctx, input, kind, opts)
			return result, true, err
		}
		result, bakeErr := discoverBakeEntrypoint(ctx, input, opts)
		if bakeErr == nil {
			return result, true, nil
		}
		result, composeErr := discoverComposeEntrypoint(ctx, input, opts)
		if composeErr == nil {
			return result, true, nil
		}
		return nil, true, fmt.Errorf("%s is not a Dockerfile, Compose, or Bake file", input)
	case ".yml", ".yaml":
		result, err := discoverComposeEntrypoint(ctx, input, opts)
		return result, true, err
	default:
		kind, ok := invocation.ProbeEntrypointKind(input)
		if !ok {
			return nil, true, fmt.Errorf("%s is not a Dockerfile, Compose, or Bake file", input)
		}
		result, err := discoverEntrypointByKind(ctx, input, kind, opts)
		return result, true, err
	}
}

func discoverEntrypointByKind(ctx stdcontext.Context, input, kind string, opts *lintOptions) (*invocation.DiscoveryResult, error) {
	switch kind {
	case invocation.KindCompose:
		return discoverComposeEntrypoint(ctx, input, opts)
	case invocation.KindBake:
		return discoverBakeEntrypoint(ctx, input, opts)
	default:
		return nil, fmt.Errorf("%s is not a Dockerfile, Compose, or Bake file", input)
	}
}

func discoverComposeEntrypoint(ctx stdcontext.Context, input string, opts *lintOptions) (*invocation.DiscoveryResult, error) {
	return invocation.ComposeProvider{}.Discover(ctx, invocation.ResolveOptions{
		Path:     input,
		Services: opts.services,
	})
}

func discoverBakeEntrypoint(ctx stdcontext.Context, input string, opts *lintOptions) (*invocation.DiscoveryResult, error) {
	return invocation.BakeProvider{}.Discover(ctx, invocation.ResolveOptions{
		Path:    input,
		Targets: opts.targets,
	})
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func rejectMixedOrchestratorInputs(inputs []string, opts *lintOptions) error {
	if hasOrchestratorSelectionFlags(opts) {
		return errors.New("--target and --service are only valid for a single explicit orchestrator file")
	}
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if invocation.IsDockerfileName(input) {
			continue
		}
		if invocation.IsObviousOrchestratorName(input) {
			return errors.New("orchestrator entrypoints cannot be mixed with other lint inputs")
		}
	}
	return nil
}

func hasOrchestratorSelectionFlags(opts *lintOptions) bool {
	return len(opts.targets) > 0 || len(opts.services) > 0
}

func validateOrchestratorFlags(opts *lintOptions, kind string) error {
	if opts.fix {
		return errors.New("--fix is not supported for orchestrator entrypoints")
	}
	if opts.contextSet && opts.contextDir != "" {
		return errors.New("--context is not supported for orchestrator entrypoints")
	}
	switch kind {
	case invocation.KindBake:
		if len(opts.services) > 0 {
			return errors.New("--service is only valid for Compose entrypoints")
		}
	case invocation.KindCompose:
		if len(opts.targets) > 0 {
			return errors.New("--target is only valid for Bake entrypoints")
		}
	}
	return nil
}

func lintInvocations(ctx stdcontext.Context, invocations []invocation.BuildInvocation, opts *lintOptions) (*lintResults, error) {
	res := &lintResults{
		fileSources:     make(map[string][]byte),
		fileConfigs:     make(map[string]*config.Config),
		fileInvocations: make(map[string]*invocation.BuildInvocation),
	}
	parseCache := make(map[string]*dockerfile.ParseResult)

	for _, inv := range invocations {
		file := inv.DockerfilePath

		cfg := res.fileConfigs[file]
		if cfg == nil {
			var err error
			cfg, err = loadConfigForFile(opts, file)
			if err != nil {
				return nil, fmt.Errorf("failed to load config for %s: %w", file, err)
			}
			validateAIConfig(cfg, file)
			validateDurationConfigs(cfg, file)
			res.fileConfigs[file] = cfg
		}
		if res.firstCfg == nil {
			res.firstCfg = cfg
		}

		if err := fileval.ValidateFile(file, cfg.FileValidation.MaxFileSize); err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		parseResult := parseCache[file]
		if parseResult == nil {
			var err error
			parseResult, err = dockerfile.ParseFile(ctx, file, cfg)
			if err != nil {
				return nil, fmt.Errorf("failed to lint %s: %w", file, err)
			}
			if syntaxErrors := syntax.Check(file, parseResult.AST, parseResult.Source); len(syntaxErrors) > 0 {
				return nil, &syntax.CheckError{Errors: syntaxErrors}
			}
			parseCache[file] = parseResult
		}

		result, err := linter.LintFile(linter.Input{
			FilePath:    file,
			Config:      cfg,
			ParseResult: parseResult,
			Invocation:  &inv,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		res.fileSources[file] = result.ParseResult.Source
		addFileInvocation(res.fileInvocations, &inv)
		res.violations = append(res.violations, result.Violations...)
		res.asyncPlans = append(res.asyncPlans, result.AsyncPlan...)
	}
	return res, nil
}

// lintFiles runs the lint pipeline on each discovered file and aggregates results.
func lintFiles(ctx stdcontext.Context, discovered []discovery.DiscoveredFile, opts *lintOptions) (*lintResults, error) {
	res := &lintResults{
		fileSources:     make(map[string][]byte),
		fileConfigs:     make(map[string]*config.Config),
		fileInvocations: make(map[string]*invocation.BuildInvocation),
	}

	for _, df := range discovered {
		file := df.Path

		cfg, err := loadConfigForFile(opts, file)
		if err != nil {
			return nil, fmt.Errorf("failed to load config for %s: %w", file, err)
		}

		validateAIConfig(cfg, file)
		validateDurationConfigs(cfg, file)
		res.fileConfigs[file] = cfg

		if err := fileval.ValidateFile(file, cfg.FileValidation.MaxFileSize); err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		if res.firstCfg == nil {
			res.firstCfg = cfg
		}

		// Parse once — reused for syntax checks, build context, and LintFile.
		parseResult, err := dockerfile.ParseFile(ctx, file, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		// Fail-fast syntax checks (unknown instructions, directive typos).
		if syntaxErrors := syntax.Check(file, parseResult.AST, parseResult.Source); len(syntaxErrors) > 0 {
			return nil, &syntax.CheckError{Errors: syntaxErrors}
		}

		var inv *invocation.BuildInvocation
		if df.ContextDir != "" {
			inv = invocationFromContextFlag(file, df.ContextDir)
		}

		result, err := linter.LintFile(linter.Input{
			FilePath:    file,
			Config:      cfg,
			ParseResult: parseResult,
			Invocation:  inv,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		res.fileSources[file] = result.ParseResult.Source
		if inv != nil {
			addFileInvocation(res.fileInvocations, inv)
		}
		res.violations = append(res.violations, result.Violations...)
		res.asyncPlans = append(res.asyncPlans, result.AsyncPlan...)
	}

	return res, nil
}

// handleLintError maps errors from lintFiles to the appropriate exit code.
// Syntax errors (unknown instructions, directive typos) return ExitSyntaxError;
// all other errors return ExitConfigError.
func handleLintError(err error) error {
	var syntaxErr *syntax.CheckError
	if errors.As(err, &syntaxErr) {
		for _, e := range syntaxErr.Errors {
			fmt.Fprintf(os.Stderr, "Error: %s\n", e.Error())
		}
		return exitWith(ExitSyntaxError)
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return exitWith(ExitConfigError)
}

// writeReport formats and writes the violation report using the configured output path.
func writeReport(
	opts *lintOptions, cfg *config.Config, violations []rules.Violation,
	fileSources map[string][]byte, filesScanned, invocationsScanned int,
) error {
	return writeReportTo(opts, cfg, violations, fileSources, filesScanned, invocationsScanned, "")
}

// writeReportTo formats and writes the violation report. If outputOverride is
// non-empty, it overrides the configured output path (e.g. "stderr" to keep
// stdout free for fixed content in stdin mode).
func writeReportTo(
	opts *lintOptions, cfg *config.Config, violations []rules.Violation,
	fileSources map[string][]byte, filesScanned, invocationsScanned int, outputOverride string,
) error {
	outCfg := getOutputConfig(opts, cfg)
	if outputOverride != "" {
		outCfg.path = outputOverride
	}

	formatType, err := reporter.ParseFormat(outCfg.format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWith(ExitConfigError)
	}

	writer, closeWriter, err := reporter.GetWriter(outCfg.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWith(ExitConfigError)
	}
	defer func() {
		if err := closeWriter(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close output: %v\n", err)
		}
	}()

	reportOpts := reporter.Options{
		Format:      formatType,
		Writer:      writer,
		ShowSource:  outCfg.showSource,
		ToolName:    "tally",
		ToolVersion: version.Version(),
		ToolURI:     "https://github.com/wharflab/tally",
	}

	if opts.noColor != nil && *opts.noColor {
		noColor := false
		reportOpts.Color = &noColor
	}

	rep, err := reporter.New(reportOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create reporter: %v\n", err)
		return exitWith(ExitConfigError)
	}

	rulesEnabled := len(linter.EnabledRuleCodes(cfg))
	metadata := reporter.ReportMetadata{
		FilesScanned:       filesScanned,
		InvocationsScanned: invocationsScanned,
		RulesEnabled:       rulesEnabled,
	}

	if err := rep.Report(violations, fileSources, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to write output: %v\n", err)
		return exitWith(ExitConfigError)
	}

	exitCode := determineExitCode(violations, outCfg.failLevel)
	if exitCode != ExitSuccess {
		return exitWith(exitCode)
	}

	return nil
}

// loadConfigForFile loads configuration for a target file.
//
// Simple config-shaped flags (--format, --max-lines, --ai-*, ...) participate
// in koanf's defaults -> config file -> env -> CLI precedence through the
// posflag provider supplied by lintFlagMapper. Only flags with append,
// inversion, or non-config semantics need explicit handling here.
func loadConfigForFile(opts *lintOptions, targetPath string) (*config.Config, error) {
	var cfg *config.Config
	var err error
	if opts.configPath != "" {
		cfg, err = config.LoadFromFileWithFlags(opts.configPath, opts.flags, lintFlagMapper())
	} else {
		cfg, err = config.LoadWithFlags(targetPath, opts.flags, lintFlagMapper())
	}
	if err != nil {
		return nil, err
	}

	// --select / --ignore append to the configured selection rather than
	// replacing it, so they live outside the posflag layer.
	if len(opts.selectR) > 0 {
		cfg.Rules.Include = append(cfg.Rules.Include, opts.selectR...)
	}
	if len(opts.ignore) > 0 {
		cfg.Rules.Exclude = append(cfg.Rules.Exclude, opts.ignore...)
	}

	// --no-inline-directives inverts the enabled setting.
	if opts.noInlineDirectives != nil {
		cfg.InlineDirectives.Enabled = !*opts.noInlineDirectives
	}

	// --acp-command takes a shell-quoted string and implicitly enables AI.
	// posflag can't express the parse step or the implicit enable, so we do
	// it here after koanf has produced the merged ai.* view.
	if opts.acpCommandSet {
		argv, err := parseACPCmd(opts.acpCommand)
		if err != nil {
			return nil, err
		}
		cfg.AI.Command = argv
		cfg.AI.Enabled = true
	}

	return cfg, nil
}

func parseACPCmd(commandLine string) ([]string, error) {
	fields, err := splitCommandLine(commandLine)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 || fields[0] == "" {
		return nil, errors.New("acp-command is empty")
	}
	return fields, nil
}

func splitCommandLine(commandLine string) ([]string, error) {
	var s commandLineSplitter
	for i := range len(commandLine) {
		var next byte
		hasNext := false
		if i+1 < len(commandLine) {
			next = commandLine[i+1]
			hasNext = true
		}
		s.consume(commandLine[i], next, hasNext)
	}
	return s.finish()
}

type commandLineSplitter struct {
	out []string
	cur strings.Builder

	inArg    bool
	inSingle bool
	inDouble bool
	escaped  bool
}

func (s *commandLineSplitter) flush() {
	if !s.inArg {
		return
	}
	s.out = append(s.out, s.cur.String())
	s.cur.Reset()
	s.inArg = false
}

func (s *commandLineSplitter) consume(ch, next byte, hasNext bool) {
	switch {
	case s.escaped:
		s.consumeEscaped(ch)
	case s.inSingle:
		s.consumeSingle(ch)
	case s.inDouble:
		s.consumeDouble(ch, next, hasNext)
	default:
		s.consumePlain(ch, next, hasNext)
	}
}

func (s *commandLineSplitter) consumeEscaped(ch byte) {
	s.cur.WriteByte(ch)
	s.escaped = false
	s.inArg = true
}

func (s *commandLineSplitter) consumeSingle(ch byte) {
	if ch == '\'' {
		s.inSingle = false
		s.inArg = true
		return
	}
	s.cur.WriteByte(ch)
	s.inArg = true
}

func (s *commandLineSplitter) consumeDouble(ch, next byte, hasNext bool) {
	switch ch {
	case '"':
		s.inDouble = false
		s.inArg = true
	case '\\':
		if hasNext && shouldEscapeNextByte(next) {
			s.escaped = true
			s.inArg = true
			return
		}
		s.cur.WriteByte('\\')
		s.inArg = true
	default:
		s.cur.WriteByte(ch)
		s.inArg = true
	}
}

func (s *commandLineSplitter) consumePlain(ch, next byte, hasNext bool) {
	switch ch {
	case ' ', '\t', '\n', '\r':
		s.flush()
	case '\'':
		s.inSingle = true
		s.inArg = true
	case '"':
		s.inDouble = true
		s.inArg = true
	case '\\':
		if hasNext && shouldEscapeNextByte(next) {
			s.escaped = true
			s.inArg = true
			return
		}
		s.cur.WriteByte('\\')
		s.inArg = true
	default:
		s.cur.WriteByte(ch)
		s.inArg = true
	}
}

func shouldEscapeNextByte(next byte) bool {
	switch next {
	case '"', '\'', '\\', ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func (s *commandLineSplitter) finish() ([]string, error) {
	if s.escaped {
		return nil, errors.New("acp-command has a trailing backslash escape")
	}
	if s.inSingle {
		return nil, errors.New("acp-command has an unterminated single quote")
	}
	if s.inDouble {
		return nil, errors.New("acp-command has an unterminated double quote")
	}

	s.flush()
	return s.out, nil
}

// outputConfig holds output configuration values.
type outputConfig struct {
	format     string
	path       string
	showSource bool
	failLevel  string
}

// getOutputConfig returns output configuration from CLI flags and config.
func getOutputConfig(opts *lintOptions, cfg *config.Config) outputConfig {
	// Start with defaults
	oc := outputConfig{
		format:     "text",
		path:       "stdout",
		showSource: true,
		failLevel:  "style",
	}

	if cfg != nil {
		// Flag values already flowed into cfg.Output via the posflag layer,
		// so reading cfg is the one true source here.
		if cfg.Output.Format != "" {
			oc.format = cfg.Output.Format
		}
		if cfg.Output.Path != "" {
			oc.path = cfg.Output.Path
		}
		oc.showSource = cfg.Output.ShowSource
		if cfg.Output.FailLevel != "" {
			oc.failLevel = cfg.Output.FailLevel
		}
	}

	// --hide-source is an inversion flag that can't go through posflag.
	if opts.hideSource {
		oc.showSource = false
	}

	return oc
}

// determineExitCode returns the appropriate exit code based on violations and fail-level.
func determineExitCode(violations []rules.Violation, failLevel string) int {
	// "none" means never fail due to violations
	if failLevel == "none" {
		return ExitSuccess
	}

	// Parse fail-level first to catch config errors even with no violations
	threshold, err := parseFailLevel(failLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --fail-level %q\n", failLevel)
		return ExitConfigError
	}

	if len(violations) == 0 {
		return ExitSuccess
	}

	// Check if any violation meets or exceeds the threshold
	for _, v := range violations {
		if v.Severity.IsAtLeast(threshold) {
			return ExitViolations
		}
	}

	return ExitSuccess
}

// parseFailLevel parses a fail-level string to a Severity.
func parseFailLevel(level string) (rules.Severity, error) {
	switch level {
	case "", "style":
		// Default to "style" (any violation fails)
		return rules.SeverityStyle, nil
	default:
		return rules.ParseSeverity(level)
	}
}

// validateAIConfig validates top-level AI configuration.
// Prints warnings to stderr but does not abort.
func validateAIConfig(cfg *config.Config, file string) {
	if cfg == nil || !cfg.AI.Enabled {
		return
	}

	if len(cfg.AI.Command) == 0 {
		source := file
		if cfg.ConfigFile != "" {
			source = cfg.ConfigFile
		}
		fmt.Fprintf(os.Stderr, "Warning: ai.enabled=true but ai.command is empty (%s)\n", source)
	}
}

// validateDurationConfigs validates duration string fields at config load time
// so the user sees a clear warning instead of silent fallback to defaults.
func validateDurationConfigs(cfg *config.Config, file string) {
	source := file
	if cfg.ConfigFile != "" {
		source = cfg.ConfigFile
	}

	if t := cfg.SlowChecks.Timeout; t != "" {
		if _, err := time.ParseDuration(t); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid slow-checks.timeout %q (%s): %v\n", t, source, err)
		}
	}
	if t := cfg.AI.Timeout; t != "" {
		if _, err := time.ParseDuration(t); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid ai.timeout %q (%s): %v\n", t, source, err)
		}
	}
}

func invocationFromContextFlag(file, contextDir string) *invocation.BuildInvocation {
	if contextDir == "" {
		return nil
	}
	inv, err := invocation.NewDockerfileInvocation(file, contextDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to normalize build context %q for %s: %v\n", contextDir, file, err)
		return nil
	}
	return inv
}

func addFileInvocation(fileInvocations map[string]*invocation.BuildInvocation, inv *invocation.BuildInvocation) {
	if inv == nil {
		return
	}
	fileInvocations[inv.Key] = inv
}

func contextDirForViolation(v rules.Violation, fileInvocations map[string]*invocation.BuildInvocation) string {
	if len(fileInvocations) == 0 {
		return ""
	}
	inv := fileInvocations[v.InvocationKey]
	if inv == nil || inv.ContextRef.Kind != invocation.ContextKindDir {
		return ""
	}
	return inv.ContextRef.Value
}

// applyFixes applies automatic fixes to violations that have suggested fixes.
// input.fileConfigs maps file paths to their per-file configs.
func applyFixes(
	ctx stdcontext.Context,
	opts *lintOptions,
	input applyFixesInput,
) (*fix.Result, error) {
	// Determine safety threshold
	safetyThreshold := fix.FixSafe
	if opts.fixUnsafe {
		safetyThreshold = fix.FixUnsafe
	}

	// Get rule filter
	ruleFilter := opts.fixRule

	// Build per-file fix modes from fileConfigs
	fixModes := buildPerFileFixModes(input.fileConfigs)

	// Register AI resolver only when AI is enabled for at least one file config.
	aiEnabled := false
	normalizedConfigs := make(map[string]*config.Config, len(input.fileConfigs))
	for path, cfg := range input.fileConfigs {
		normalizedConfigs[filepath.ToSlash(path)] = cfg
		if cfg != nil && cfg.AI.Enabled {
			aiEnabled = true
		}
	}
	if aiEnabled {
		autofix.Register()
	}

	registryInsightsByFile := collectRegistryInsights(input.asyncPlans, input.asyncResult)

	// Enrich AI resolver requests with per-file config + outer fix context.
	fixCtx := autofixdata.FixContext{
		SafetyThreshold: safetyThreshold,
		RuleFilter:      ruleFilter,
		FixModes:        fixModes,
	}
	for i := range input.violations {
		v := &input.violations[i]
		for _, sf := range v.AllFixes() {
			if !sf.NeedsResolve {
				continue
			}
			if sf.ResolverID != autofixdata.ResolverID {
				continue
			}
			req, ok := sf.ResolverData.(interface {
				SetConfig(cfg *config.Config)
				SetFixContext(ctx autofixdata.FixContext)
			})
			if !ok {
				continue
			}
			cfg := normalizedConfigs[filepath.ToSlash(v.File())]
			req.SetConfig(cfg)
			req.SetFixContext(fixCtx)

			if setter, ok := sf.ResolverData.(interface {
				SetRegistryInsights(insights []autofixdata.RegistryInsight)
			}); ok {
				setter.SetRegistryInsights(registryInsightsByFile[filepath.ToSlash(v.File())])
			}

			if setter, ok := sf.ResolverData.(interface {
				SetContextDir(dir string)
			}); ok {
				if dir := contextDirForViolation(*v, input.fileInvocations); dir != "" {
					setter.SetContextDir(dir)
				}
			}
		}
	}

	aiFixes, maxAITimeout := planAcpFixSpinner(input.violations, safetyThreshold, ruleFilter, fixModes, normalizedConfigs)
	stopSpinner := startAcpFixSpinner(aiFixes, maxAITimeout)
	defer stopSpinner()

	fixer := &fix.Fixer{
		SafetyThreshold:   safetyThreshold,
		RuleFilter:        ruleFilter,
		EnabledRules:      buildPerFileEnabledRules(input.fileConfigs, input.sources),
		SlowChecksEnabled: buildPerFileSlowChecksEnabled(input.fileConfigs, input.sources),
		FixModes:          fixModes,
		Concurrency:       4,
	}

	result, err := fixer.Apply(ctx, input.violations, input.sources)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// writeFixedFiles writes modified files back to disk, preserving original permissions.
func writeFixedFiles(result *fix.Result) error {
	for _, fc := range result.Changes {
		if !fc.HasChanges() {
			continue
		}
		mode := os.FileMode(0o644)
		if info, err := os.Stat(fc.Path); err == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(fc.Path, fc.ModifiedContent, mode); err != nil {
			return fmt.Errorf("failed to write %s: %w", fc.Path, err)
		}
	}
	return nil
}

// buildPerFileFixModes builds a per-file map of fix modes from fileConfigs.
// Returns map[filePath]map[ruleCode]FixMode.
func buildPerFileFixModes(fileConfigs map[string]*config.Config) map[string]map[string]fix.FixMode {
	result := make(map[string]map[string]fix.FixMode)
	for filePath, cfg := range fileConfigs {
		if cfg == nil {
			continue
		}
		modes := fix.BuildFixModes(cfg)
		if len(modes) > 0 {
			result[filepath.Clean(filePath)] = modes
		}
	}
	return result
}

func buildPerFileEnabledRules(
	fileConfigs map[string]*config.Config,
	sources map[string][]byte,
) map[string][]string {
	enabledRules := make(map[string][]string, len(sources))
	for path := range sources {
		cfg := fileConfigs[path]
		if cfg == nil {
			cfg = fileConfigs[filepath.ToSlash(path)]
		}
		enabledRules[filepath.Clean(path)] = linter.EnabledRuleCodes(cfg)
	}
	return enabledRules
}

func buildPerFileSlowChecksEnabled(fileConfigs map[string]*config.Config, sources map[string][]byte) map[string]bool {
	enabled := make(map[string]bool, len(sources))
	for path := range sources {
		cfg := fileConfigs[path]
		if cfg == nil {
			cfg = fileConfigs[filepath.ToSlash(path)]
		}
		if cfg == nil {
			cfg = config.Default()
		}
		enabled[filepath.Clean(path)] = config.SlowChecksEnabled(cfg.SlowChecks.Mode)
	}
	return enabled
}

// runAsyncChecks executes async check plans if slow checks are enabled.
// Returns nil if slow checks are disabled or no plans exist.
// Respects per-file slow-checks configuration from res.fileConfigs.
func runAsyncChecks(ctx stdcontext.Context, res *lintResults) (*async.RunResult, []async.CheckRequest) {
	if len(res.asyncPlans) == 0 {
		return nil, nil
	}

	plans, maxTimeout := filterAsyncPlans(res)
	if len(plans) == 0 {
		return nil, nil
	}

	// Register the registry resolver (once per invocation).
	if registry.NewDefaultResolver == nil {
		fmt.Fprintf(os.Stderr, "note: slow checks not available (missing build tags)\n")
		return nil, nil
	}
	imgResolver := registry.NewDefaultResolver()
	asyncImgResolver := registry.NewAsyncImageResolver(imgResolver)

	rt := &async.Runtime{
		Concurrency: 4,
		Timeout:     maxTimeout,
		Resolvers: map[string]async.Resolver{
			asyncImgResolver.ID(): asyncImgResolver,
		},
	}

	result := rt.Run(ctx, plans)
	reportSkipped(result)
	return result, plans
}

// filterAsyncPlans applies per-file slow-checks policy to async plans.
// Returns the filtered plans and the maximum timeout across all enabled files.
func filterAsyncPlans(res *lintResults) ([]async.CheckRequest, time.Duration) {
	// Pre-apply severity overrides and enable filter so fail-fast only considers
	// violations from rules the user has actually enabled (respecting --select,
	// --ignore, and severity overrides).
	procCtx := processor.NewContext(res.fileConfigs, res.firstCfg, res.fileSources)
	filtered := processor.NewSeverityOverride().Process(res.violations, procCtx)
	filtered = processor.NewEnableFilter().Process(filtered, procCtx)
	errorContexts := filesWithErrors(failFastViolations(filtered))
	maxTimeout := 20 * time.Second
	plans := make([]async.CheckRequest, 0, len(res.asyncPlans))
	var skippedAuto int

	for _, req := range res.asyncPlans {
		cfg := res.fileConfigs[req.File]
		if cfg == nil {
			cfg = res.firstCfg
		}
		if cfg == nil {
			continue
		}

		slowCfg := cfg.SlowChecks
		if !config.SlowChecksEnabled(slowCfg.Mode) {
			if slowCfg.Mode == "auto" {
				skippedAuto++
			}
			continue
		}

		// Per-file fail-fast: skip if fast rules produced SeverityError.
		if slowCfg.FailFast && errorContexts[asyncErrorKey(req.File, req.InvocationKey)] {
			continue
		}

		// Apply per-file timeout to the request.
		if d, err := time.ParseDuration(slowCfg.Timeout); err == nil && d > 0 {
			req.Timeout = d
			if d > maxTimeout {
				maxTimeout = d
			}
		}

		plans = append(plans, req)
	}

	if skippedAuto > 0 {
		ciName := config.CIName()
		if ciName != "" {
			fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (%s detected; use --slow-checks=on to enable)\n", skippedAuto, ciName)
		} else {
			fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (use --slow-checks=on to enable)\n", skippedAuto)
		}
	}

	return plans, maxTimeout
}

func failFastViolations(violations []rules.Violation) []rules.Violation {
	out := make([]rules.Violation, 0, len(violations))
	for _, v := range violations {
		if strings.HasPrefix(v.RuleCode, rules.PowerShellRulePrefix) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// reportSkipped prints summary notes for skipped async checks.
func reportSkipped(result *async.RunResult) {
	if len(result.Skipped) == 0 {
		return
	}
	counts := make(map[async.SkipReason]int)
	for _, s := range result.Skipped {
		counts[s.Reason]++
	}
	if n := counts[async.SkipTimeout]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) timed out (increase --slow-checks-timeout)\n", n)
	}
	if n := counts[async.SkipNetwork]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (registry unreachable or rate-limited)\n", n)
	}
	if n := counts[async.SkipAuth]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (authentication failed)\n", n)
	}
	if n := counts[async.SkipNotFound]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (image not found)\n", n)
	}
	if n := counts[async.SkipResolverErr]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped due to errors\n", n)
	}
}

func reportSkippedFixes(result *fix.Result) {
	if result == nil || result.TotalSkipped() == 0 {
		return
	}

	type skippedFixInfo struct {
		filePath string
		ruleCode string
		errorMsg string
	}

	var (
		aiTimeouts int
		aiErrors   int
		otherErrs  int
		samples    []skippedFixInfo
	)

	for _, fc := range result.Changes {
		if fc == nil {
			continue
		}
		for _, s := range fc.FixesSkipped {
			if s.Reason != fix.SkipResolveError || s.Error == "" {
				continue
			}

			isAI := strings.Contains(s.Error, "ai-autofix") || strings.Contains(s.Error, "acp ")
			if isAI {
				if strings.Contains(s.Error, stdcontext.DeadlineExceeded.Error()) {
					aiTimeouts++
				} else {
					aiErrors++
				}
			} else {
				otherErrs++
			}

			if len(samples) < 5 {
				samples = append(samples, skippedFixInfo{
					filePath: fc.Path,
					ruleCode: s.RuleCode,
					errorMsg: compactSingleLine(s.Error, 500),
				})
			}
		}
	}

	if aiTimeouts > 0 {
		fmt.Fprintf(os.Stderr, "note: %d AI fix(es) timed out (increase --ai-timeout or ai.timeout)\n", aiTimeouts)
	}
	if aiErrors > 0 {
		fmt.Fprintf(os.Stderr, "note: %d AI fix(es) failed (see details below)\n", aiErrors)
	}
	if otherErrs > 0 {
		fmt.Fprintf(os.Stderr, "note: %d fix(es) skipped due to resolver errors\n", otherErrs)
	}

	for _, s := range samples {
		fmt.Fprintf(os.Stderr, "note: skipped fix %s (%s): %s\n", s.ruleCode, s.filePath, s.errorMsg)
	}
}

func compactSingleLine(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "; ")
	s = strings.TrimSpace(s)
	if maxLen > 0 && len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}

// filesWithErrors returns a set of file/invocation pairs that have SeverityError violations.
func filesWithErrors(violations []rules.Violation) map[string]bool {
	m := make(map[string]bool)
	for _, v := range violations {
		if v.Severity == rules.SeverityError {
			m[asyncErrorKey(v.File(), v.InvocationKey)] = true
		}
	}
	return m
}

func asyncErrorKey(file, invocationKey string) string {
	return invocationKey + "|" + filepath.ToSlash(file)
}

// filterFixedViolations removes violations that were fixed from the list.
// It also re-checks violations from rules implementing PostFixRevalidator
// against the modified file content, suppressing stale violations.
func filterFixedViolations(
	violations []rules.Violation,
	fixResult *fix.Result,
	fileConfigs map[string]*config.Config,
) []rules.Violation {
	return fix.FilterFixedViolations(violations, fixResult, fileConfigs)
}

// reportNoFilesFound prints a context-aware message when no Dockerfiles are found.
func reportNoFilesFound(inputs []string) {
	for _, input := range inputs {
		if discovery.ContainsGlobChars(input) {
			fmt.Fprintf(os.Stderr, "Error: no Dockerfiles matched pattern: %s\n", input)
			return
		}
	}

	// For directory inputs, resolve to absolute path so the user knows exactly
	// which directory was scanned.
	for _, input := range inputs {
		abs, err := filepath.Abs(input)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err == nil && info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: no Dockerfile or Containerfile found in %s\n", abs)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Error: no Dockerfiles found\n")
}
