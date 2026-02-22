# BuildKit-Parseable but Non-Buildable Dockerfiles (Heuristic Checks)

> Status: proposal

This document proposes a small set of **“preflight” correctness checks** for Dockerfiles/Containerfiles that **successfully parse into an AST**
with the BuildKit Dockerfile parser, but are **very likely to fail during build** (or represent a “half-edited” file that no longer has coherent
build semantics).

The focus is **not** on contrived edge cases. These checks target common “human typo” and “AI patch mis-edit” scenarios.

## Goals

- Catch **build-blocking** mistakes early with actionable messages.
- Prefer **static/AST-based** checks where possible (fast, deterministic).
- When a check is inherently heuristic, bias toward **high-signal warnings** with good suggestions.
- Avoid duplicating existing BuildKit/hadolint coverage unless it materially improves UX (e.g., better error messaging).

## Non-goals

- Guarantee that `docker build` will succeed (network, context files, credentials, platform availability).
- Validate that paths referenced in `COPY`/`ADD` exist (that’s build-context dependent).
- Re-implement all of BuildKit’s front-end/LLB conversion behavior.

## Why this category exists

BuildKit parsing has multiple “layers”:

1. **Parser AST** (`parser.Parse`) is intentionally permissive.
2. **Instruction decoding** (`instructions.Parse`) is stricter and may hard-fail on malformed instructions.
3. **LLB conversion / execution** fails on semantic issues that are only discoverable later.

Tally already sanitizes certain AST patterns (e.g., instructions before first `FROM`) to keep linting resilient.
This proposal extends that philosophy to a handful of additional high-value “parseable-but-non-buildable” cases.

## Proposed checks (5–10)

All rule codes below use the `tally/` namespace to avoid implying upstream parity with BuildKit/hadolint.

| Rule code | Severity | Category | What it catches |
|----------|----------|----------|-----------------|
| `tally/missing-from` | Error | Correctness | Dockerfile contains no `FROM` at all |
| `tally/unknown-instruction` | Error | Correctness | Likely-typo instruction keywords (`FORM`, `COPPY`, `WROKDIR`, …) |
| `tally/invalid-from` | Error | Correctness | Malformed `FROM` lines that still parse into AST (missing base / bad `AS`) |
| `tally/invalid-json-form` | Error | Correctness | JSON-form instructions with invalid JSON (e.g., `CMD [bash, -lc, "…"]`) |
| `tally/copy-from-empty-scratch-stage` | Error | Correctness | `COPY --from=<stage>` where `<stage>` is `FROM scratch` and truly empty |
| `tally/run-mount-from-unknown-stage` | Warning | Correctness | `RUN --mount=…from=<ref>…` where `<ref>` looks like a misspelled stage |
| `tally/circular-stage-deps` | Error | Correctness | Stage dependency cycles across `FROM <stage>` / `COPY --from` / `RUN --mount from=` |
| `tally/shell-run-in-scratch` | Warning | Correctness | Shell-form `RUN …` in a `FROM scratch` stage (almost always fails) |
| `tally/syntax-directive-typo` | Warning | Correctness | `# syntax=…` that doesn’t look like a valid image ref or common typos |

Notes:

- Existing coverage that overlaps conceptually (but not in behavior):
  - `hadolint/DL3061` already reports “instruction before first FROM”.
  - `buildkit/DuplicateStageName` and `hadolint/DL3022/DL3023` cover several multi-stage reference issues.
- The proposal here focuses on **hard build failures** and **“half-edited” files** that currently tend to surface as parse errors or confusing build
  errors.

---

## 1) `tally/missing-from` (Error)

### Scenario

File contains only `RUN`/`COPY`/`ENV` instructions because a `FROM` line was deleted during a refactor or patch application.

### Bad

```dockerfile
RUN apk add --no-cache ca-certificates
COPY . /app
```

### Expected behavior

Emit an error like: “Dockerfile has no `FROM` instruction; build has no stages.”

### Implementation sketch

- AST-only: scan top-level nodes for any `FROM` (case-insensitive).
- This is a great candidate for a **construction-time issue** (semantic builder), because it does not require typed instructions.

