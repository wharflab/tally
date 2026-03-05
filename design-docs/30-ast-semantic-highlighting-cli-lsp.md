# AST-Based Semantic Highlighting for CLI Snippets and LSP

> Status: proposal
>
> Scope: drop Chroma, keep Lip Gloss, add shared semantic token engine for CLI + LSP

## 1. Context and Motivation

`tally` currently uses `github.com/alecthomas/chroma/v2` for snippet highlighting in text output.
That is generic lexer-based highlighting, but `tally` already has richer domain context:

- Dockerfile AST from BuildKit parser and typed instructions
- semantic stage/shell model (`internal/semantic`)
- shell parsing with `mvdan.cc/sh/v3/syntax`

Key limitations of current approach:

1. Dockerfile grammar in Chroma is generic and misses modern/highly specialized syntax (`RUN --mount`, rich `ADD/COPY` flags, parsing directives).
2. Heredoc script bodies are not highlighted with effective shell dialect awareness.
3. Shell-form instructions (`RUN`, `CMD`, `ENTRYPOINT`, `HEALTHCHECK CMD-SHELL`) are not highlighted as shell semantics.
4. Chroma adds broad functionality we do not need (many lexers and formatters), while we need deep Dockerfile/shell specialization.
5. The existing AST model already contains enough information to produce better highlighting than tokenizing plain lines.

This proposal replaces Chroma with an AST-aware semantic token pipeline that powers both terminal snippets and LSP semantic tokens.

---

## 2. Goals

1. Remove direct dependency on `github.com/alecthomas/chroma/v2`.
2. Keep terminal rendering with `charm.land/lipgloss/v2`.
3. Implement semantic tokenization from existing Dockerfile + shell AST/model.
4. Support modern Dockerfile syntax including mounts, directive comments, and advanced flags.
5. Support shell-aware highlighting inside heredocs and shell-form instructions, including shebang overrides.
6. Improve console focus by emphasizing the exact violation span (not only line-level markers).
7. Expose shared tokenization to LSP via:
   - `textDocument/semanticTokens/full`
   - `textDocument/semanticTokens/range`
   - `textDocument/semanticTokens/full/delta`

---

## 3. Non-Goals (v1)

1. Full PowerShell/CMD AST parser integration.
2. New user-facing theme system beyond dark/light.
3. Rewriting diagnostics model or rule engine.

---

## 4. High-Level Design

Introduce a new package family under `internal/highlight/`.

### 4.1 Core model

```go
type TokenKind string

type Token struct {
    Line     int // 0-based
    StartCol int // 0-based, inclusive
    EndCol   int // 0-based, exclusive
    Kind     TokenKind
    ModMask  uint32
}

type DocumentTokens struct {
    Tokens []Token
}
```

Token kinds for v1:

- `keyword`
- `comment`
- `string`
- `number`
- `operator`
- `variable`
- `parameter`
- `property`
- `function`

### 4.2 Subpackages

- `internal/highlight/core` — shared types, range helpers, sort/merge guarantees
- `internal/highlight/dockerfile` — Dockerfile instruction/directive tokenization
- `internal/highlight/shellposix` — mvdan-based shell tokenization
- `internal/highlight/extract` — shared script extraction/mapping (migrated from shellcheck rule internals)
- `internal/highlight/renderansi` — ANSI line rendering via Lip Gloss
- `internal/highlight/lspencode` — semantic token encoding + delta edit generation
- `internal/highlight/theme` — dark/light palette and resolution

---

## 5. Tokenization Strategy

## 5.1 Dockerfile tokens

Use BuildKit AST (`parser.Node`) + typed instructions + source lines to emit tokens for:

- parsing directives (`# syntax=`, `# escape=`, `# check=`)
- instruction keywords (`FROM`, `RUN`, `COPY`, `ADD`, etc.)
- flag names and values:
  - `RUN --mount`, `--network`, `--security`
  - `COPY/ADD --from`, `--chown`, `--chmod`, `--link`, `--exclude`, `--checksum`, `--keep-git-dir`
