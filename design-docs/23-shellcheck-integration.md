# ShellCheck Integration — Design & Roadmap

> Status: proposal  
> Research date: 2026-02-24  
> Upstream references (source dive):
>
> - hadolint/hadolint @ `0d4f787`
> - koalaman/shellcheck @ `29f0d8d` (ShellCheck `0.11.0`)
> - wasilibs/go-shellcheck @ `7683e26`

Hadolint achieves most of its “shell linting” by integrating **ShellCheck** and running it on shell code embedded inside Dockerfile `RUN`
instructions. Tally currently uses `mvdan.cc/sh/v3` for parsing shell snippets (command extraction, wrappers, position tracking) and implements
increasingly sophisticated shell-aware Dockerfile rules, but it does **not** run ShellCheck today.

This document:

1. Pins down **exactly what Hadolint checks** (and what it intentionally skips).
2. Pins down **what ShellCheck can output** (especially JSON1 + fixes) and which knobs Hadolint uses.
3. Compares ShellCheck’s capabilities to `mvdan.cc/sh/v3`.
4. Proposes an implementation path for **feature parity** and a realistic roadmap to go further:
   - More Dockerfile instructions (shell form beyond `RUN`)
   - Better locations (line/column mapping into continued instructions / heredocs)
   - Deterministic auto-fixes where feasible
5. Defines a **stable rule namespace** for ShellCheck findings in Tally (`shellcheck/SC####`) so we don’t tie ShellCheck semantics to the
   `hadolint/*` compatibility layer.

---

## Executive summary

- **Implementing ShellCheck in Go is not realistic** (hundreds of diagnostics + CFG/dataflow + a large test corpus). Don’t try to “port ShellCheck”
  for parity.
- With Tally distributed as **GPLv3**, we can ship **a single Tally binary** that embeds **ShellCheck (GPLv3) compiled to WASI WebAssembly** and
  executes it via **wazero**. This removes the “users must install shellcheck” friction while keeping full upstream parity (we’re literally running
  ShellCheck).
- The parity approach becomes:
  1. Keep `mvdan.cc/sh/v3` for parsing + our Dockerfile-specific analyses + safe, formatting-aware edits.
  2. Add `shellcheck/ShellCheck` that executes the embedded `shellcheck.wasm` with `-f json1` (ranges + fix replacements).
  3. Upgrade inline ignore directives to apply to the **next instruction range**, not just the next line, so precise ShellCheck locations behave
     like users expect on multi-line `RUN` / heredocs.
- “Go further” than Hadolint is feasible once embedded ShellCheck exists:
  - Lint **shell-form `CMD`/`ENTRYPOINT`/`HEALTHCHECK CMD`** (Hadolint doesn’t)
  - Lint **BuildKit `RUN` heredoc scripts** (`RUN <<EOF … EOF`) with correct per-line locations
  - Surface ShellCheck’s fix replacements as `SuggestedFix` (default: suggestion-only; optional apply)

---

## Goals

- **Parity (Hadolint):** produce `SC####` diagnostics for shell code inside Dockerfile `RUN` instructions with equivalent defaults/exclusions.
- **Stable naming:** emit ShellCheck findings under the `shellcheck/*` namespace (not `hadolint/*`):
  - `shellcheck/SC2086` for the ShellCheck diagnostic
  - keep Hadolint directives (`# hadolint ...`) as a compatibility input format only
- **Better than Hadolint:** report **precise line/column** within multi-line `RUN` and heredoc scripts (not just the `RUN` line).
- **More coverage:** lint additional Dockerfile instructions with shell-form semantics (phase-gated).
- **Auto-fix (where possible):** surface ShellCheck fix suggestions (JSON1 includes replacements) without requiring AI.
- **Deterministic + predictable:** make results stable across environments (avoid `.shellcheckrc` / `SHELLCHECK_OPTS` surprises unless explicitly
  enabled).
- **Single binary:** ship ShellCheck “built-in” (embedded wasm), so users don’t install external tools.

## Non-goals

- Re-implement ShellCheck’s full rule set in Go.
- Perfectly emulate “what `/bin/sh` means inside the base image” without running the image.
- Lint non-POSIX shells (PowerShell/cmd). (Future: separate linters.)

---

## 1) What Hadolint actually checks (ShellCheck integration)

### 1.1 Scope: which Dockerfile instructions

In `Hadolint.Rule.Shellcheck`:

- Runs ShellCheck on:
  - `RUN …` instructions (shell scripts)
  - `ONBUILD RUN …` (it unwraps ONBUILD and applies the same rule)
