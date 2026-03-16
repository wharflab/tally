# Semantic Token Alignment With `better-dockerfile-syntax`

Research date: 2026-03-16
Status: design note / investigation

## Executive Summary

The key finding is that `jeff-hykin/better-dockerfile-syntax` is a TextMate grammar extension, not a semantic-token provider. Its value for Tally is
therefore not "copy its semantic tokens" but:

1. Learn which Dockerfile and embedded-shell scopes good themes already recognize.
2. Make sure Tally's semantic token boundaries do not erase those useful scopes.
3. Make sure Tally's VS Code `semanticTokenScopes` fallback map points to the same scope vocabulary that the grammar actually emits.

The current degradation is real and explainable:

- Tally emits a single `comment.documentation` token for every top-level `# ...` line.
- VS Code applies semantic highlighting on top of TextMate highlighting.
- Once Tally covers `# escape=\`` with one semantic comment token, no `semanticTokenScopes` mapping can recover inner structure for `escape`,`=`, or
  the backtick value. The underlying grammar is already covered up.

`better-dockerfile-syntax` does help us in other areas:

- instruction keyword scope naming
- `FROM` image / version / digest / stage alias scopes
- `RUN` embedded shell scopes
- `ENV` assignment scopes
- heredoc operator / delimiter scopes

But it does **not** appear to special-case Dockerfile parser directives such as `# syntax=`, `# escape=`, or `# check=`. That means parser
directive highlighting needs to be a Tally-owned semantic feature, not something we can inherit from that extension.

The recommended path is:

1. Fix the semantic boundaries first: stop tokenizing recognized directive lines as one giant comment.
2. Fix the VS Code fallback mapping second: align Tally's `semanticTokenScopes` with the real scopes emitted by `better-dockerfile-syntax`.
3. Only then consider custom Dockerfile-specific semantic token types for cases where standard LSP classes are too coarse.

---

## 1. What `better-dockerfile-syntax` Actually Provides

### 1.1 It is a grammar extension, not a semantic-token extension

In its `package.json`, the extension contributes a Dockerfile grammar at `syntaxes/dockerfile.tmLanguage.json` for language `dockerfile`. It does not
contribute:

- `semanticTokenTypes`
- `semanticTokenModifiers`
- `semanticTokenScopes`
- a semantic token provider implementation

So it improves coloring through TextMate scopes only.

### 1.2 What the extension claims to improve

Its Dockerfile README explicitly says it adds:

- shell command syntax
- variables
- image name / version / digest highlighting

That matches the grammar structure in `source/languages/dockerfile/generate.rb` and `syntaxes/dockerfile.tmLanguage.yaml`.

### 1.3 The scopes it actually emits

The Dockerfile grammar uses scopes like:

- instruction keywords:
  - `keyword.other.special-method.dockerfile`
  - `keyword.other.special-method.from.dockerfile`
  - `keyword.other.special-method.as.dockerfile`
  - `keyword.control.onbuild.dockerfile`
- `FROM` image reference pieces:
  - `entity.name.image.dockerfile`
  - `constant.numeric.version.dockerfile`
  - `constant.constant.language.symbol.digest.dockerfile`
  - `entity.name.image.stage.dockerfile`
- comments:
  - `comment.line.number-sign.dockerfile`
- `ENV` assignments:
  - `variable.other.assignment.dockerfile`
  - `keyword.operator.assignment.dockerfile`
- embedded shell:
  - `support.function.builtin.shell.dockerfile`
  - `variable.other.normal.shell.dockerfile`
  - `keyword.operator.assignment.shell.dockerfile`
  - `keyword.operator.heredoc.shell.dockerfile`
  - `keyword.control.heredoc-token.shell.dockerfile`
  - `comment.line.number-sign.shell.dockerfile`

This is the scope vocabulary themes are likely already tuned for when that extension is installed.

### 1.4 Important limitation: no parser-directive grammar

The Dockerfile grammar appears to treat `# ...` lines generically:

- the `comments` rule is `^\s*+(#)(.*$)`
- the captures are only comment scopes

I found no Dockerfile rule that separately scopes:

- `# syntax=...`
- `# escape=...`
- `# check=...`

So if `# escape=\`` looked richer before Tally started sending semantic tokens, that richness likely came from:

- a different Dockerfile grammar
- a theme-specific rule
- or some interaction outside this extension

It does **not** appear to come from a parser-directive-specific rule in `better-dockerfile-syntax`.