- stage alias binding (`FROM ... AS name`) and usage (`--from=name`)
- heredoc operators and delimiters (`<<`, `<<-`, marker names)

When BuildKit lacks fine-grained columns, use deterministic per-line scanners over source text.

## 5.2 Embedded shell tokens

For shell-form instructions and heredoc script bodies:

1. Reuse extraction/mapping logic currently implemented for ShellCheck ranges (move to shared `internal/highlight/extract`).
2. Resolve effective shell dialect per instruction:
   - stage shell setting from semantic model
   - updates from `SHELL` instructions
   - heredoc shebang override (highest precedence for heredoc body)
3. If parseable (`bash`, `posix`, `mksh`): parse with `mvdan.cc/sh/v3/syntax` and emit semantic tokens from AST nodes.
4. If non-parseable (`powershell`, `cmd`, unknown): apply conservative lexical fallback (no false AST precision).

This ensures large heredocs can be expanded and highlighted correctly for shell language context.

---

## 6. CLI Rendering Changes

Refactor `internal/reporter/text.go`:

1. Remove Chroma lexer/formatter/style fields and initialization.
2. For each displayed snippet range:
   - request semantic tokens from shared highlighter
   - render line segments with Lip Gloss styles by token kind
3. Keep existing structural output (headers, separators, line numbers, `>>>`).
4. Add **exact violation overlay**:
   - apply stronger style (inverse + underline + bold) for precise `rules.Location` span
   - keep line marker as secondary cue

This provides both semantic coloring and precise issue targeting.

---

## 7. Theme Resolution (Dark/Light only)

Only two palettes are supported in v1:

- `dark`
- `light`

Resolution order:

1. If color output is disabled (`NO_COLOR` / `--no-color`), skip theme selection.
2. Optional deterministic override via `TALLY_THEME`:
   - `dark` => dark palette
   - `light` => light palette
   - `auto` or unset => continue
3. Auto-detect with `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)`:
   - true => dark
   - false => light
4. If detection is unavailable/ambiguous, fallback to **dark**.

No additional theme variants are introduced.

---

## 8. LSP Semantic Tokens

Extend `internal/lspserver` capabilities and handlers.

### 8.1 Capabilities

Advertise `semanticTokensProvider` with:

- `legend` (v1 token kinds above)
- `range: true`
- `full: { delta: true }`

### 8.2 Handlers

Implement:

- `textDocument/semanticTokens/full`
- `textDocument/semanticTokens/range`
- `textDocument/semanticTokens/full/delta`

All handlers call the same shared highlighter used by CLI.

### 8.3 Delta behavior

Maintain per-document semantic token cache:

- key: URI + resultId + document version
- value: encoded token array + decoded token list metadata

On delta request:

1. If previous `resultId` cache exists and document version is compatible, return `SemanticTokensDelta` edits.
2. If cache miss/stale/incompatible, return full tokens (spec-compliant fallback).

This keeps behavior robust while still providing efficient updates for supported clients.

---

## 9. Public API / Config Changes

`internal/reporter.TextOptions`:

- remove Chroma-specific fields
- add minimal theme mode (`auto|dark|light`)

No mandatory CLI flag changes are required.

Optional environment override for deterministic behavior:

- `TALLY_THEME=auto|dark|light`

---

## 10. Dependency Changes

1. Remove direct dependency on `github.com/alecthomas/chroma/v2` from `go.mod`/`go.sum`.
2. Keep `charm.land/lipgloss/v2` as rendering backend.

---

## 11. Implementation Plan (Single Release)