- Does **not** run ShellCheck on:
  - Shell-form `CMD`, `ENTRYPOINT`, `HEALTHCHECK CMD` (Hadolint focuses SC-checking on build steps, not runtime commands)

### 1.2 Shell selection (dialect) + “non-POSIX shell” behavior

Hadolint maintains shell state via:

- Dockerfile `SHELL [...]` instruction → updates the active shell for subsequent `RUN`s in the stage.
- `# hadolint shell=<name>` pragma → sets the “default shell” for new stages and the current shell.
  - Commonly used for Windows images to prevent false positives.

It **skips ShellCheck entirely** when the selected shell looks non-POSIX (substring match against `pwsh`, `powershell`, `cmd`).

It also skips if the script begins with an explicit `#!` shebang that isn’t one of:

- `#!/bin/sh`, `#!/bin/bash`, `#!/bin/ksh`
- `#!/usr/bin/env sh`, `bash`, `ksh`

### 1.3 Environment variable context passed to ShellCheck

Hadolint collects “known env var names” per stage:

- Adds ARG names and ENV keys to the set.
- Always includes a default proxy env set:
  - `HTTP_PROXY`, `http_proxy`, `HTTPS_PROXY`, `https_proxy`, `FTP_PROXY`, `ftp_proxy`, `NO_PROXY`, `no_proxy`

When calling ShellCheck, Hadolint prepends:

- A synthetic shebang derived from the current shell (first word, e.g. `/bin/sh` from `"/bin/sh -c"`).
- `export VAR=1` lines for each known env var name.
- Then the original script text.

This avoids false positives like “variable is referenced but not assigned” for variables Docker already provides via `ARG`/`ENV`.

### 1.4 ShellCheck configuration knobs Hadolint uses

Hadolint uses ShellCheck as a library (not a subprocess) and sets:

- `check-sourced` = false (doesn’t report issues from sourced files)
- `min severity` = style (includes everything)
- `optional checks` = none enabled
- `rc file` = effectively disabled (mocked system interface returns no config)
- Excluded warnings (by numeric code):
  - **SC2187**: “Ash scripts will be checked as Dash. Add ‘# shellcheck shell=dash’ to silence.”
  - **SC1090/SC1091**: “ShellCheck can’t follow non-constant source … / Not following: …”

Rationale: these require ShellCheck directives or external file access which are awkward / non-hermetic in Dockerfile `RUN` contexts.

**Net result:** Hadolint runs ShellCheck’s *default* ruleset (i.e., all non-optional checks for the selected dialect, at all severities), then
drops findings for `SC2187`, `SC1090`, and `SC1091`.

### 1.5 Locations and fixes: where Hadolint falls short (and where Tally can do better)

Hadolint converts ShellCheck `PositionedComment` into a Dockerfile failure:

- Code becomes `SC####`.
- Severity is mapped from ShellCheck’s level.
- **Line number is the Dockerfile instruction line**, *not* the ShellCheck-reported line/column inside the script.
- ShellCheck fix suggestions (`pcFix`) are ignored (Hadolint does not auto-fix SC issues).

**Opportunity for Tally:** our SourceMap-based infrastructure can map ShellCheck’s `(line,column,endLine,endColumn)` into the Dockerfile precisely,
including for continued lines and heredocs, and we can surface fix replacements as `SuggestedFix`.

---

## 2) What ShellCheck can actually output (and what we can consume)

### 2.1 Dialects supported

ShellCheck supports (CLI `-s/--shell=`):

- `sh` (POSIX sh)
- `bash`
- `dash`
- `ksh`
- `busybox`

This is a key mismatch with “Docker default shell is `/bin/sh -c`”: `/bin/sh` varies by base image (dash on Debian/Ubuntu, busybox on Alpine,
bash on some images, etc.). Static linting must either:

- default to `sh` (portable but may emit portability warnings), or
- use hints (`SHELL [...]`, `# tally shell=...`, heuristics from base image), or
- be configurable.

### 2.2 Exit codes (important for any integration)

From `shellcheck.hs`:

- `0`: no problems
- `1`: some problems (normal “lint found issues”)
- `2`: runtime exception
- `3`: syntax failure / CLI usage errors
- `4`: unsupported flags / format errors

Integration must treat **exit code 1 as “success + findings”** and only treat 2/3/4 as tool failures.

### 2.3 JSON1 format includes ranges *and* fixes

ShellCheck’s `-f json1` formatter includes:

- `line`, `column`, `endLine`, `endColumn`
- `level` (error/warning/info/style)
- `code` (numeric SC code)
- `message`
- `fix.replacements[]` (when available)

Example (real `shellcheck -f json1` output, captured via `go-shellcheck` during research):

