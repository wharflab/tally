# ShellCheck WASM Reactor: Single-Instance Reuse Investigation

## Goal

Replace the ShellCheck WASM command module (which re-instantiates per call with
stdin/stdout) with a reactor module where a single long-lived instance handles
all `sc_check` calls via FFI, eliminating per-call instantiation overhead.

## Update (2026-02-28)

Root cause: the host never called the WASI reactor `_initialize` export after
instantiation. This module has no WASM start section, so wazero will not run
reactor initialization automatically.

Without `_initialize`, the third varied `sc_check` call deterministically traps
with `wasm error: invalid table access`, and the subsequent `sc_alloc` calls
fail because WASI `proc_exit` closes the module instance.

Fix: call `_initialize` exactly once immediately after instantiation, call
`hs_init(0, 0)` once, and for each check allocate buffers and `sc_free` them.
This is implemented in `internal/shellcheck/runner.go` and covered by
`TestReactorSingleInstanceDifferentScripts`.

## Reactor Architecture

The reactor module (`Reactor.hs`) exports four FFI functions:

- `hs_init(0, 0)` — initialize the GHC RTS (called once after instantiation)
- `sc_alloc(n)` → `Ptr` — allocate n bytes in WASM linear memory (`mallocBytes`)
- `sc_check(scriptPtr, scriptLen, optsPtr, optsLen, outLenPtr)` → `Ptr` — run
  ShellCheck on a script, return pointer to JSON result
- `sc_free(ptr)` — free a buffer (`Foreign.Marshal.Alloc.free`)

Options are passed as a line-based text protocol (`"dialect sh\nseverity style\nnorc\n"`)
instead of CLI args. JSON output is hand-serialized in Haskell (no aeson in the hot path).

The intended single-instance flow:

```text
compile module (once, cached)
  → instantiate (once)
    → _initialize (once)
    → hs_init (once)
      → for each script:
          sc_alloc inputs → write inputs → sc_check → read result → sc_free all
```

## The Problem

When reusing a single reactor instance across multiple calls with **varied input
scripts** **without calling `_initialize`**, the module crashes with
`wasm error: invalid table access` after approximately 2 successful calls. The
crash is deterministic and reproducible.

### Minimal Reproduction

```go
func TestReactorDifferentScripts(t *testing.T) {
    r := NewRunner() // single-instance runner
    ctx := context.Background()

    prelude := "#!/bin/sh\n" +
        "export FTP_PROXY=1\nexport HTTPS_PROXY=1\nexport HTTP_PROXY=1\n" +
        "export NO_PROXY=1\nexport PATH=1\nexport ftp_proxy=1\n" +
        "export http_proxy=1\nexport https_proxy=1\nexport no_proxy=1\n"

    scripts := []string{
        prelude + "           echo $1",              // 198 bytes
        prelude + "    echo foo \\\n    && echo $1",  // 209 bytes
        prelude + "            echo $1",              // 199 bytes
        prelude + "    echo $1",                      // 191 bytes
        prelude + "                echo $1",           // 203 bytes
    }

    for i, s := range scripts {
        out, _, err := r.Run(ctx, s, Options{
            Dialect: "sh", Severity: "style", Norc: true, Exclude: []string{"1040"},
        })
        if err != nil {
            t.Logf("call %d (len=%d): ERROR: %v", i, len(s), err)
            continue
        }
        t.Logf("call %d: len=%d comments=%d", i, len(s), len(out.Comments))
    }
}
```

Typical output:

```text
call 0: len=198 comments=1   ← OK
call 1: len=209 comments=1   ← OK
call 2 (len=199): ERROR: sc_check: wasm error: invalid table access
call 3 (len=191): ERROR: sc_alloc(191): runtime error: invalid memory address or nil pointer dereference
call 4 (len=203): ERROR: sc_alloc(203): runtime error: invalid memory address or nil pointer dereference
```

### Observations

1. **Calls 0–1 succeed.** The first two `sc_check` invocations complete normally and
   return correct ShellCheck findings.

