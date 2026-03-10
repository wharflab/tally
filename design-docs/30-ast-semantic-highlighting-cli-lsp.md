# AST-Aware Semantic Highlighting for CLI Snippets and LSP

> Status: implemented for Dockerfile, POSIX shell, PowerShell, CLI rendering, and LSP `semanticTokens/full|range`
>
> Scope: replace Chroma in CLI snippet rendering, add a shared semantic token engine, and expose semantic tokens from the existing LSP server

## Executive Summary

`tally` already has richer structure than a generic syntax highlighter:

- BuildKit Dockerfile AST and typed instructions
- `internal/semantic` stage and shell model
- `internal/shell` shell-variant detection and `mvdan.cc/sh/v3` parsing
- `internal/sourcemap` line/offset mapping
- shell script extraction logic already implemented in `internal/rules/shellcheck/script.go`

Today the text reporter still highlights snippets with `github.com/alecthomas/chroma/v2`, one line at a time, with no Dockerfile-aware token model and
no reuse for LSP. The proposal is to replace that with a shared, AST-aware highlighter that serves two consumers:

1. CLI snippet rendering in `internal/reporter/text.go`
2. LSP semantic token handlers in `internal/lspserver`

The important design constraint is pragmatism: v1 should stay pure Go, reuse existing repo primitives, and ship incrementally. In particular:

- v1 should deliver shared tokenization plus `textDocument/semanticTokens/full` and `range`
- `full/delta` should be phase 2, after token normalization and caching are proven stable
- v1 can ship with conservative lexical fallback for PowerShell and `cmd`; follow-up work may upgrade specific dialects behind the shared shell
  tokenizer boundary
- Zed grammar de-bundling should not be part of the initial delivery

---

## 1. Current State

Relevant code that already exists:

- `internal/reporter/text.go`
  - uses Chroma for per-line Dockerfile highlighting
  - applies line-level `>>>` markers, but not exact span overlays
- `internal/lspserver/server.go`
  - advertises diagnostics, formatting, code actions, and execute-command support
  - already uses generated 3.17-era protocol types that include semantic token messages
- `internal/sourcemap/sourcemap.go`
  - provides line text, offsets, and end-line expansion for continuations
- `internal/rules/shellcheck/script.go`
  - already extracts shell-form scripts and heredoc bodies and maps them back to Dockerfile lines
- `internal/shell/shell.go`
  - already models shell variants (`bash`, `posix`, `mksh`, `powershell`, `cmd`, `unknown`)
  - already exposes shebang detection and mvdan-backed parsing variants
- `_integrations/zed-tally/`
  - currently bundles Dockerfile language assets and grammar configuration

This means the proposal does not need to invent new foundations. It mainly needs a shared token model, normalization rules, renderer, and LSP
adapters.

---

## 2. Problems With the Current Approach

1. Chroma is lexer-based and Dockerfile-generic. It does not understand stage aliases, directive comments, heredoc boundaries, or BuildKit-specific
   flags with the same precision as our own parser stack.
2. The reporter highlights whole lines independently, so multi-line instructions, embedded shell, and exact rule spans cannot be rendered precisely.
3. `tally` already pays the cost of Dockerfile parsing and shell analysis, but the reporter discards that structure and re-lexes plain text.
4. The LSP server has no semantic token support, so editor highlighting cannot reuse the same semantics as CLI snippets.
5. The original draft bundled too many concerns into one release: reporter rewrite, LSP full/range/delta, Zed grammar changes, and phase-2 PowerShell
   parser strategy. That is too much coupling for an initial implementation.

---

## 3. Goals

1. Remove direct dependency on `github.com/alecthomas/chroma/v2`.
2. Keep Lip Gloss as the terminal rendering layer.
3. Build one shared semantic token pipeline for Dockerfile and embedded shell snippets.
4. Reuse existing `internal/sourcemap`, `internal/shell`, `internal/semantic`, and shell-script extraction logic.
5. Improve CLI output by highlighting the exact violation span, not only the affected lines.
6. Add LSP semantic tokens for full-document and range requests.
7. Keep v1 pure Go and portable across current repo targets.

---

## 4. Non-Goals (v1)