1. Create `internal/highlight/*` core, tokenizers, renderer, LSP encoder.
2. Extract shared shell snippet mapping helpers from rule-internal code into neutral shared package.
3. Replace snippet highlighter in `internal/reporter/text.go` with new renderer.
4. Add LSP semantic token capability and handlers (`full`, `range`, `full/delta`).
5. Add semantic token cache and delta edit generation.
6. Remove Chroma dependency and dead code paths.
7. Update `_integrations/zed-tally` implementation for semantic-token-first flow:
   - remove bundled grammar declaration from `_integrations/zed-tally/extension.toml`
   - remove extension-owned Dockerfile grammar/query assets under `_integrations/zed-tally/grammars/dockerfile/**` and
     `_integrations/zed-tally/languages/dockerfile/**`
   - keep LSP registration targeting built-in `Dockerfile` language
8. Add shell tokenizer provider abstraction for future non-POSIX dialects:
   - explicit variant registry (`bash`, `posix`, `mksh`, `powershell`, `cmd`)
   - keep `powershell`/`cmd` on lexical fallback in v1
   - define PowerShell AST adapter contract for phase-2 tree-sitter integration
9. Update tests/snapshots/docs (including Zed extension docs, PowerShell strategy, and migration notes).

---

## 12. Testing Plan

## 12.1 Unit tests

- Dockerfile tokenizer:
  - directive comments
  - `RUN --mount=...`
  - `ADD/COPY` advanced flags
  - heredoc boundaries
- Shell tokenizer:
  - bash/posix/mksh AST cases
  - heredoc shebang override
  - fallback mode for powershell/cmd
- Theme resolver:
  - no-color path
  - `TALLY_THEME` override
  - auto detect path
  - fallback behavior
- LSP encoder:
  - stable ordering
  - full encoding
  - delta edit generation

## 12.2 Integration tests

- Reporter snapshots for representative Dockerfiles:
  - regular RUN
  - shell-form instructions
  - heredoc scripts
  - COPY/ADD flags
  - precise violation overlay
- LSP tests:
  - initialize capability includes semanticTokensProvider
  - `semanticTokens/full` response
  - `semanticTokens/range` response
  - `semanticTokens/full/delta` hit/miss behavior

## 12.3 Regression tests

- No-color output remains plain and readable.
- Existing diagnostics/code actions/formatting remain unchanged.

---

## 13. Windows/PowerShell Future Compatibility

This design is intentionally compatible with planned Windows container support (`design-docs/26-windows-container-support.md`):

- Token engine supports variant-specific pipelines.
- Non-POSIX dialects can start with lexical fallback.
- Later PowerShell/CMD AST support can be added behind same token model without breaking renderer/LSP contracts.

### 13.1 PowerShell semantic-token requirements

Any parser strategy used for future PowerShell support should satisfy:

1. Stable start/end ranges at token-level precision for `RUN` shell form and heredoc-expanded scripts.
2. Deterministic, machine-readable syntax categories mappable to LSP semantic token kinds/modifiers.
3. Practical performance for `semanticTokens/full`, `semanticTokens/range`, and `semanticTokens/full/delta`.
4. Predictable behavior in CI and local development (minimal hidden runtime assumptions).
5. Graceful fallback when parser is unavailable.

### 13.2 Option A — Tree-sitter (`go-tree-sitter` + `tree-sitter-powershell`)

Pros (for semantic tokens):

- In-process parser with precise node ranges suitable for token spans.
- Naturally supports range-oriented workflows and can be reused for both CLI snippets and LSP handlers.
- Grammar is actively maintained and purpose-built for modern PowerShell syntax.
- Keeps tokenization architecture consistent with future multi-language embedding scenarios.

Tradeoffs:

- Adds C-backed/cgo dependency surface and associated toolchain/runtime complexity.
- Requires careful lifecycle management of parser/tree/query objects.
- Increases binary/build complexity versus pure-Go fallback.

### 13.3 Option B — `go-powershell` + native parser scripting (Codex-style)

Assessment from semantic-token perspective:

- `github.com/KnicKnic/go-powershell` is primarily a PowerShell execution/hosting library, not a static AST parser API for source tokenization.
- Codex-style parsing via `[System.Management.Automation.Language.Parser]::ParseInput(...)` is viable as a native parser access pattern, but requires
  PowerShell runtime orchestration and JSON bridge logic.