---

## 2. What VS Code Does With Semantic Tokens

The relevant VS Code behavior is:

1. TextMate grammar tokenization happens first.
2. Semantic tokens are then applied on top.
3. If a theme does not define semantic token coloring rules for a token selector, VS Code falls back to a TextMate scope map.
4. Extensions can extend that fallback map through `contributes.semanticTokenScopes`.

This leads to two important constraints:

### 2.1 `semanticTokenScopes` only helps with color lookup, not token boundaries

If Tally emits a token covering columns `0..endOfLine` as `comment.documentation`, VS Code cannot recover substructure from the grammar inside that
range. A better scope map will still theme it as a comment.

This is why parser directives must be fixed at the tokenizer level, not only in `_integrations/vscode-tally/package.json`.

### 2.2 The fallback map needs to match the grammar's actual scopes

If Tally emits `keyword` but the fallback map points to `keyword.control.dockerfile`, while the installed grammar mostly emits
`keyword.other.special-method.*.dockerfile`, then language-specific theme rules are bypassed. The user gets a more generic color than they had before
semantic tokens were turned on.

That is exactly the kind of degradation we should avoid.

---

## 3. Tally's Current Semantic Token Model

### 3.1 Server legend

Today Tally exposes a small standard legend from `internal/highlight/core/token.go` and `internal/highlight/lspencode/encode.go`:

- token types:
  - `keyword`
  - `comment`
  - `string`
  - `number`
  - `operator`
  - `variable`
  - `parameter`
  - `property`
  - `function`
- modifiers:
  - `declaration`
  - `readonly`
  - `documentation`

This is reasonable as a baseline, but it is very coarse for Dockerfile-specific structure.

### 3.2 Current Dockerfile tokenization

The Dockerfile tokenizer in `internal/highlight/dockerfile/tokenize.go` currently emits tokens for:

- instruction keyword at line start
- `FROM ... AS alias`
- flags like `--mount`
- comma-separated `key=value` segments inside some flag values
- quoted strings
- variable expansions
- numbers
- heredoc operator + delimiter
- full-line top-level comments

The comment behavior is the critical issue:

```go
if strings.HasPrefix(trimmed, "#") {
    out = append(out, core.Token{
        Line:      i,
        StartCol:  start,
        EndCol:    len([]rune(line)),
        Type:      core.TokenComment,
        Modifiers: core.ModDocumentation,
        Priority:  40,
    })
}
```

This covers:

- regular comments
- parser directives (`# syntax=...`, `# escape=...`, `# check=...`)
- tally / hadolint inline directives

with the same full-line semantic comment token.

### 3.3 Current VS Code fallback mapping

`_integrations/vscode-tally/package.json` contributes `semanticTokenScopes`, but the map is only loosely related to the installed grammar:

- `keyword` -> `keyword.control.dockerfile`, `keyword.control`
- `variable` -> `variable.other.readwrite.dockerfile`, `variable.other.readwrite`, `variable`
- `function` -> `entity.name.function.dockerfile`, `entity.name.function`, `support.function`
- `operator` -> `keyword.operator.dockerfile`, `keyword.operator`
- `variable.declaration` -> `variable.other.constant.dockerfile`, `variable.other.constant`, `variable`

These are generic choices. Several do not line up with the scopes from `better-dockerfile-syntax`.

---

## 4. Concrete Mismatches Between Tally and `better-dockerfile-syntax`

### 4.1 Parser directives are flattened into one comment token

This is the highest-impact mismatch.

For `# escape=\``:

- `better-dockerfile-syntax` does not provide a special directive grammar
- Tally emits one full-line semantic comment token
- result: the line is guaranteed to look like a comment once semantic highlighting wins

This is the most direct explanation for the user-visible degradation.

### 4.2 Instruction keywords map to the wrong fallback scope family

`better-dockerfile-syntax` uses:

- `keyword.other.special-method.dockerfile`
- `keyword.other.special-method.from.dockerfile`
- `keyword.other.special-method.as.dockerfile`
- `keyword.control.onbuild.dockerfile`

Tally maps `keyword` to:

- `keyword.control.dockerfile`
- `keyword.control`

That is not the grammar's primary instruction-keyword vocabulary. Themes with Dockerfile-specific `keyword.other.special-method...` rules do not get
the same result once semantic highlighting is enabled.

### 4.3 Stage aliases are semantically `variable.declaration`, but visually behave more like `entity.name.image.stage`