1. Full AST-backed `cmd.exe` parsing.
2. User-configurable multi-theme system beyond `auto|dark|light`.
3. Changing diagnostics, fixes, or rule semantics.
4. Zed grammar de-bundling as part of the initial release.
5. Mandatory support for `textDocument/semanticTokens/full/delta` in the first shipped version.

---

## 5. Key Decisions

### 5.1 One engine, two adapters

The highlighter should produce a normalized token stream once, then hand it to:

- an ANSI renderer for CLI snippets
- an LSP encoder for semantic tokens

The CLI and LSP paths must not each invent their own tokenization rules.

### 5.2 Reuse repo primitives instead of duplicating them

The token engine should build on:

- `dockerfile.ParseResult`
- `sourcemap.SourceMap`
- `semantic.Model` when available
- `shell.Variant`
- extracted script mappings from the existing ShellCheck helper logic

A design that re-parses raw source from scratch without using those inputs would be a regression.

### 5.3 Normalize once, then render/encode

LSP semantic tokens must be sorted and non-overlapping. CLI rendering also becomes much simpler if all overlaps are resolved before rendering. The
design therefore needs an explicit normalization stage, not just ad hoc per-consumer cleanup.

### 5.4 Keep v1 pure Go

PowerShell tree-sitter or other cgo-backed parsers are plausible future work, but they should not block the initial replacement of Chroma.

### 5.5 Phase the rollout

Recommended phases:

1. Shared token model + CLI renderer
2. LSP `semanticTokens/full` and `range`
3. LSP `full/delta` cache and delta computation
4. Optional future dialect-specific parser expansion

---

## 6. Proposed Package Layout

Introduce a new package family under `internal/highlight/`.

- `internal/highlight/core`
  - token types, modifiers, ranges, normalization, sorting, clipping
- `internal/highlight/dockerfile`
  - Dockerfile tokenization using BuildKit AST, typed instructions, and source scanning
- `internal/highlight/shell`
  - shell tokenization entrypoints and provider dispatch by `shell.Variant`
- `internal/highlight/extract`
  - shared script extraction and source mapping moved out of shellcheck-specific code
- `internal/highlight/renderansi`
  - Lip Gloss based CLI rendering from normalized tokens
- `internal/highlight/lspencode`
  - LSP semantic token legend, encoding, and later delta support
- `internal/highlight/theme`
  - dark/light palette selection and style lookup

This layout matches the repo’s current style better than burying everything in `internal/reporter` or `internal/lspserver`.

---

## 7. Token Model

Use a shared internal token model with 0-based line/column positions.

```go
type TokenType string

type Token struct {
    Line      int
    StartCol  int
    EndCol    int
    Type      TokenType
    Modifiers uint32
}
```

Recommended token types for v1 are standard LSP-friendly names:

- `keyword`
- `comment`
- `string`
- `number`
- `operator`
- `variable`
- `parameter`
- `property`
- `function`

Recommended modifier usage for v1:

- `declaration`
- `readonly`
- `documentation`

The internal model can remain slightly richer than the public LSP legend if needed, but the public legend should stay small and standard unless there
is a strong reason to add custom token types.

### 7.1 Normalization invariants

Every consumer should receive tokens that satisfy these invariants:

1. Tokens are sorted by `(line, startCol, endCol, type)`.
2. Tokens never overlap after normalization.
3. Tokens are clipped to the actual source line width.
4. Zero-width or invalid spans are dropped.
5. Later, more specific tokens win over earlier, coarser tokens.

That last rule matters when Dockerfile-level tokens and embedded-shell tokens compete for the same source span.

---

## 8. Tokenization Strategy

### 8.1 Dockerfile tokenization

Use BuildKit AST plus deterministic source scanning to emit tokens for:

- parsing directives such as `# syntax=`, `# escape=`, `# check=`
- instruction keywords such as `FROM`, `RUN`, `COPY`, `ADD`, `ARG`, `ENV`
- operators and punctuation with semantic value, such as heredoc operators and JSON-form punctuation where useful
- flag names and values, including BuildKit-specific flags
- stage alias declarations and references
- variable interpolations where column positions can be determined reliably

Examples that must be covered explicitly:

- `RUN --mount=type=cache,target=/root/.cache ...`
- `COPY --from=builder --chmod=755 ...`
- `FROM alpine AS builder`
- heredoc introducers like `<<EOF` and `<<-EOF`