2. **Call 2 crashes with `invalid table access`.** This is a WASM trap — a
   `call_indirect` instruction attempted to call through the function table at an
   index that is out of bounds or references a null entry.

3. **Calls 3+ fail with nil `sys.Context`.** After the trap on call 2, wazero's
   WASI `proc_exit` handler closes the module (`mod.CloseWithExitCode`), which
   sets `mod.Sys = nil`. All subsequent WASI calls (e.g., `fd_write` inside
   `sc_alloc` → `mallocBytes`) panic because `mod.Sys.FS()` dereferences nil.

4. **Same script repeated works indefinitely.** If all 5 calls use the identical
   20-byte script `"#!/bin/sh\necho $1\n"`, all succeed. The crash requires
   **varied inputs** of moderate size (~200 bytes with multiple export lines).

5. **Context and goroutine do not matter.** The crash occurs identically with
   `context.Background()`, on a single goroutine, with sequential execution.

### Error Chain

The `invalid table access` on call 2 is the root failure. The subsequent nil
`sys.Context` errors are a consequence: wazero's WASI `proc_exit` implementation
(`imports/wasi_snapshot_preview1/proc.go:33`) calls `mod.CloseWithExitCode(ctx, exitCode)`
when the GHC RTS error handler invokes `proc_exit`. This permanently destroys the
module instance (`module_instance.go:155` sets `m.Sys = nil`).

## Environment

- Go: 1.26.0 darwin/arm64
- wazero: v1.11.0 (latest release as of 2026-02-28)
- GHC WASM: 9.14.1.20260213 via ghc-wasm-meta commit `4e1f900e`
- ShellCheck: 0.11.0
- wasm-opt: binaryen (version bundled with ghc-wasm-meta)

## Theories Investigated

### 1. ast-grep Source Rewrites Corrupting Library Code

**Theory:** The 7 ast-grep rewrites that modify `shellcheck.hs`,
`src/ShellCheck/Analytics.hs`, and `src/ShellCheck/Parser.hs` might remove
functions that are still reachable through indirect dispatch tables, leaving
dangling entries in the WASM function table.

**Test:** Built the reactor binary with zero rewrites applied (only `striptests`).

**Result:** Same crash pattern. The `invalid table access` occurs at different
WASM function indices (as expected with a different binary) but with the same
call pattern (calls 0–1 succeed, call 2 crashes).

### 2. wasm-opt Optimization Corrupting Function Tables

**Theory:** The `wasm-opt --flatten --rereloop --converge -O3` pass might
incorrectly transform indirect call targets or function table entries.

**Test:** Built without wasm-opt (raw GHC linker output, 12 MB binary).

**Result:** Same crash. The unoptimized binary provides better stack traces
with symbol names:

```text
shellcheck-reactor.wasm.StgRun(i32,i32) i32
shellcheck-reactor.wasm.scheduleWaitThread(i32,i32,i32)
shellcheck-reactor.wasm.rts_inCall(i32,i32,i32)
shellcheck-reactor.wasm.sc_check(i32,i32,i32,i32,i32) i32
```

The crash originates in `StgRun`, the GHC RTS's STG machine evaluator.

### 3. Hand-Rolled JSON Serialization Bug

**Theory:** The custom `encodeResult`/`pokeCString` functions in `Reactor.hs`
might corrupt WASM memory through incorrect pointer arithmetic or buffer overflows.

**Test:** Replaced hand-rolled JSON with `Data.Aeson.encode` + `Data.ByteString`

- `Foreign.Marshal.Utils.copyBytes`. Added `aeson` and `bytestring` to the
reactor's cabal build-depends.

**Result:** Same crash. Different WASM function indices confirm a different
binary was produced, but the failure pattern is identical. The crash occurs
before serialization — inside `checkScript` itself.

### 4. Haskell Laziness / Thunk Accumulation

**Theory:** Lazy `let` bindings in `sc_check` create thunk chains that retain
references to data from previous calls, causing heap corruption when the GC
runs during a later call.

**Test:** Added `{-# LANGUAGE BangPatterns #-}` and strict `!` annotations on
all intermediate values (`opts`, `spec`, `result`, `jsonStr`, `jsonLen`).