### False positives

Very low.

---

## 2) `tally/unknown-instruction` (Error)

### Scenario

Typos in instruction keywords are surprisingly common, and AI patching can create them when editing only the first token of a line.

### Bad

```dockerfile
FORM alpine:3.19
RUN echo "hello"
```

### Suggested UX

Produce a direct error, plus a suggestion:

- `Unknown instruction "FORM". Did you mean "FROM"?`

### Implementation sketch

- AST-only:
  - Extract `node.Value` for each top-level instruction node.
  - Compare (case-insensitive) against the canonical set of Dockerfile instructions.
  - If unknown, compute a small edit-distance suggestion against known instruction names.
- To keep linting resilient, consider **sanitizing unknown-instruction nodes** out of the AST passed to `instructions.Parse`, while still reporting
  `tally/unknown-instruction` against the original AST location.

### False positives

Low; Dockerfile doesn’t support “user-defined instructions”.

---

## 3) `tally/invalid-from` (Error)

### Scenario

The Dockerfile contains a `FROM` keyword but it’s malformed due to partial edits:

- Base image deleted but `AS name` left behind
- `AS` present but alias missing
- Flags present but image missing

### Bad (missing base)

```dockerfile
FROM AS builder
RUN go build ./...
```

### Bad (missing stage alias)

```dockerfile
FROM alpine:3.19 AS
RUN echo "ok"
```

### Implementation sketch

- AST-only:
  - For each `FROM` node, inspect its token chain (`node.Next`, `node.Next.Next`, …).
  - Validate minimally:
    - At least one token exists for base name.
    - If `AS` is present, it must be followed by a non-empty stage name token.
- Optional: Provide targeted suggestions (e.g., “`FROM` requires a base image name before `AS`”).

### False positives

Low.

---

## 4) `tally/invalid-json-form` (Error)

### Scenario

JSON-form instructions are easy to corrupt when a patch removes quotes or commas.

### Bad

```dockerfile
FROM alpine:3.19
CMD [bash, -lc, "echo hi"]
```

### Why it matters

This parses into the AST fine, but fails when decoding instructions (“invalid JSON array”).

### Implementation sketch

- AST-only:
  - For instructions that support JSON form (`CMD`, `ENTRYPOINT`, `RUN`, `SHELL`, `HEALTHCHECK`, `COPY`, `ADD`), detect if the raw value starts
    with `[` (trimmed).
  - Attempt to parse it as a JSON array; if it fails, emit `tally/invalid-json-form` at the instruction location.
- If this check exists, `dockerfile.Parse` can treat JSON decode failures as **violations** rather than a hard error, allowing other rules to run.

### False positives

Low (only triggers when the user is clearly trying to use JSON form).

---

## 5) `tally/copy-from-empty-scratch-stage` (Error)

### Scenario

Multi-stage builds sometimes end up with a “placeholder” stage:

- a stage is renamed/deleted
- a patch accidentally removes the `COPY`/`RUN` that was populating it
- other stages still copy from it

The most deterministic version of this is `FROM scratch` with **no file-producing instructions**.

### Bad

```dockerfile
FROM scratch AS artifacts

FROM alpine:3.19
COPY --from=artifacts /out/app /usr/local/bin/app
```

### Why it matters

If the `artifacts` stage is truly empty scratch, it contains no files. Any `COPY --from=artifacts …` is guaranteed to fail.

### Implementation sketch

- Semantic-model required (stage resolution + per-stage command list):
  - Find stages where `BaseName == "scratch"` and the stage has **no** `ADD`/`COPY` (and optionally no `RUN`).
  - If such a stage is used as a source for `COPY --from=<stage>`, emit an error.

### False positives

Very low.

---

## 6) `tally/run-mount-from-unknown-stage` (Warning)

### Scenario

BuildKit `RUN --mount=…from=…` is commonly used for cross-stage bind mounts (source code, toolchains). A one-character typo yields a build failure.

### Bad