```json
{
  "file": "-",
  "line": 1,
  "column": 43,
  "endLine": 1,
  "endColumn": 45,
  "level": "info",
  "code": 2086,
  "message": "Double quote to prevent globbing and word splitting.",
  "fix": {
    "replacements": [
      {
        "line": 1,
        "column": 43,
        "endLine": 1,
        "endColumn": 43,
        "insertionPoint": "afterEnd",
        "precedence": 10,
        "replacement": "\""
      },
      {
        "line": 1,
        "column": 45,
        "endLine": 1,
        "endColumn": 45,
        "insertionPoint": "beforeStart",
        "precedence": 10,
        "replacement": "\""
      }
    ]
  }
}
```

This gives us enough data to produce Tally `TextEdit`s **without** parsing unified diffs.

### 2.4 Environmental determinism pitfalls (must be handled)

ShellCheck CLI implicitly includes:

- `.shellcheckrc` config lookup (unless `--norc` or `--rcfile`)
- `SHELLCHECK_OPTS` environment variable is parsed and prepended to argv

For parity with Hadolint (and stable CI), the default integration should:

- pass `--norc`
- ensure `SHELLCHECK_OPTS` is empty/unset (ideally: don’t inherit host env at all; otherwise override it)

If users want `.shellcheckrc`, make it opt-in.

---

## 3) What `mvdan.cc/sh/v3` gives us (and what it doesn’t)

Tally already depends on `mvdan.cc/sh/v3` (currently `v3.12.0`).

**It provides:**

- A high-quality parser and AST with positions
- A printer/formatter (shfmt is built on it)
- Dialect parsing variants: POSIX, Bash, mksh
- Useful for Dockerfile-specific analyses:
  - command extraction (`apt-get`, `apk`, `yum`, etc.)
  - wrapper handling (`env … cmd`, `bash -c '…'`)
  - targeted edits with stable column mapping (when we preserve the original source)

**It does not provide:**

- Anything close to ShellCheck’s full static analysis suite:
  - portability checks
  - quoting/globbing pitfalls at scale
  - dataflow/CFG analysis (unassigned variables, unreachable code paths, etc.)
  - the breadth of SC rules and their semantics

**Conclusion:** `mvdan.cc/sh/v3` remains the right dependency for parsing and deterministic edits, but it is not a substitute for ShellCheck.

---

## 4) Implementation options (single-binary GPLv3)

### 4.1 Options matrix

| Approach | Parity potential | UX story | Effort | Notes |
|---|---:|---|---:|---|
| **A. Embed ShellCheck via WASI Wasm (wazero), built in-repo** | Full | **Single binary**, no user installs | Med | Run `shellcheck.wasm` with `-f json1`; embed bytes; pin versions for determinism |
| **B. External `shellcheck` subprocess (dev/debug mode)** | Full | Requires `shellcheck` installed | Low | Useful for debugging and comparing output vs embedded build |
| **C. Implement a small subset of SC rules in Go** | Partial | Single binary | High | Not parity; only worth it for a small “no-ShellCheck” baseline |

### 4.2 Recommendation

1. **Implement A as the default** (embedded `shellcheck.wasm`), to meet “no external dependency” UX while keeping full upstream parity.
2. Keep **B as an opt-in debug feature** (useful locally and for bisecting “embedded vs upstream” behavior).
3. Treat **C as optional** and narrowly scoped (a handful of high-signal checks that are easy to implement and auto-fix with `mvdan.cc/sh/v3`).

---

## 5) Proposed Tally design (embedded ShellCheck.wasm)

### 5.0 Rule namespace (`shellcheck/*`)

ShellCheck is its own ecosystem with stable, widely-recognized diagnostic codes (`SC####`). Even though Hadolint is a major consumer of ShellCheck,
Tally should treat ShellCheck as a first-class integration and expose findings under their own namespace:

- Emit violations as `shellcheck/SC2086` (not `hadolint/SC2086`).
- Continue to treat Hadolint-related items as *compatibility inputs*:
  - `# hadolint ignore=...` / `# hadolint shell=...` should remain supported for migration.
  - Internally, those map onto `tally` directives and the `shellcheck/*` rule namespace.

This keeps our future “replace Hadolint” path clean: the `hadolint/*` namespace becomes purely “Hadolint rule compatibility”, while ShellCheck
remains ShellCheck.

### 5.1 User-facing behavior (v1)

- New rule aggregator: `shellcheck/ShellCheck`
  - When enabled, it executes the embedded ShellCheck engine and emits violations with rule codes:
    - `shellcheck/SC####` (ShellCheck diagnostics live in their own namespace).
- Default exclusions match Hadolint:
  - `SC2187`, `SC1090`, `SC1091`