Important constraint: BuildKit gives strong line structure but not full token-level columns for everything. When column precision is missing, the
tokenizer should use source scanning anchored by AST line ownership rather than inventing a second parser.

### 8.2 Embedded shell tokenization

For shell-form instructions and heredoc bodies:

1. Reuse the extraction and source-mapping logic currently in `internal/rules/shellcheck/script.go`.
2. Move that logic into `internal/highlight/extract` or a similarly neutral package.
3. Resolve the effective dialect using:
   - stage shell state from `internal/semantic`
   - `SHELL` instructions
   - heredoc shebang override when present
4. If `shell.Variant.IsParseable()` is true, parse with `mvdan.cc/sh/v3/syntax`.
5. If the variant is `powershell`, use the parser-backed PowerShell tokenizer; if the variant is `cmd` or `unknown`, use lexical fallback.

This is the correct reuse boundary: the semantic highlighter and ShellCheck both need the same script extraction and source-to-snippet mapping, so
they should share one implementation.

### 8.3 Lexical fallback contract

Fallback mode must be conservative:

- no invented AST precision
- keep comments, strings, variables, and obvious operators when detectible
- do not emit dense, low-confidence tokens that create noisy highlighting

The goal is graceful degradation, not fake precision.

---

## 9. CLI Rendering

Refactor `internal/reporter/text.go` to remove Chroma-specific state and replace it with token-driven segment rendering.

### 9.1 Reporter changes

`TextOptions` should:

- keep `Color`
- keep `ShowSource`
- replace `ChromaStyle` with `Theme string` or equivalent `auto|dark|light`
- keep syntax highlighting enabled by default when color is enabled

### 9.2 Rendering behavior

For each displayed snippet:

1. Build a `SourceMap` from the file content.
2. Request normalized semantic tokens for the snippet’s lines.
3. Render text segments with Lip Gloss styles by token type.
4. Apply an exact violation overlay for `rules.Location` at render time.
5. Keep the existing line-number gutter and `>>>` marker as a secondary cue.

### 9.3 Exact span overlay

The overlay should be independent of the semantic token stream.

Recommended overlay behavior:

- if the violation has an exact column range, apply underline + bold + optional inverse to that range only
- if the violation is point-like, highlight the token containing the point when possible, otherwise fall back to the whole line marker
- if the violation is line-only, keep the current affected-line marker behavior

Diagnostics should not be encoded into semantic token types just to make the CLI renderer simpler.

---

## 10. LSP Semantic Tokens

Extend `internal/lspserver` to advertise and serve semantic tokens from the same shared engine.

### 10.1 Capability advertisement

Phase 1 should advertise:

- `semanticTokensProvider.legend`
- `semanticTokensProvider.range = true`
- `semanticTokensProvider.full = true`

Phase 2 can upgrade `full` to `{ delta: true }`.

### 10.2 Handlers

Add handlers for:

- `textDocument/semanticTokens/full`
- `textDocument/semanticTokens/range`

Both handlers should:

1. resolve content from the open document store when possible
2. fall back to disk for file-backed URIs when appropriate
3. parse using the existing Dockerfile pipeline
4. generate normalized tokens
5. encode via a shared LSP legend and encoder

### 10.3 Delta support

`textDocument/semanticTokens/full/delta` should be deferred to phase 2.

Reasoning:

- the implementation cost is not in the wire format but in stable cache identity and edit computation
- token normalization must be frozen first, or delta churn becomes noisy and fragile
- `full` and `range` provide most of the user-visible value immediately

When added later, cache keys should at minimum include:

- document URI
- document version
- semantic token legend version
- tokenizer version or result ID

If cache state is missing or incompatible, the server should fall back to a full response.

---

## 11. Theme Resolution

Only two palettes are needed in v1:

- `dark`
- `light`

Resolution order:

1. if color output is disabled, skip theme resolution entirely
2. if `TALLY_THEME=dark|light`, use that palette
3. if `TALLY_THEME=auto` or unset, use `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)`
4. if detection is unavailable or ambiguous, fall back to `dark`

This preserves deterministic behavior for tests and CI without introducing a broader theming system.

---

## 12. Implementation Plan

### Phase 1: shared engine + CLI