```dockerfile
FROM alpine:3.19 AS builder
RUN echo "built" > /out/app

FROM alpine:3.19
RUN --mount=type=bind,from=bulider,source=/out,target=/mnt/out \
    cp /mnt/out/app /usr/local/bin/app
```

### Suggested UX

- Warn with “unknown stage” and offer a “did you mean …” suggestion when edit-distance is small.

### Implementation sketch

- Semantic-model required:
  - Extract mounts from `RUN` commands (`runmount.GetMounts`).
  - For each mount with `from=<ref>`:
    - If `<ref>` resolves to a prior stage index: ok.
    - Else if `<ref>` looks like an external image ref: ignore.
    - Else: warn and suggest closest stage name.

### False positives

Moderate (because `from=` can be an image ref). Mitigations:

- Only warn when:
  - `<ref>` does not contain `/` or `:` and
  - the best match is within a small edit distance

---

## 7) `tally/circular-stage-deps` (Error)

### Scenario

Refactors/patches can accidentally create cycles:

- stage A copies from stage B
- stage B copies from stage A

This is nonsensical for a build graph and should fail.

### Bad

```dockerfile
FROM alpine:3.19 AS a
COPY --from=b /x /x

FROM alpine:3.19 AS b
COPY --from=a /y /y

FROM alpine:3.19
COPY --from=a /x /x
```

### Implementation sketch

- Semantic-model required:
  - Build a dependency graph across:
    - `FROM <stage>` base refs
    - `COPY --from=<stage>`
    - `RUN --mount … from=<stage>`
  - Run a cycle detection pass (DFS with recursion stack, or Kahn topological sort).
  - Emit one error per cycle (ideally showing the cycle path).

### False positives

Very low.

---

## 8) `tally/shell-run-in-scratch` (Warning)

### Scenario

A common “shrink the image” edit is changing `FROM alpine` → `FROM scratch` without reworking the stage to avoid `RUN` steps.

### Bad

```dockerfile
FROM scratch
RUN echo "hello"
```

### Why it matters

Shell-form `RUN …` requires a shell (`/bin/sh`) in the rootfs. `scratch` does not provide one, so this almost always fails at build-time.

### Implementation sketch

- Semantic-model required (stage base name + command list):
  - If `BaseName == "scratch"` and there exists any shell-form `RUN`, warn.
  - (Optional refinement) only warn for `RUN` that is clearly shell form, not exec form.

### False positives

Low-to-moderate (advanced users can bootstrap a shell into scratch).
Make it configurable and easy to disable.

---

## 9) `tally/syntax-directive-typo` (Warning)

### Scenario

The `# syntax=` directive is often copy-pasted. Typos can break builds very early (frontend image can’t be resolved).

### Bad

```dockerfile
# syntax=docker/dokcerfile:1.7
FROM alpine:3.19
RUN echo "hi"
```

### Implementation sketch

- AST-only:
  - Parse the leading `# syntax=…` directive string (BuildKit exposes it via parser result metadata).
  - If present:
    - Validate it looks like an image reference (no spaces, non-empty).
    - If it is “close” to `docker/dockerfile`, suggest the correct spelling.

### False positives

Moderate (custom frontends exist). Make it a warning, and only suggest fixes for obvious typos.

---

## Implementation notes (where this fits in tally)

Some of these checks should be **AST-first** and resilient even when typed instruction parsing fails:

- `tally/missing-from`
- `tally/unknown-instruction`
- `tally/invalid-from`
- `tally/invalid-json-form`
- `tally/syntax-directive-typo`

Others require the semantic stage model:

- `tally/copy-from-empty-scratch-stage`
- `tally/run-mount-from-unknown-stage`
- `tally/circular-stage-deps`
- `tally/shell-run-in-scratch`

To maximize usefulness, the parser/lint pipeline should prefer:

1. Parse AST (always)
2. Run AST-only preflight checks (collect violations)
3. Sanitize AST for `instructions.Parse` (best-effort) to keep later rules running
4. Build semantic model and run stage-aware checks

This mirrors existing “sanitize to keep linting” behavior for DL3061/DL3043.