Tally tokenizes `AS build` as:

- `AS` -> `keyword`
- `build` -> `variable.declaration`

`better-dockerfile-syntax` scopes the alias as:

- `entity.name.image.stage.dockerfile`

Tally's current fallback mapping for `variable.declaration` does not point there, so stage aliases lose the theme's stage-name styling.

### 4.4 Shell command names do not map to the shell grammar's preferred scope

In embedded shell, `better-dockerfile-syntax` uses:

- `support.function.builtin.shell.dockerfile`

Tally's current `function` fallback prefers:

- `entity.name.function.dockerfile`
- `entity.name.function`
- `support.function`

That misses the Dockerfile shell grammar's main command-name scope.

### 4.5 Variable fallback misses the grammar's real variable scopes

The grammar uses:

- `variable.other.dockerfile`
- `variable.other.normal.shell.dockerfile`
- `variable.other.assignment.dockerfile`

Tally currently maps `variable` to:

- `variable.other.readwrite.dockerfile`
- `variable.other.readwrite`
- `variable`

This is a generic mapping, not a grammar-accurate one.

### 4.6 Operator fallback misses assignment and heredoc operators

The grammar has useful operator scopes such as:

- `keyword.operator.assignment.dockerfile`
- `keyword.operator.assignment.shell.dockerfile`
- `keyword.operator.heredoc.shell.dockerfile`
- `keyword.operator.redirect.shell.dockerfile`
- `keyword.operator.pipe.shell.dockerfile`

Tally maps `operator` only to:

- `keyword.operator.dockerfile`
- `keyword.operator`

That throws away useful language-specific theming opportunities.

### 4.7 Better Syntax does not help with Dockerfile flags

I did not find grammar rules for BuildKit-style instruction flags such as:

- `--mount`
- `--from`
- `--chmod`
- `--chown`

Tally already has first-class semantic tokenization for many of these. This is an area where Tally should lead, not imitate the grammar.

### 4.8 Better Syntax does not help with PowerShell

The extension improves POSIX-shell-style Dockerfile bodies. Tally already goes beyond that by:

- detecting effective shell per stage
- using parser-backed PowerShell tokenization
- avoiding misclassifying path-like native executables as commands

This is important: we should learn scope vocabulary from the extension where useful, but we should **not** regress PowerShell handling toward a
TextMate-like POSIX approximation.

---

## 5. What We Should Learn From the Extension

### 5.1 Learn its scope vocabulary, not its parsing strategy

The extension's grammar is a useful catalog of scopes that themes already understand for Dockerfile and shell content. Tally should reuse that
vocabulary in `semanticTokenScopes`.

### 5.2 Preserve grammar detail by avoiding coarse semantic spans

The extension highlights well where it creates narrow scopes:

- `FROM`
- image name / version / digest
- `AS`
- stage alias
- shell builtins / variables / heredoc pieces
- ENV assignment names / operators

Tally should follow the same principle: emit smaller semantic spans for structured syntax, not line-wide catch-all tokens.

### 5.3 Accept that parser directives are our job

The grammar does not meaningfully help with:

- `# syntax=...`
- `# escape=...`
- `# check=...`
- tally / hadolint inline directives

Tally already has the raw ingredients to do this properly:

- `internal/sourcemap` directive detection
- `internal/directive` parsing for tally / hadolint / buildx directives
- `parser.DetectSyntax` and `internal/syntax` logic for `# syntax=...`
- escape-token awareness already used elsewhere

We should use those instead of coloring parser directives as ordinary comments.

---

## 6. Recommended Design Changes

## 6.1 Phase 1: fix the degradation with minimal risk

This phase is the highest value and should be implemented before any custom token taxonomy work.

### A. Stop emitting full-line semantic comments for recognized directive lines

For comment lines that are recognized as directives, do **not** emit:

- one full-line `comment.documentation` token

Instead, emit structured spans. Suggested initial scheme:

- leading `#`:
  - no semantic token, or `operator`
- directive keyword (`syntax`, `escape`, `check`, `tally`, `hadolint`, `shell`, `ignore`, `global`, `skip`):
  - `keyword`
- `=` and separators:
  - `operator`
- directive values:
  - `string`, `number`, `property`, or `variable` as appropriate

Example targets:

`# escape=\``

- `escape` -> `keyword`
- `=` -> `operator`
- `` ` `` -> `string` or a Dockerfile-specific subtype later

`# syntax=docker/dockerfile:1.10`