1. Create `internal/highlight/core`, `dockerfile`, `extract`, `shell`, `renderansi`, and `theme`.
2. Move shared script extraction logic out of `internal/rules/shellcheck/script.go`.
3. Replace Chroma usage in `internal/reporter/text.go`.
4. Add unit tests and reporter snapshot coverage.
5. Remove the Chroma dependency from `go.mod` and `go.sum`.

### Phase 2: LSP full + range

1. Add semantic token legend and encoder.
2. Advertise semantic token capability in `handleInitialize`.
3. Implement `full` and `range` handlers in `internal/lspserver`.
4. Add LSP black-box tests in `internal/lsptest`.

### Phase 3: LSP delta

1. Add per-document token cache.
2. Implement `textDocument/semanticTokens/full/delta`.
3. Benchmark churn behavior and fallback correctness.

### Phase 4: future dialect expansion

1. Add optional provider adapters for additional dialects.
2. Keep lexical fallback available even when an optional parser exists.

---

## 13. Testing Plan

### 13.1 Unit tests

- Dockerfile tokenizer
  - directives
  - stage alias declaration and reference
  - `RUN --mount`
  - `COPY/ADD` advanced flags
  - heredoc markers
- Shell tokenizer
  - bash, POSIX, mksh
  - heredoc shebang override
  - parser-backed PowerShell behavior
  - fallback behavior for `cmd`
- Normalizer
  - sorting
  - overlap resolution
  - clipping and invalid-span removal
- Theme resolver
  - no-color path
  - env override
  - auto-detect fallback
- LSP encoder
  - stable legend order
  - full encoding
  - phase-2 delta edit generation

### 13.2 Integration tests

- text reporter snapshots for representative Dockerfiles
- exact violation overlay snapshots
- no-color output remains readable and stable
- LSP initialize snapshot includes `semanticTokensProvider`
- `semanticTokens/full` for open document content
- `semanticTokens/range` for partial requests
- phase-2 delta hit/miss cases

### 13.3 Benchmark coverage

Add targeted benchmarks for:

- large Dockerfiles with many `RUN --mount` instructions
- large heredoc bodies
- repeated LSP full-token requests on unchanged content

The implementation does not need a premature optimization pass, but it should prove that replacing Chroma does not create obvious regressions on
realistic files.

---

## 14. Zed Strategy

The original draft assumed we could likely drop the bundled Dockerfile grammar because Dockerfile was built into Zed. That assumption is too strong.

As of the current Zed docs referenced below, Dockerfile support is provided by a Dockerfile extension rather than a native built-in language. That
changes the migration strategy.

### 14.1 What this means for v1

For the initial semantic-token rollout:

- keep `_integrations/zed-tally` grammar and language assets unchanged
- add semantic token support on the server side only
- validate Zed behavior with `semantic_tokens = "combined"` and `"full"`

This avoids coupling the highlighter work to editor packaging decisions.

### 14.2 Future de-bundling should be a separate decision

If we later want to stop bundling Dockerfile grammar assets, that should be evaluated separately and only after we answer these questions:

1. Can `tally` depend on Zed’s community Dockerfile extension in a stable way?
2. Does that preserve structural editor behavior, not just visual highlighting?
3. Does the user experience remain acceptable when semantic tokens are disabled?

Until those answers are clear, grammar de-bundling should remain out of scope for this document’s acceptance criteria.

---

## 15. PowerShell Follow-Up

The PowerShell follow-up has been implemented as an extension of the shared highlighting pipeline, not as a second highlighting architecture.

This design should remain compatible with the Windows-container direction in `design-docs/26-windows-container-support.md`.

What is already implemented:

- `internal/highlight/highlight.go`
  - central document analysis entrypoint used by both CLI and LSP
  - already resolves embedded shell snippets through `extract.Mapping` and `internal/shell.Variant`
- `internal/highlight/extract/script.go`
  - already extracts shell-form `RUN`, `CMD`, `ENTRYPOINT`, `HEALTHCHECK CMD-SHELL`, and heredoc bodies
  - already preserves Dockerfile-to-script line alignment for remapping tokens back into the parent file
- `internal/highlight/shell/tokenize.go`
  - current dispatch boundary for embedded shell tokenization
  - uses `mvdan` for parseable POSIX-like shells, a tree-sitter-backed tokenizer for `powershell`, and lexical fallback for `cmd` and `unknown`