- Default shell dialect resolution:
  - determined per stage using semantic model (`SHELL [...]` + `# tally shell=` / `# hadolint shell=`)
  - non-POSIX shells skip
- Output locations:
  - Phase 1: map findings to the `RUN` instruction start line (Hadolint parity)
  - Phase 2: map findings to exact line/column inside `RUN` (requires directive range upgrade; see §6)

### 5.2 Configuration sketch

TOML (proposed; names intentionally mirror ShellCheck CLI):

```toml
[rules.shellcheck.ShellCheck]
severity = "warning" # or "off"
mode = "embedded"              # fixed in v1; reserve "external" for debug
norc = true                    # default true (match Hadolint hermetic behavior)
enable-optional = []            # default empty
extended-analysis = "auto"      # auto|true|false
exclude = ["SC2187", "SC1090", "SC1091"]
include = []                    # mutually exclusive with exclude when set
```

Implementation note: this requires adding a `Shellcheck map[string]RuleConfig` to `internal/config.RulesConfig` (parallel to `Hadolint` and
`Buildkit`), so that selection patterns like `shellcheck/*` and per-rule blocks like `[rules.shellcheck.SC2086]` work end-to-end.

Per-code override still works with existing config plumbing:

```toml
[rules.shellcheck.SC2086]
severity = "off"
```

### 5.3 Dialect mapping (stage shell → `shellcheck -s`)

Map the stage’s effective shell executable basename:

- `bash`, `zsh` → `bash` (ShellCheck does not support zsh; bash is closest)
- `sh` → `sh`
- `dash` → `dash`
- `ash` → `busybox` (best available match)
- `ksh`, `mksh` → `ksh`
- `pwsh`, `powershell`, `cmd` → **skip**

### 5.4 Environment variables passed to ShellCheck

For parity with Hadolint (and fewer false positives):

- Collect env var names available at the point of each `RUN`:
  - stage effective env keys from semantic model (includes inherited stage env)
  - declared `ARG` names in scope
  - plus a small built-in baseline (`PATH`, proxy vars)
- Prepend a deterministic header:
  - `export VAR=1` for each key (sorted)
  - track `headerLineCount` to shift positions back after parsing JSON1

**Alternative:** don’t inject and instead exclude SC2154/SC2155 etc, but that loses useful checks. Hadolint’s export-prelude approach is better.

### 5.5 Embedded ShellCheck (WASI Wasm)

#### Build artifact (`shellcheck.wasm`)

We build `shellcheck.wasm` **in this repo** (not via `wasilibs/go-shellcheck`) and embed it into the Tally binary.

Suggested layout:

- `_tools/shellcheck-wasm/Dockerfile` (builds `shellcheck.wasm`)
- `_tools/shellcheck-wasm/patches/*.patch` (optional upstream feature-trim patches; applied during wasm build)
- `Makefile` (pins upstream ShellCheck tag via `SHELLCHECK_VERSION`, e.g. `v0.11.0`)
- `internal/shellcheck/wasm/shellcheck.wasm` (generated artifact; checked in for reproducible builds)
- `internal/shellcheck/wasm/wasm.go` (`//go:embed shellcheck.wasm`)

Update workflow (proposed):

- `make update-shellcheck-wasm` (requires Docker) rebuilds the wasm and writes `internal/shellcheck/wasm/shellcheck.wasm`.
- `make update-shellcheck-wasm-host` (macOS arm64; no Docker) rebuilds the wasm and writes `internal/shellcheck/wasm/shellcheck.wasm`
  using a repo-local `ghc-wasm-meta` toolchain installed under `bin/`.
- Keep the generated wasm checked in so `go build ./...` works without requiring a Haskell/WASI toolchain.

Example (one possible implementation):

```bash
docker build -t tally-shellcheck-wasm -f _tools/shellcheck-wasm/Dockerfile _tools/shellcheck-wasm
docker run --rm -v "$PWD/internal/shellcheck/wasm:/out" tally-shellcheck-wasm
```

Build steps (mirrors the proven `go-shellcheck` approach):

- Base image: `dhi.io/debian-base:trixie-dev`
- Install: `build-essential`, `curl`, `git`, `jq`, `unzip`, `xz-utils`, `zstd`
- Fetch and set up `ghc-wasm-meta` pinned commit (`GHC_WASM_META_COMMIT` in `Makefile`, fetched via git `ADD`)
- Fetch ShellCheck source at the pinned tag (`SHELLCHECK_VERSION` in `Makefile`, fetched via git `ADD`) and run `./striptests`
- (Optional) Apply any patches in `_tools/shellcheck-wasm/patches/*.patch` (in lexical order) via `git apply`
- Build: `. ~/.ghc-wasm/env && wasm32-wasi-cabal update && wasm32-wasi-cabal build --allow-newer shellcheck`
- Optimize: `wasm-opt -O3 --flatten --rereloop --converge ... -o shellcheck.wasm`