**Result:** Same crash. Strict evaluation does not change the failure pattern.

### 5. Cross-Goroutine WASI Context Loss

**Theory:** When `runTasks` dispatches shellcheck calls to worker goroutines,
the WASI `sys.Context` (file descriptors) bound during module instantiation
might not be accessible from other goroutines.

**Test:** Made `runTasks` sequential (single goroutine), and separately tried
`context.Background()` for all WASM calls.

**Result:** Same crash even with sequential single-goroutine execution using
`context.Background()`. The goroutine/context theory is not the cause of the
`invalid table access`, though it is a real concern for the `sys.Context` nil
panic (which is a secondary failure after the module is closed by the trap).

### 6. C Heap Fragmentation from sc_alloc/sc_free

**Theory:** Per-call `malloc`/`free` cycles fragment the C heap in WASM linear
memory, eventually causing the C heap to grow into the GHC heap or corrupt
heap metadata.

**Test:** Tried multiple approaches:

- Pre-allocated reusable WASM buffers (grow-only, never freed)
- Per-call allocation with no freeing (leak all buffers)
- Different allocation/free ordering

**Result:** Pre-allocated buffers produced *fewer* findings (1 instead of 4),
suggesting buffer reuse interacts poorly with the WASM memory model. Leak-all
and different free ordering did not prevent the crash.

### 7. wazero Bug with Go 1.24+ ABI Changes

**Theory:** wazero issue [#2375](https://github.com/tetratelabs/wazero/issues/2375)
documents `invalid table access` crashes with Go 1.24+ on amd64, fixed by
PR [#2378](https://github.com/tetratelabs/wazero/pull/2378) which reserves
the DX register when calling `memmove`.

**Relevance:** The fix is amd64-specific. Our environment is arm64. wazero
v1.11.0 includes the amd64 fix but there may be analogous arm64 issues with
Go 1.26's ABI. The wazero issue [#2083](https://github.com/tetratelabs/wazero/issues/2083)
also documents `invalid table access` when calling the same module instance
concurrently, though our reproduction is single-threaded.

## Root Cause Assessment

The unoptimized stack trace points to `StgRun` as the crash site. `StgRun` is
the GHC RTS's core eval/apply loop for the STG machine. It uses `call_indirect`
extensively to enter closures and dispatch case alternatives.

The crash pattern (works for N identical calls, fails on the Nth varied call)
suggests that the GHC RTS accumulates state — likely in the Haskell heap,
nursery, or remembered set — across `rts_inCall` invocations. When a
sufficiently different call triggers a GC or heap expansion, the accumulated
state leads to a stale or corrupt info pointer being used as a `call_indirect`
table index.

This could be:

- A GHC WASM backend bug in how `rts_inCall`/`rts_inCallEnd` manage the
  capability and nursery between FFI re-entries
- A wazero compiler bug in how the function table is accessed after memory
  growth (`memory.grow` changes the linear memory buffer)
- An interaction between the two that does not manifest with identical inputs
  (which produce identical heap layouts each time)

## Resolution

Single-instance reuse works correctly after calling `_initialize`. The
previous "per-call instantiation" workaround was replaced by the current
approach in `internal/shellcheck/runner.go`:

```text
compile module (once, disk-cached by wazero)
  → instantiate (once)
    → _initialize (once)
    → hs_init (once)
      → for each script (mutex-serialized):
          sc_alloc inputs → write inputs → sc_check → read result → sc_free all
```

Advantages over the old command module:

- No CLI arg parsing or format dispatch
- No stdin/stdout buffering
- Hand-serialized JSON (no aeson in hot path)
- Smaller binary (6.6 MB vs 7.0 MB)
- Zero per-call instantiation overhead

This is fast enough that the LSP server no longer needs the two-pass
diagnostics workaround (fast pass without ShellCheck + debounced full pass).
All diagnostics now run in a single inline pass.

Validated by `TestReactorSingleInstanceDifferentScripts` which runs 5 varied
scripts on a single runner instance — the exact scenario that previously crashed.