- `internal/highlight/powershell/tokenize.go`
  - contains the isolated tree-sitter integration and AST-to-`core.Token` mapping for PowerShell
- `internal/highlight/core/token.go`
  - shared token model, normalization, overlap resolution, and range filtering
- `internal/highlight/renderansi` and `internal/highlight/lspencode`
  - already consume normalized `[]core.Token`; they should not need dialect-specific logic

### 15.1 What changed

The implementation uses the seam this document proposed:

1. `internal/highlight/powershell` owns the grammar binding and PowerShell AST-to-`core.Token` mapping.
2. `internal/highlight/shell/tokenize.go` dispatches `VariantPowerShell` to that package.
3. `VariantCmd` and `VariantUnknown` remain on lexical fallback.
4. `internal/highlight/highlight.go`, `renderansi`, and `lspencode` remain dialect-agnostic.

### 15.2 Dependency and packaging guidance

- keep it pure Go from the repo's point of view; do not introduce cgo
- vendor or pin the grammar in a way that is deterministic in CI
- keep the grammar-specific code isolated under the new PowerShell highlighter package so the rest of the repo stays unaware of tree-sitter details

The rest of the highlight stack should continue to depend only on:

- `core.Token`
- `sourcemap.SourceMap`
- `extract.Mapping`
- `shell.Variant`

### 15.3 Concrete integration points

The implemented call flow is:

1. `highlight.Analyze(...)` builds a Dockerfile `SourceMap` and semantic stage model.
2. `shellTokens(...)` in `internal/highlight/highlight.go` determines the effective shell for each instruction.
3. `extract.ExtractRunScript(...)` or a sibling extractor produces an `extract.Mapping`.
4. `remapShellTokens(...)` calls `highlightshell.Tokenize(mapping.Script, variant)`.
5. `highlightshell.Tokenize(...)` dispatches:
   - POSIX-like shells -> existing `mvdan` path
   - PowerShell -> tree-sitter tokenizer
   - `cmd`/unknown -> existing lexical fallback
6. `core.Normalize(...)` resolves overlaps and clips tokens.
7. CLI and LSP consume the resulting normalized tokens unchanged.

That means the PowerShell implementation only needs to solve two problems well:

1. generate accurate single-line `core.Token` spans for the extracted script text
2. preserve the current remapping contract so token lines still line up with the Dockerfile after `OriginStartLine` adjustment

### 15.4 Token and position invariants

The current highlight pipeline assumes all emitted tokens satisfy these rules:

1. Columns are rune-based, not byte-based.
2. Lines are 0-based within the extracted script before remapping.
3. Each token is single-line.
4. `EndCol` is exclusive.
5. Invalid or zero-width spans should be dropped before returning.

These invariants matter because:

- `core.Normalize(...)` is line-oriented
- `renderansi.RenderLine(...)` slices by rune columns
- `lspencode.Encode(...)` emits LSP deltas from rune-based columns

Tree-sitter node positions are often byte-based. Convert them to rune columns against the exact source line before emitting tokens. Do not mix byte
and rune units in the same token.

If a tree-sitter node spans multiple lines, split it into one token per line or skip it. Do not emit multiline tokens into `core.Normalize(...)`.

### 15.5 Scope of the PowerShell tokenizer

The first parser-backed PowerShell pass does not need to model every grammar node. It should focus on visibly valuable and stable categories:

- keywords
- comments
- strings
- numbers
- operators
- variables
- function/command names
- property/member access when the grammar makes it reliable

Do not invent dense token output for ambiguous grammar regions just because tree-sitter exposes many node kinds. Preserve the same conservative bias
used by the current lexical fallback.

### 15.6 Dockerfile-specific cases to preserve

The new PowerShell path must continue to respect existing Dockerfile extraction rules:

- `SHELL ["powershell", "-Command"]` and Windows full-path shells must resolve to `VariantPowerShell` through `internal/shell`
- heredoc body extraction must still work through `extract.heredocBodyScriptMode(...)`
- heredoc shebang override should continue to win when a heredoc explicitly targets another shell
- do not assume POSIX-style `RUN <<EOF command ...` stdin-payload forms exist for PowerShell; empirical Docker testing with
  `SHELL ["pwsh", "-Command", ...]` showed that bare `RUN <<EOF ... EOF` script bodies work, but `RUN <<EOF cat > file`, `RUN <<EOF /bin/cat > file`,
  and `RUN <<EOF $input | ...` are rejected by PowerShell with `The '<' operator is reserved for future use`