Notes:

- Keep `./striptests`: it meaningfully reduces the wasm size.
- Expect a ~7–8MB wasm for ShellCheck `0.11.0` after optimization (the upstream `go-shellcheck` artifact is ~7.3MB).

##### Size optimization notes (beyond `striptests`)

There are **no upstream Cabal flags** to “build only JSON1 output” (or otherwise exclude formatters) — ShellCheck’s main program
(`shellcheck.hs`) imports all formatters directly. To actually drop unused output formats from the final wasm, we would need to **patch**
upstream sources during the build (see below).

However, we can likely get meaningful wins without maintaining a large patch:

- **GHC dead-code / section GC flags:** ShellCheck’s own static Linux builders use:
  - `--ghc-options="-split-sections -optc-Os -optc-Wl,--gc-sections"`
  - For wasm, we should experiment with passing similar `--ghc-options` to `wasm32-wasi-cabal build ...` and measure size + runtime impact.
- **`wasm-opt` stripping:** consider adding `--strip-dwarf` (and/or `--strip-debug`) to the `wasm-opt` step if the produced wasm contains
  debug sections.
- **`wasm-opt` size mode:** `-Oz` may reduce size further, but is likely to hurt performance; our default should stay `-O3` unless we set an
  explicit size budget.

##### Optional: “JSON1-only” ShellCheck build

We can reduce wasm size (at the expense of upgrade friction) by patching upstream sources at build time. In this repo we keep patches as plain
unified diffs (applied with `git apply`) under `_tools/shellcheck-wasm/patches/`.

Current demo patches:

- `_tools/shellcheck-wasm/patches/0001-json1-only.patch`: disable all output formats except JSON1 (unknown formats error)
- `_tools/shellcheck-wasm/patches/0002-stdin-only.patch`: only accept stdin input (no files, or `-`)
- `_tools/shellcheck-wasm/patches/0003-drop-sc2148.patch`: remove SC2148 emission (shebang-related rule) as a demonstration

Size tracking (ShellCheck `v0.11.0`, same toolchain, measured on 2026-02-24):

- Before patches: `7,460,925` bytes
- After patches: `7,317,428` bytes (**-143,497 bytes / -1.92%**)

Notes:

- Disabling formats at the CLI level doesn’t necessarily stop Cabal from *compiling* formatter modules, because they are still listed in
  `ShellCheck.cabal`’s `exposed-modules`. For larger size wins, patch `ShellCheck.cabal` as well (more invasive).
- The wasm build prints `wc -c shellcheck.wasm` at the end; include that number in ShellCheck bump PR descriptions to track regressions.

Maintaining patches across ShellCheck upgrades:

1. Bump `SHELLCHECK_VERSION` in `Makefile`.
2. Run `make update-shellcheck-wasm`.
3. If the build fails during patch application:
   - Clone the new ShellCheck tag locally, run `./striptests`, and manually re-apply the intent of the patch.
   - Regenerate the patch with `git diff` (copy the full diff; don’t truncate hunks) and replace the corresponding file under
     `_tools/shellcheck-wasm/patches/`.

Tooling note:

- We keep patches as unified diffs applied with `git apply` because they’re audit-friendly and don’t require extra tooling in the build image.
- If patch churn becomes painful, consider switching specific patches to AST-based rewrites (e.g. `ast-grep` supports Haskell patterns) and
  emitting diffs from the rewrite step — but this adds another tool to maintain in the build pipeline.

If we ever need to significantly reduce wasm size further, we can make the “JSON1-only” trimming more aggressive:

- Patch upstream `shellcheck.hs` to:
  - hardcode `-f json1` and remove `--format` option parsing
  - remove imports of `ShellCheck.Formatter.*` except `JSON1`
  - potentially remove CLI features we don’t use (`--list-optional`, `--version`, color, other output formats)
- Benefit: may drop some formatter-only dependencies (notably `Diff`) and a chunk of CLI code.
- Cost: this is a real fork surface area; it may conflict on future ShellCheck upgrades and requires ongoing maintenance.

Versioning facts (ShellCheck `0.11.0`, source-dive, reproducible count):

- Distinct diagnostic codes emitted: **428** (110× `SC1xxx`, 318× `SC2xxx`)
- Optional check groups (CLI `--list-optional`): **11**

#### Runtime execution (wazero)

Run the embedded wasm as the ShellCheck CLI, but in-process:

- Compile once per process: `rt.CompileModule(ctx, shellcheckwasm.Binary)`
- For each snippet, instantiate with a fresh `ModuleConfig`:
  - Args: `shellcheck -f json1 -s <dialect> --norc -e SC2187,SC1090,SC1091 -` (read from stdin; add `-i/-o/--extended-analysis` if configured)
  - stdin: the constructed script (shebang + `export …` prelude + snippet)
  - stdout: capture JSON1
  - stderr: capture for debugging / error surfacing
  - env: **do not inherit host env** by default; if we pass env through, explicitly set `SHELLCHECK_OPTS=` (empty) for determinism.
    - Note: in the `go-shellcheck` runner, `PWD` is explicitly skipped because it can cause the Haskell runtime to attempt `chdir`, which is not
      supported in wasip1.
  - syscalls: wazero’s defaults are intentionally sandboxed/deterministic (fake clocks, deterministic random source, nanosleep returns
    immediately). Only enable OS-backed implementations if needed for compatibility:
    - `WithSysNanosleep`, `WithSysNanotime`, `WithSysWalltime`
    - `WithRandSource(rand.Reader)` (only if a nondeterministic random source is required; likely unnecessary for ShellCheck)
- Exit codes (same as native ShellCheck):
  - `0` and `1` are success (`1` means “findings exist”)
  - `2/3/4` are tool failures → surface a single internal diagnostic (and optionally include stderr)

Filesystem mounting:

- For Hadolint parity (`check-sourced=false` and `SC1090/SC1091` excluded), run from stdin and do **not** require filesystem access.
- If we later support “external sources” / `source-path`, mount only the Dockerfile directory and make it explicitly opt-in.

Library shape (what we need from our in-repo runner):

- Cache the compiled module (and ideally the wazero runtime) across many snippet checks.
- Offer a structured API (not `main()`):
  - input: args/dialect + script bytes
  - output: `(exitCode, stdoutJSON, stderrText)` with timeouts / context cancellation

### 5.6 Turning JSON1 comments into Tally violations

For each JSON1 comment:

- RuleCode: `shellcheck/SC` + zero-padded numeric code (e.g. `shellcheck/SC2086`)
- Severity: map `level` → Tally `Severity`
- DocURL:
  - ShellCheck wiki: `https://www.shellcheck.net/wiki/SC2086`
- Location:
  - `(line,column,endLine,endColumn)` adjusted by `headerLineCount`
  - plus Dockerfile instruction start line offset (for shell-form instructions)
- Fix:
  - If `fix.replacements` present:
    - convert replacements → `[]TextEdit` with absolute file ranges
    - mark fix safety conservatively (`FixSuggestion` by default)
    - validate no overlapping edits; otherwise drop the fix (still report the diagnostic)

---

## 6) Prerequisite: inline ignore directives must cover entire next instruction

Today, our next-line directives are literally “next **line** only”:

- `# tally ignore=shellcheck/SC2086` (or `# tally ignore=SC2086`) only suppresses violations whose `Location.Start.Line` is the next non-comment
  line.
- For migration, we should keep accepting `# hadolint ignore=SC2086` as an alias for `# tally ignore=SC2086`.

If we start emitting precise ShellCheck locations inside multi-line `RUN`s, ignores above the `RUN` line will **not** suppress findings on
continuation lines or heredoc content. This is not Hadolint-compatible and will be surprising.

### 6.1 Proposed directive behavior (Hadolint compatible)

Redefine next-line directives as **next-instruction directives**:

