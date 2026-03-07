package shellcheck

import (
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

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

// Runner executes ShellCheck via a WASM reactor module.
//
// The WASM binary is compiled once (expensive, cached on disk by wazero).
// A single reactor module instance is instantiated and initialized once
// (_initialize + hs_init). Each subsequent call uses sc_alloc/sc_check/sc_free
// with WASM linear memory buffers to avoid per-call instantiation overhead.
//
// Note: WASI reactor modules export _initialize and require the host to call it
// exactly once after instantiation.
type Runner struct {
	initOnce sync.Once
	initErr  error

	rt       wazero.Runtime
	compiled wazero.CompiledModule

	cache wazero.CompilationCache

	// Single long-lived reactor instance and cached exports.
	mod     api.Module
	mem     api.Memory
	scAlloc api.Function
	scCheck api.Function
	scFree  api.Function

	mu sync.Mutex // serialize calls into the single module instance
}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, script string, opts Options) (JSON1Output, string, error) {
	if err := r.init(ctx); err != nil {
		return JSON1Output{}, "", err
	}

	result, err := r.callReactor(ctx, script, opts)
	if err != nil {
		return JSON1Output{}, "", err
	}

	var out JSON1Output
	if unmarshalErr := json.Unmarshal(result, &out); unmarshalErr != nil {
		return JSON1Output{}, "", fmt.Errorf("parse shellcheck json1: %w", unmarshalErr)
	}

	return out, "", nil
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
		if err != nil && cache != nil {
			// On Windows, concurrent processes racing on the compilation
			// cache can fail with "Access is denied" during the atomic
			// rename. Retry without the cache — the compiled module is
			// identical, just not persisted to disk.
			_ = rt.Close(initCtx)
			rtCfg = wazero.NewRuntimeConfig().WithDebugInfoEnabled(false)
			rt = wazero.NewRuntimeWithConfig(initCtx, rtCfg)
			if _, wasiErr := wasi_snapshot_preview1.Instantiate(initCtx, rt); wasiErr != nil {
				_ = rt.Close(initCtx)
				r.initErr = fmt.Errorf("instantiate WASI (retry): %w", wasiErr)
				return
			}
			compiled, err = rt.CompileModule(compileCtx, wasm.Binary)
		}
		if err != nil {
			_ = rt.Close(initCtx)
			r.initErr = fmt.Errorf("compile shellcheck.wasm: %w", err)
			return
		}

		// Instantiate a single reactor module instance and keep it for reuse.
		modCfg := wazero.NewModuleConfig().
			WithName("shellcheck-reactor").
			WithStdin(io.Reader(strings.NewReader(""))).
			WithStdout(io.Discard).
			WithStderr(io.Discard)

		mod, err := rt.InstantiateModule(initCtx, compiled, modCfg)
		if err != nil {
			_ = compiled.Close(initCtx)
			_ = rt.Close(initCtx)
			r.initErr = fmt.Errorf("instantiate reactor: %w", err)
			return
		}

		// WASI reactor modules export _initialize and require the host to call it once.
		initFn := mod.ExportedFunction("_initialize")
		if initFn == nil {
			_ = mod.Close(initCtx)
			_ = compiled.Close(initCtx)
			_ = rt.Close(initCtx)
			r.initErr = errors.New("_initialize not exported")
			return
		}
		if _, err := initFn.Call(initCtx); err != nil {
			_ = mod.Close(initCtx)
			_ = compiled.Close(initCtx)
			_ = rt.Close(initCtx)
			r.initErr = fmt.Errorf("_initialize: %w", err)
			return
		}

		// Initialize the GHC RTS (once per module instance).
		hsInit := mod.ExportedFunction("hs_init")
		if hsInit == nil {
			_ = mod.Close(initCtx)
			_ = compiled.Close(initCtx)
			_ = rt.Close(initCtx)
			r.initErr = errors.New("hs_init not exported")
			return
		}
		if _, err := hsInit.Call(initCtx, 0, 0); err != nil {
			_ = mod.Close(initCtx)
			_ = compiled.Close(initCtx)
			_ = rt.Close(initCtx)
			r.initErr = fmt.Errorf("hs_init: %w", err)
			return
		}

		scAlloc := mod.ExportedFunction("sc_alloc")
		scCheck := mod.ExportedFunction("sc_check")
		scFree := mod.ExportedFunction("sc_free")
		if scAlloc == nil || scCheck == nil || scFree == nil {
			_ = mod.Close(initCtx)
			_ = compiled.Close(initCtx)
			_ = rt.Close(initCtx)
			r.initErr = errors.New("missing exports: need sc_alloc, sc_check, sc_free")
			return
		}

		r.rt = rt
		r.compiled = compiled
		r.mod = mod
		r.mem = mod.Memory()
		r.scAlloc = scAlloc
		r.scCheck = scCheck
		r.scFree = scFree
	})
	return r.initErr
}