Pros:

- Uses official PowerShell parser semantics.
- Can leverage native parser behavior for tricky language constructs.

Tradeoffs:

- Runtime/process dependency (`pwsh`/Windows PowerShell availability) complicates portability and CI predictability.
- Higher request overhead and operational complexity for LSP token refresh paths.
- Less practical for low-latency, always-on semantic tokenization than an in-process parser.

### 13.4 Recommendation

Choose **Option A (Tree-sitter)** for phase-2 PowerShell semantic tokens.

Rationale:

1. It best fits LSP semantic token serving model (full/range/delta) with in-process, low-latency token extraction.
2. It preserves cross-feature code reuse with the CLI renderer and shared token pipeline.
3. It provides parser independence from external PowerShell runtime availability.

`go-powershell`/native-parser scripting can remain a contingency path for specialized validation workflows, but not as primary semantic token source.

### 13.5 Integration guidelines for phase 2

1. Implement `internal/highlight/shellpowershell` behind shared shell tokenizer interface.
2. Keep tokenizer registry dialect-driven (`shell.Variant*`) with strict fallback contract:
   - parser available => AST semantic tokens
   - parser unavailable => lexical fallback
3. Map PowerShell node types to existing token legend first (`keyword`, `variable`, `string`, `operator`, `function`, `parameter`, `property`), then
   extend legend only if required.
4. Reuse existing script extraction/range expansion logic for Dockerfile heredocs and shell-form instructions so PowerShell path follows same
   snippet-to-source mapping contracts.
5. Add parser capability probe at startup and expose internal diagnostics/trace for fallback decisions.
6. Add focused fixtures for:
   - interpolation and expandable strings
   - pipelines and script blocks
   - here-strings
   - parameter binding and splatting
7. Keep parser implementation isolated so future parser swap is possible without changing renderer or LSP handler contracts.

---

## 14. Zed Extension Strategy (Can We Drop Bundled Tree-Sitter Dockerfile Grammar?)

Short answer: **yes, we can likely drop the bundled grammar in `_integrations/zed-tally` once LSP semantic tokens are in place**, with caveats.

Important distinction:

- We can drop the **extension-bundled** Dockerfile grammar.
- We cannot drop Tree-sitter usage in Zed entirely; Zed still relies on Tree-sitter language support for structure-oriented editor behaviors.

### 14.1 Research findings

1. Zed supports semantic tokens with three modes:
   - `off` (default)
   - `combined`
   - `full` (semantic tokens replace Tree-sitter highlighting layer)
2. Zed now supports LSP semantic token highlighting (preview release `0.224.0`, Feb 11, 2026).
3. Dockerfile is listed as a built-in supported language in Zed, so we can target the built-in `Dockerfile` language instead of shipping our own
   grammar copy.
4. Zed language extension docs still define language metadata around grammar-backed language config; therefore, de-bundling should rely on
   **existing built-in Dockerfile language** rather than a grammar-less custom language definition.

### 14.2 Proposed migration

#### Phase A — Semantic-token-ready server (this project)

Ship the LSP semantic token implementation (`full`, `range`, `full/delta`) and validate with Zed `semantic_tokens = "full"` and `combined`.

#### Phase B — De-bundle extension grammar

Update `_integrations/zed-tally`:

1. Remove extension-owned Dockerfile grammar declaration in `extension.toml`:
   - remove `[grammars.dockerfile]` block
2. Remove extension-owned language assets:
   - `languages/dockerfile/config.toml`
   - `languages/dockerfile/highlights.scm`
   - `languages/dockerfile/injections.scm`
   - `grammars/dockerfile/**`
3. Keep language server registration targeting built-in Dockerfile language:
   - `[language_servers.tally] languages = ["Dockerfile"]`