- The directive applies to the *entire range* of the next Dockerfile instruction, including:
  - line continuations (`\`-continued Dockerfile lines)
  - BuildKit heredoc blocks attached to the instruction (`RUN <<EOF … EOF`, `COPY <<EOF … EOF`, etc.)

### 6.2 Implementation sketch

- Build an “instruction line span index” per file during linting:
  - for each instruction node, record `startLine..endLine` (0-based)
- In `directive.Parse`, when resolving `AppliesTo`, use the span index:
  - find the next instruction start line
  - set `LineRange{Start: startLine, End: endLine}`

This change is independently valuable even without ShellCheck, since many of our existing rules can (and should) place violations on a specific
continued line once the ignore model matches user expectations.

---

## 7) Going beyond Hadolint (post-parity roadmap)

### Phase 1 (Parity MVP)

- Add `shellcheck/ShellCheck` rule that lints:
  - `RUN` (shell form only)
  - `ONBUILD RUN` (shell form only)
- Match Hadolint defaults:
  - dialect selection
  - excluded codes `[SC2187, SC1090, SC1091]`
  - env var injection via `export VAR=1`
- Report locations at the `RUN` line (until directive range upgrade lands)

**Estimated effort:** ~1–2 weeks (embedding `shellcheck.wasm` + wazero runner + JSON1 plumbing + tests; faster if we check in the generated wasm
and don’t require CI to rebuild it).

### Phase 2 (Better UX: precise locations + directive ranges)

- Implement next-instruction directive spans (§6)
- Emit precise locations for ShellCheck findings (line/column within `RUN`)
- Support ShellCheck fixes as `SuggestedFix` (likely `FixSuggestion`)

**Estimated effort:** ~1–2 weeks (directive span logic + extraction edge cases + tests).

### Phase 3 (More instructions)

Lint shell-form commands in:

- `CMD` (shell form)
- `ENTRYPOINT` (shell form)
- `HEALTHCHECK CMD` (shell form)

These are runtime semantics (not build-time), but linting them is useful and strictly “more” than Hadolint’s default SC integration.

**Estimated effort:** ~1 week (mostly extraction + mapping + tests).

### Phase 4 (Better defaults: base image heuristics)

Improve the default `sh` dialect choice via heuristics:

- If base image tag looks like Alpine → prefer `busybox`
- If base image looks like Debian/Ubuntu → prefer `dash`
- Otherwise default to `sh`

Keep this heuristic **opt-in** or gated behind a “best effort” mode since it can be wrong.

---

## 8) Risks / edge cases (don’t underestimate)

1. **RUN flags** (`RUN --mount=…`, `--network=…`, `--security=…`):
   - Source extraction must blank these out (space-preserving) before passing the script to ShellCheck, otherwise the script starts with
     `--mount=...` and parsing/linting becomes garbage.
2. **Heredoc scripts**:
   - `RUN <<EOF … EOF` should lint the heredoc content (not the redirection expression).
   - Mapping `(line,column)` into the Dockerfile requires the heredoc start line and indentation rules.
3. **Tabs / unicode columns**:
   - ShellCheck JSON1 treats tabs as width 1; Go string indexing is byte-based. This is usually fine for Dockerfiles but can be a footgun.
4. **Fix replacement semantics**:
   - ShellCheck replacements have `precedence` and `insertionPoint`. Our fixer currently has no concept of intra-fix precedence.
   - Start by supporting only non-overlapping replacements; drop fixes that overlap or share the same position.
5. **Wasm runtime constraints + performance**:
   - Wazero module instantiation is not free; compile `shellcheck.wasm` once and reuse the compiled module.
   - WASI preview1 doesn’t support everything a native process does (notably: `chdir`). Avoid passing `PWD` through, and keep FS access opt-in.
   - The GHC WASI toolchain is still “special”; keep it pinned (toolchain commit + ShellCheck tag) and treat upgrades as deliberate changes.
6. **Environment determinism**:
   - `.shellcheckrc` and `SHELLCHECK_OPTS` must be neutralized by default.
7. **Version drift**:
   - ShellCheck’s messages and fixes can change between versions; tests should assert primarily on code + location, and snapshots will need updates
     when `shellcheck.wasm` is bumped.

---

## 9) Suggested test plan

### Unit tests (pure Go)

- Script extraction:
  - `RUN` single-line and multi-line with `\`
  - `RUN --mount=...` and multiple flags
  - `ONBUILD RUN ...` mapping
  - heredoc `RUN <<EOF` extraction + line offsets
- JSON1 parsing:
  - parse fixtures for comments with and without fixes
  - validate replacement → TextEdit conversion (including insertion-only edits)
- Directive spans:
  - ignore directive above a multi-line instruction suppresses findings on all lines in the instruction span

### Integration tests (uses embedded ShellCheck)

- Add `internal/integration/testdata/shellcheck-basic/Dockerfile` with known SC findings.
- Tests run against the embedded `shellcheck.wasm`, so CI does not need to install `shellcheck`.
- Snapshot only:
  - rule code (`shellcheck/SC####`)
  - severity
  - location (line/column)
  - message prefix (avoid full string comparisons if too brittle)

---

## Appendix: GPLv3 compliance notes (embedding ShellCheck)

Embedding `shellcheck.wasm` means the Tally binary distributes ShellCheck “object code” as part of the combined work. With Tally itself being
GPLv3, this is allowed — but we should still make compliance *easy* and auditable:

- Pin the exact upstream ShellCheck version tag used to build the wasm (`SHELLCHECK_VERSION` in `Makefile`).
- Keep the wasm build recipe in-repo (`_tools/shellcheck-wasm/Dockerfile`) and make it reproducible (pinned toolchain commit, pinned build flags).
- When publishing release binaries, publish “Corresponding Source” alongside them (recommended: a `source.tar.gz` release asset) that includes:
  - the Tally source at that release tag
  - the ShellCheck source matching the embedded wasm
  - the wasm build recipe/scripts and version pins

This should be treated as release-engineering, not something end-users need to run locally.

## Appendix: Upgrading ShellCheck (and wiring it to Renovate)

### What’s pinned

We intentionally pin *two* things:

- ShellCheck version tag in `Makefile` (`SHELLCHECK_VERSION`)
- GHC WASI toolchain meta commit in `_tools/shellcheck-wasm/Dockerfile` (`ghc-wasm-meta` commit)

ShellCheck bumps are expected. Toolchain bumps should be rarer and treated as “infrastructure changes”.

### Renovate integration (proposed)

Add a Renovate regex manager for `SHELLCHECK_VERSION` in `Makefile` so we get PRs when ShellCheck releases. Example:

```json
{
  "customType": "regex",
  "managerFilePatterns": ["/^Makefile$/"],
  "matchStrings": ["SHELLCHECK_VERSION\\s*[:=]\\s*(?<currentValue>v\\d+\\.\\d+\\.\\d+)"],
  "depNameTemplate": "koalaman/shellcheck",
  "datasourceTemplate": "github-releases",
  "versioningTemplate": "semver"
}
```

Because the PR requires regenerating a binary wasm artifact, add a packageRule to **disable automerge**:

```json
{
  "matchDepNames": ["koalaman/shellcheck"],
  "automerge": false
}
```

### Upgrade workflow (human-in-the-loop)

When Renovate opens a ShellCheck bump PR:

1. Run `make update-shellcheck-wasm` to rebuild `internal/shellcheck/wasm/shellcheck.wasm`.
2. Run `make test` and update snapshots if needed (`UPDATE_SNAPS=true ...`).
3. Sanity-check that the embedded engine matches upstream behavior:
   - (optional) run native `shellcheck` CLI on the same snippet and diff JSON1 output against our embedded runner.
4. Merge.

If a ShellCheck release changes messages/fixes, expect snapshot churn. Prefer asserting on `(code, severity, location)` in tests and only loosely
on message text (prefix/contains).

## Appendix: ShellCheck “rule count” precision (0.11.0)

ShellCheck doesn’t publish “number of rules” as a stable API surface. The most precise thing to count is **distinct diagnostic codes** it can
emit.

For ShellCheck `0.11.0` (repo `koalaman/shellcheck` at the tag used by `go-shellcheck`), a source-based extraction yields:

- **428** distinct `SC####` codes:
  - 110× `SC1xxx` (mostly parse/lex + some early checks)
  - 318× `SC2xxx` (most semantic checks)
- **11** optional check groups (`--list-optional`)

Re-count command (run in the pinned ShellCheck source tree; prints `428` on `0.11.0`):

```bash
(
  rg --no-filename -o -r '$1' '\\b(?:warn|err|info|style)(?:WithFix)?\\b[^\\n]*?\\b([0-9]{4})\\b' src
  rg --no-filename -o -r '$1' '\\bmakeComment(?:WithFix)?\\b[^\\n]*?\\b([0-9]{4})\\b' src
  rg --no-filename -o -r '$1' '\\bparseProblem(?:AtWithEnd|AtId|At)?\\b[^\\n]*?\\b([0-9]{4})\\b' src
  rg --no-filename -o -r '$1' '\\bparseNote(?:AtWithEnd|AtId|At)?\\b[^\\n]*?\\b([0-9]{4})\\b' src
) | sort -u | wc -l
```

## Appendix: ShellCheck MCP / “official API”

- ShellCheck does **not** provide an official “MCP server” (Model Context Protocol) or a network API.
- The supported integration surfaces are:
  - the **CLI** (`shellcheck`), including `-f json1` output and options like `-e/-i/-o/--norc`
  - the **Haskell library modules** (how Hadolint integrates today)

## Appendix: why we should not depend on `wasilibs/go-shellcheck`

`wasilibs/go-shellcheck` is a great “ShellCheck as a Go CLI distribution”, but it’s not a great *dependency* for Tally’s needs:

- We need a **library-shaped API** (run many snippets, cache compiled modules, controlled env/FS, structured error handling), not a `main()`.
- We want to **own and pin the Wasm build** (ShellCheck tag + toolchain commit + wasm-opt flags), to keep results deterministic and updates
  intentional.
- We want to avoid inheriting CLI distribution constraints (e.g. filesystem sandbox assumptions, env passthrough) unless we explicitly choose them.

We can still borrow the *approach* (GHC WASI → `shellcheck.wasm` + wazero execution), but keep the implementation in-repo.