// callReactor runs sc_check against the single instantiated reactor module.
func (r *Runner) callReactor(ctx context.Context, script string, opts Options) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if r.mod == nil {
		return nil, errors.New("shellcheck reactor not initialized")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Use a non-cancelable context for deferred sc_free calls so that WASM
	// memory is freed even if the request context is cancelled mid-flight.
	cleanupCtx := context.WithoutCancel(ctx)

	optsStr := buildOpts(opts)

	// Allocate and write script into WASM linear memory.
	scriptPtr, err := wasmAlloc(ctx, r.scAlloc, len(script))
	if err != nil {
		return nil, err
	}
	defer r.freePtr(cleanupCtx, scriptPtr)
	if !r.mem.Write(scriptPtr, []byte(script)) {
		return nil, errors.New("failed to write script to WASM memory")
	}

	// Allocate and write options.
	optsPtr, err := wasmAlloc(ctx, r.scAlloc, len(optsStr))
	if err != nil {
		return nil, err
	}
	defer r.freePtr(cleanupCtx, optsPtr)
	if !r.mem.Write(optsPtr, []byte(optsStr)) {
		return nil, errors.New("failed to write opts to WASM memory")
	}

	// Allocate space for the output length (4 bytes for CInt).
	outLenPtr, err := wasmAlloc(ctx, r.scAlloc, 4)
	if err != nil {
		return nil, err
	}
	defer r.freePtr(cleanupCtx, outLenPtr)

	// Call sc_check(scriptPtr, scriptLen, optsPtr, optsLen, outLenPtr) → resultPtr.
	results, err := r.scCheck.Call(ctx,
		uint64(scriptPtr), uint64(len(script)),
		uint64(optsPtr), uint64(len(optsStr)),
		uint64(outLenPtr),
	)
	if err != nil {
		return nil, fmt.Errorf("sc_check: %w", err)
	}
	resultPtr := uint32(results[0])
	defer r.freePtr(cleanupCtx, resultPtr)

	// Read output length.
	outLenBytes, ok := r.mem.Read(outLenPtr, 4)
	if !ok {
		return nil, errors.New("failed to read output length from WASM memory")
	}
	outLen := binary.LittleEndian.Uint32(outLenBytes)

	// Read JSON result — copy before freeing the buffer.
	jsonView, ok := r.mem.Read(resultPtr, outLen)
	if !ok {
		return nil, fmt.Errorf("failed to read %d bytes of JSON from WASM memory at ptr %d", outLen, resultPtr)
	}
	result := make([]byte, len(jsonView))
	copy(result, jsonView)

	return result, nil
}

func (r *Runner) freePtr(ctx context.Context, ptr uint32) {
	if r.scFree == nil || ptr == 0 {
		return
	}
	if _, err := r.scFree.Call(ctx, uint64(ptr)); err != nil {
		// Best-effort: failures can happen after a trap closes the module.
		return
	}
}

// wasmAlloc calls sc_alloc to allocate n bytes in WASM linear memory.
func wasmAlloc(ctx context.Context, scAlloc api.Function, size int) (uint32, error) {
	results, err := scAlloc.Call(ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("sc_alloc(%d): %w", size, err)
	}
	return uint32(results[0]), nil
}

// buildOpts generates the line-protocol string consumed by the Reactor.hs options parser.
func buildOpts(opts Options) string {
	var b strings.Builder

	if opts.Dialect != "" {
		b.WriteString("dialect ")
		b.WriteString(opts.Dialect)
		b.WriteByte('\n')
	}
	if opts.Severity != "" {
		b.WriteString("severity ")
		b.WriteString(opts.Severity)
		b.WriteByte('\n')
	}
	if opts.Norc {
		b.WriteString("norc\n")
	}
	if opts.ExtendedAnalysis != nil && *opts.ExtendedAnalysis {
		b.WriteString("extended-analysis\n")
	}
	for _, name := range opts.EnableOptional {
		b.WriteString("enable ")
		b.WriteString(name)
		b.WriteByte('\n')
	}
	for _, code := range opts.Include {
		b.WriteString("include ")
		b.WriteString(strings.TrimPrefix(code, "SC"))
		b.WriteByte('\n')
	}
	for _, code := range opts.Exclude {
		b.WriteString("exclude ")
		b.WriteString(strings.TrimPrefix(code, "SC"))
		b.WriteByte('\n')
	}

	return b.String()
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

	if err := os.MkdirAll(cacheDir, 0o750); err != nil { //nolint:gosec // G703: cacheDir from os.UserCacheDir + constant subpath
		return nil
	}

	cache, err := wazero.NewCompilationCacheWithDir(cacheDir)
	if err != nil {
		return nil
	}
	return cache
}