- `syntax` -> `keyword`
- `=` -> `operator`
- `docker/dockerfile` -> ideally image-like or string-like token
- `:1.10` -> number/string split if easy, otherwise one value token

`# check=skip=DL3006,DL3008`

- `check` -> `keyword`
- first `=` -> `operator`
- `skip` -> `keyword`
- second `=` -> `operator`
- rule IDs -> `property` or `string`

`# tally global ignore=DL3006`

- `tally`, `global`, `ignore` -> `keyword`
- `=` -> `operator`
- `DL3006` -> `property` or `string`

This change alone addresses the `# escape=\`` complaint.

### B. Align `_integrations/vscode-tally/package.json` with the grammar's real scopes

The fallback map should prefer the scopes from `better-dockerfile-syntax`, for example:

- `keyword`
  - `keyword.other.special-method.from.dockerfile`
  - `keyword.other.special-method.as.dockerfile`
  - `keyword.other.special-method.dockerfile`
  - `keyword.control.onbuild.dockerfile`
  - `keyword.control.shell.dockerfile`
  - `keyword.control.heredoc-token.shell.dockerfile`
  - `keyword.control`
- `comment`
  - `comment.line.number-sign.dockerfile`
  - `comment.line.number-sign.shell.dockerfile`
  - `comment.line.number-sign`
  - `comment`
- `comment.documentation`
  - same as `comment` unless we later introduce a true directive subtype
- `string`
  - `string.quoted.double.dockerfile`
  - `string.quoted.single.dockerfile`
  - `string.unquoted.dockerfile`
  - `string.unquoted.heredoc.shell.dockerfile`
  - `string`
- `number`
  - `constant.numeric.version.dockerfile`
  - `constant.numeric`
- `operator`
  - `keyword.operator.assignment.dockerfile`
  - `keyword.operator.assignment.shell.dockerfile`
  - `keyword.operator.heredoc.shell.dockerfile`
  - `keyword.operator.redirect.shell.dockerfile`
  - `keyword.operator.pipe.shell.dockerfile`
  - `keyword.operator.logical.shell.dockerfile`
  - `keyword.operator`
- `variable`
  - `variable.other.dockerfile`
  - `variable.other.normal.shell.dockerfile`
  - `variable.other.assignment.dockerfile`
  - `variable`
- `variable.declaration`
  - `entity.name.image.stage.dockerfile`
  - `variable.other.assignment.dockerfile`
  - `variable`
- `parameter`
  - keep generic unless we add a better Dockerfile-specific subtype later
- `property`
  - `variable.other.property`
  - `variable.other.property.dockerfile`
- `function`
  - `support.function.builtin.shell.dockerfile`
  - `support.function`
  - `entity.name.function`

This does not create new semantics, but it stops throwing away the grammar's existing scope language.

### C. Add explicit tests for directive-token behavior

Add tokenization tests covering at least:

- `# escape=\``
- `# syntax=docker/dockerfile:1.10`
- `# check=skip=DL3006,DL3008`
- `# tally global ignore=DL3006`

The tests should assert:

- recognized directives are **not** emitted as one full-line comment token
- directive keyword / operator / value spans exist
- plain comments still remain comment tokens

## 6.2 Phase 2: add Dockerfile-specific token types where standard LSP types are too weak

After Phase 1, we can decide whether standard token types are still too lossy for theming.

Good candidates for custom subtypes:

- `dockerInstruction` extends `keyword`
- `dockerDirective` extends `keyword`
- `dockerImage` extends `string` or `type`
- `dockerStage` extends `variable`
- `dockerFlag` extends `parameter`
- `dockerHeredocDelimiter` extends `keyword`

Why this may be worth it:

- stage names are not really ordinary variables
- image references are not really ordinary strings
- parser directives are not really ordinary comments or keywords

If we do this, we must update both:

- the LSP legend emitted by the server
- the VS Code extension contributions:
  - `semanticTokenTypes`
  - `semanticTokenScopes`

The custom scopes should map to the grammar's best equivalents, for example:

- `dockerInstruction` -> `keyword.other.special-method.dockerfile`
- `dockerStage` -> `entity.name.image.stage.dockerfile`
- `dockerImage` -> `entity.name.image.dockerfile`
- `dockerHeredocDelimiter` -> `keyword.control.heredoc-token.shell.dockerfile`

