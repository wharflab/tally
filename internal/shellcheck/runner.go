package shellcheck

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"

	"github.com/wharflab/tally/internal/shellcheck/wasm"
)

type Options struct {
	// Dialect is the shell dialect for ShellCheck: sh, bash, dash, ksh, busybox.
	Dialect string

	// Severity is ShellCheck's minimum severity: error, warning, info, style.
	// Empty means use ShellCheck default.
	Severity string

	// Norc disables loading .shellcheckrc.
	Norc bool

	// ExtendedAnalysis sets --extended-analysis. Nil means use ShellCheck default.
	ExtendedAnalysis *bool

	// EnableOptional is passed via --enable=... (optional checks).
	EnableOptional []string

	// Include is passed via --include=... (consider only given codes).
	Include []string

	// Exclude is passed via --exclude=... (exclude given codes).
	Exclude []string
}

type Runner struct {
	initOnce sync.Once
	initErr  error

	rt       wazero.Runtime
	compiled wazero.CompiledModule

	cache wazero.CompilationCache
}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, script string, opts Options) (JSON1Output, string, error) {
	if err := r.init(ctx); err != nil {
		return JSON1Output{}, "", err
	}

	args := buildArgs(opts)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := wazero.NewModuleConfig().
		WithArgs(args...).
		WithStdin(strings.NewReader(script)).
		WithStdout(&stdout).
		WithStderr(&stderr)

	mod, err := r.rt.InstantiateModule(ctx, r.compiled, cfg)

	// Ensure module resources are released even when _start calls proc_exit.
	if mod != nil {
		_ = mod.Close(ctx)
	}

	exitCode, exitErr := exitCodeFromErr(err)
	if exitErr != nil {
		return JSON1Output{}, stderr.String(), exitErr
	}

	// ShellCheck returns exit code 1 when findings exist; treat as success.
	if exitCode != 0 && exitCode != 1 {
		return JSON1Output{}, stderr.String(), fmt.Errorf("shellcheck failed (exit %d): %s", exitCode, strings.TrimSpace(stderr.String()))
	}

	var out JSON1Output
	if unmarshalErr := json.Unmarshal(stdout.Bytes(), &out); unmarshalErr != nil {
		return JSON1Output{}, stderr.String(), fmt.Errorf("parse shellcheck json1: %w", unmarshalErr)
	}

	return out, stderr.String(), nil
}

func (r *Runner) init(ctx context.Context) error {
	initCtx := runtimeInitContext(ctx)
	r.initOnce.Do(func() {
		rtCfg := wazero.NewRuntimeConfig().WithDebugInfoEnabled(false)
		cache := newCompilationCache()
		if cache != nil {
			rtCfg = rtCfg.WithCompilationCache(cache)
			r.cache = cache
		}

		rt := wazero.NewRuntimeWithConfig(initCtx, rtCfg)

		if _, err := wasi_snapshot_preview1.Instantiate(initCtx, rt); err != nil {
			_ = rt.Close(initCtx)
			r.initErr = fmt.Errorf("instantiate WASI: %w", err)
			return
		}

		// Use 2/3 of available CPUs for concurrent WASM compilation,
		// leaving headroom for the rest of the process.
		// Skip entirely on Windows where the experimental concurrent
		// compilation path has shown reliability issues.
		compileCtx := initCtx
		if runtime.GOOS != "windows" {
			compileCtx = experimental.WithCompilationWorkers(compileCtx, max(runtime.NumCPU()*2/3, 1))
		}
		compiled, err := rt.CompileModule(compileCtx, wasm.Binary)
		if err != nil {
			_ = rt.Close(initCtx)
			r.initErr = fmt.Errorf("compile shellcheck.wasm: %w", err)
			return
		}

		r.rt = rt
		r.compiled = compiled
	})
	return r.initErr
}

func runtimeInitContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func newCompilationCache() wazero.CompilationCache {
	cacheDir := os.Getenv("TALLY_SHELLCHECK_WAZERO_CACHE_DIR")
	if cacheDir == "" {
		baseDir, err := os.UserCacheDir()
		if err != nil {
			return nil
		}
		cacheDir = filepath.Join(baseDir, "tally", "shellcheck-wazero-cache")
	}

	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil
	}

	cache, err := wazero.NewCompilationCacheWithDir(cacheDir)
	if err != nil {
		return nil
	}
	return cache
}

func buildArgs(opts Options) []string {
	args := []string{"shellcheck", "-f", "json1"}

	if opts.Dialect != "" {
		args = append(args, "-s", opts.Dialect)
	}
	if opts.Severity != "" {
		args = append(args, "-S", opts.Severity)
	}
	if opts.Norc {
		args = append(args, "--norc")
	}
	if opts.ExtendedAnalysis != nil {
		args = append(args, "--extended-analysis="+strconv.FormatBool(*opts.ExtendedAnalysis))
	}
	if len(opts.EnableOptional) > 0 {
		args = append(args, "--enable="+strings.Join(opts.EnableOptional, ","))
	}
	if len(opts.Include) > 0 {
		args = append(args, "--include="+strings.Join(opts.Include, ","))
	}
	if len(opts.Exclude) > 0 {
		args = append(args, "--exclude="+strings.Join(opts.Exclude, ","))
	}

	// Read from stdin. Our embedded build is patched to support stdin-only.
	args = append(args, "-")
	return args
}

func exitCodeFromErr(err error) (int, error) {
	if err == nil {
		return 0, nil
	}

	var exitErr *sys.ExitError
	if errors.As(err, &exitErr) {
		return int(exitErr.ExitCode()), nil
	}

	return 0, fmt.Errorf("shellcheck runtime error: %w", err)
}