- therefore, if future extraction logic ever handles stdin-payload heredocs generically, keep those cases distinct from PowerShell script-body
  heredocs

In other words: the tokenizer upgrade only activates after extraction has already decided "this snippet is PowerShell".

### 15.7 Test coverage

Coverage should continue to include:

- `internal/highlight/shell/tokenize_test.go`
  - dispatch tests proving `VariantPowerShell` uses the parser-backed path
- `internal/highlight/powershell/*_test.go`
  - token-level tests for strings, variables, commands, comments, numbers, and member/property access
- `internal/lsptest/lsp_test.go`
  - at least one PowerShell-backed `semanticTokens/full` or `range` black-box case
- reporter snapshot or targeted reporter tests
  - only when a PowerShell fixture materially changes visible snippet rendering

### 15.8 What should not change

Unless the implementation reveals a real gap, avoid changing:

- `core.Token` type names
- LSP legend ordering in `internal/highlight/lspencode/encode.go`
- ANSI renderer overlay behavior
- semantic token cache behavior in `internal/lspserver`
- ShellCheck extraction semantics

PowerShell support should look like a tokenizer upgrade, not a second semantic-highlighting architecture.

### 15.9 Definition of done for the PowerShell follow-up

This follow-up is complete when all of these remain true:

1. `VariantPowerShell` no longer falls back to the generic lexical tokenizer.
2. PowerShell tokens are emitted through the same `highlight.Analyze(...)` pipeline used by CLI and LSP.
3. Token positions remain rune-based and normalized correctly.
4. Existing Windows-container fixtures render more usefully in CLI snippets.
5. LSP semantic tokens for PowerShell-backed Dockerfile snippets become more specific without changing diagnostics behavior.
6. If the parser fails or is unavailable, the code still degrades cleanly to the current lexical fallback behavior.

---

## 16. Risks and Mitigations

1. Column precision can be inconsistent across BuildKit-derived data.
   Mitigation: use AST-anchored source scanning and add fixtures for tricky spans.

2. Sharing extraction logic could create package-cycle pressure.
   Mitigation: move script extraction into a neutral package under `internal/highlight` or another cycle-safe location.

3. Embedded-shell tokens can overlap coarse Dockerfile tokens.
   Mitigation: define explicit normalization precedence and test it directly.

4. Large heredocs can be expensive to retokenize repeatedly.
   Mitigation: support range-limited tokenization where possible and add phase-2 LSP caching.

5. Zed semantics may look different in `combined` versus `full` mode.
   Mitigation: document recommended settings, but do not tie server rollout to extension repackaging.

6. PowerShell parser support adds build complexity.
   Mitigation: isolate dialect providers behind the shared shell tokenization boundary and preserve lexical fallback.

---

## 17. Acceptance Criteria

### Required for initial delivery

1. Chroma imports are removed from the reporter implementation and dependency graph.
2. CLI snippets use the shared semantic token engine.
3. CLI rendering highlights exact violation spans when column information exists.
4. Heredoc and shell-form snippets use shell-aware tokenization when the dialect is parseable.
5. LSP advertises and serves `semanticTokens/full` and `semanticTokens/range`.
6. Existing diagnostics, formatting, and code actions remain unchanged.
7. No-color output remains plain and readable.

### Deferred to follow-up work

1. `semanticTokens/full/delta`
2. Zed grammar de-bundling

---

## 18. References

- BuildKit parser package:
  - <https://pkg.go.dev/github.com/moby/buildkit/frontend/dockerfile/parser>
- `mvdan.cc/sh/v3/syntax`:
  - <https://pkg.go.dev/mvdan.cc/sh/v3/syntax>
- LSP semantic tokens spec:
  - <https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/>
- Zed semantic tokens docs:
  - <https://zed.dev/docs/semantic-tokens>
- Zed Docker language docs:
  - <https://zed.dev/docs/languages/docker>
- Zed Dockerfile extension page:
  - <https://zed.dev/extensions/dockerfile>