## 6.3 Phase 3: expand token precision for Dockerfile-native structure

This is not required to solve the current regression, but it is a natural next step.

Possible improvements:

- tokenize `FROM` image reference parts directly:
  - repository
  - tag
  - digest
- tokenize `ENV` assignment names and `=`
- tokenize `ARG NAME=value` similarly
- tokenize `COPY --from=builder` value as stage reference when appropriate
- tokenize `ONBUILD RUN` so both `ONBUILD` and the trigger instruction get semantic treatment

This is where Tally can exceed the grammar, especially because it already has AST and semantic model data.

---

## 7. Implementation Sketch

### 7.1 Likely file touch points

- `internal/highlight/dockerfile/tokenize.go`
  - add directive-aware tokenization
  - skip full-line comment token for recognized directives
- `internal/highlight/dockerfile/tokenize_test.go`
  - add parser-directive coverage
- `internal/highlight/core/token.go`
  - only if adding custom token types
- `internal/highlight/lspencode/encode.go`
  - only if legend changes
- `_integrations/vscode-tally/package.json`
  - update `semanticTokenScopes`
  - add `semanticTokenTypes` later if custom subtypes are introduced
- `internal/lsptest/lsp_test.go`
  - assert semantic tokens on directive lines if we want end-to-end coverage

### 7.2 Reuse opportunities inside Tally

We do not need to invent a new parser stack for directives.

Reusable pieces already exist:

- `internal/sourcemap.SourceMap.Comments()`
  - identifies directive-looking comment lines
- `internal/directive.Parse(...)`
  - parses tally / hadolint / buildx directives
- `parser.DetectSyntax(source)`
  - extracts syntax directive value
- existing escape-token handling
  - already used across highlighting and parsing

The main work is span selection, not directive recognition.

---

## 8. Validation Plan

Use VS Code's scope inspector and real-theme manual checks against a document containing:

1. Parser directives

```dockerfile
# syntax=docker/dockerfile:1.10
# escape=`
# check=skip=DL3006,DL3008
# tally global ignore=max-lines
```

2. Image and stage syntax

```dockerfile
FROM ghcr.io/org/app:1.2@sha256:deadbeef AS build
COPY --from=build /out /app
```

3. POSIX shell

```dockerfile
RUN --mount=type=cache,target=/tmp echo "$HOME" <<EOF
hello
EOF
```

4. Windows / PowerShell

```dockerfile
# escape=`
SHELL ["powershell", "-Command"]
RUN Invoke-WebRequest "https://example.com/app.tar.gz" -OutFile "$HOME/app.tar.gz"
```

Expected outcomes:

- `# escape=\`` no longer collapses into a single semantic comment span
- instruction keywords still pick up theme styling close to the grammar-only experience
- stage aliases can be themed like stage names, not generic variables
- shell builtins / command names keep sensible coloring
- PowerShell still benefits from Tally's parser-backed semantics

---

## 9. Recommended Decision

Implement Phase 1 now:

1. directive-aware semantic tokenization for comment directives
2. grammar-aligned `semanticTokenScopes`
3. focused tests around parser directives and fallback-sensitive Dockerfile syntax

Do **not** spend time trying to reverse-engineer parser-directive styling from `better-dockerfile-syntax`, because the extension does not appear to
model parser directives specially. The right move is to preserve grammar benefits where they exist and let Tally provide first-class semantics where
the grammar is weak.

That gives us the best of both systems:

- TextMate-compatible scope vocabulary from the extension
- AST-aware Dockerfile and shell semantics from Tally
- no unnecessary degradation when semantic highlighting is enabled

---

## Sources

Internal repo files reviewed:

- `internal/highlight/core/token.go`
- `internal/highlight/dockerfile/tokenize.go`
- `internal/highlight/highlight.go`
- `internal/highlight/lspencode/encode.go`
- `internal/lspserver/semantic_tokens.go`
- `internal/lspserver/server.go`
- `_integrations/vscode-tally/package.json`
- `internal/directive/parser.go`
- `internal/sourcemap/sourcemap.go`

External sources reviewed:

- `better-dockerfile-syntax` repository:
  - `package.json`
  - `source/languages/dockerfile/README.md`
  - `source/languages/dockerfile/generate.rb`
  - `syntaxes/dockerfile.tmLanguage.yaml`
- VS Code Semantic Highlight Guide:
  - <https://code.visualstudio.com/api/language-extensions/semantic-highlight-guide>