4. Update extension README to recommend semantic token mode:
   - per-language `semantic_tokens = "full"` (preferred) or `"combined"`

#### Phase C — Compatibility fallback and quality checks

1. Verify extension behavior with semantic tokens `off` (Tree-sitter fallback from Zed built-in Dockerfile support).
2. Verify behavior with semantic tokens `combined` and `full`.
3. If specific Dockerfile visual semantics regress in Zed (vs prior bundled grammar), provide user-facing token rule guidance in docs via
   `global_lsp_settings.semantic_token_rules`.

### 14.3 Rollback plan

If de-bundling reveals unacceptable UX regressions (for example, editor structural behavior differences tied to grammar queries), reintroduce bundled
grammar in extension while keeping semantic-token support unchanged in `tally` LSP.

### 14.4 Acceptance criteria for de-bundling

1. `zed-tally` works without shipping `grammars/dockerfile`.
2. Diagnostics, formatting, and code actions remain functional.
3. Syntax appearance in `semantic_tokens = "full"` is acceptable using `tally` semantic tokens.
4. `semantic_tokens = "off"` remains acceptable through Zed's built-in Dockerfile language fallback.

---

## 15. Risks and Mitigations

1. **Column precision mismatch**
   - Mitigation: source-based scanners + mapping fixtures and fuzz-style edge tests.
2. **Token cache complexity for delta**
   - Mitigation: strict cache keying by URI+version+resultId and full fallback.
3. **Performance on large heredocs**
   - Mitigation: range-limited tokenization for snippets and LSP range requests; cache full results.
4. **Package cycle risk during extraction refactor**
   - Mitigation: keep extraction helpers in neutral package under `internal/highlight/extract`.
5. **Zed semantic tokens are opt-in**
   - Mitigation: document recommended Zed settings and keep Tree-sitter fallback path.
6. **De-bundling extension grammar could change editor structural behavior**
   - Mitigation: phase-gated rollout with explicit rollback option.
7. **Future PowerShell tree-sitter integration adds cgo footprint**
   - Mitigation: isolate parser adapter, preserve lexical fallback, and gate with capability checks.

---

## 16. Acceptance Criteria

1. Chroma imports and dependency removed.
2. CLI snippets show semantic Dockerfile and shell highlighting with exact span overlay.
3. Heredoc shell bodies use effective shell-aware highlighting.
4. LSP returns semantic tokens for full/range and supports full/delta.
5. Theme defaults to reliable dark/light auto detection with deterministic override.
6. Existing lint diagnostics behavior remains stable.
7. Zed integration strategy for optional grammar de-bundling is documented and actionable.
8. PowerShell phase-2 parser strategy and recommendation are documented with integration guidelines.

---

## 17. References

- LSP Semantic Tokens Spec (3.17):
  - <https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/>
- Example semantic token architecture:
  - <https://github.com/vroland/lsp-syntax-highlighter>
- BuildKit parser package:
  - <https://pkg.go.dev/github.com/moby/buildkit/frontend/dockerfile/parser>
- mvdan shell syntax package:
  - <https://pkg.go.dev/mvdan.cc/sh/v3/syntax>
- go-tree-sitter:
  - <https://github.com/tree-sitter/go-tree-sitter>
- tree-sitter-powershell grammar:
  - <https://github.com/airbus-cert/tree-sitter-powershell>
- go-powershell:
  - <https://github.com/KnicKnic/go-powershell>
- Codex PowerShell parser script reference:
  - <https://github.com/openai/codex/blob/main/codex-rs/shell-command/src/command_safety/powershell_parser.ps1>
- Zed semantic tokens docs:
  - <https://zed.dev/docs/semantic-tokens>
  - <https://zed.dev/docs/extensions/languages>
  - <https://zed.dev/docs/configuring-languages>
- Zed preview release with semantic token support:
  - <https://zed.dev/releases/preview/0.224.0>
- Zed supported languages (Dockerfile built-in):
  - <https://zed.dev/languages>
