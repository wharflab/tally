# AI AutoFix via ACP: Diff/Patch Output Contract

> Status: proposal

## 0. Context

This document proposes an evolution of the AI AutoFix system described in:

- `design-docs/13-ai-autofix-acp.md`

Current behavior (implemented): the agent is prompted to return **a full Dockerfile** inside a single fenced ````Dockerfile` block. Tally then treats
the response as a whole-file replacement and validates the resulting Dockerfile (BuildKit parse + invariants + lint + bounded retry loop).

This proposal switches the agent output contract to: **return a git/unified diff patch against the provided Dockerfile**.

## 1. Decision

Adopt a **patch-based ACP output contract** for AI AutoFix.

- Default for AI AutoFix objectives should become: **patch output**.
- Whole-file output remains a possible fallback/option (either as a compatibility mode or for objectives where patching is demonstrably worse).

Rationale: patches are easier to validate heuristically, can be rejected cheaply, and tend to encourage smaller, more targeted edits.

## 2. Motivation (predictability + heuristic analysis)

### 2.1 Why full-file rewrites are hard to reason about

When the model outputs an entire Dockerfile, tally can validate the *result* (parse + invariants) but it’s hard to validate the *process*:

- Was the change minimal or did the model rewrite unrelated sections?
- Did it drop comments or reorder instructions unnecessarily?
- Did it subtly change whitespace or formatting in ways that cause noisy diffs?

For rules like `tally/prefer-multi-stage-build`, the desired outcome is structurally large but still has important invariants:

- **Stage count increases** (typically from 1 to 2+).
- **Final-stage runtime settings remain identical** (already enforced in code today).

A patch contract lets us enforce structural and “blast radius” heuristics before we even parse the proposed Dockerfile.

### 2.2 Expert opinion: “diff forces focus” is directionally true but not sufficient

Requiring a diff often nudges models toward incremental edits because:

- The model must identify *what* it is changing.
- The output format encodes unchanged context explicitly.

However, a diff **does not guarantee minimality**:

- The model can still output a single hunk that deletes the whole file and re-adds everything.
- The model can generate syntactically valid but semantically wrong patches.

So the value is not just “models behave better”; it’s that patches give tally an **inspectable artifact** that makes a heuristic acceptance loop
feasible.

## 3. Benefits and trade-offs

### 3.1 Benefits

1. **Heuristic validation becomes practical**
   - Example: for multi-stage conversion, ensure the patch adds at least one `FROM` line (`+FROM ...`).
   - More generally: enforce that certain instructions are not touched, or that changes are bounded.

2. **Better UX for preview and auditing**
   - Patches are exactly what reviewers want to see.
   - This aligns with the LSP “preview/diff UX” hallway already noted in doc 13.

3. **Potentially smaller edits and fewer noisy changes**
   - Even if a multi-stage conversion is large, it can still avoid rewriting unrelated areas.

4. **More reusable fix engine**
   - A patch-based “objective loop” can be reused by future AI rules: each objective declares patch acceptance criteria.

### 3.2 Costs / risks

1. **Patch correctness is a new failure mode**
   - Model must emit a parseable diff.
   - Patch application can fail due to malformed hunks or mismatched context.

2. **Secret redaction interacts poorly with patch application** (critical)
   - Patch application requires the agent to see the exact base content it’s diffing against.
   - If we redact content inside the base Dockerfile block, the model will generate a patch against redacted text, which may not apply to the real
     file.

3. **More complex prompt/parse pipeline**
   - We add a patch parser + applier step before existing post-apply validation.

4. **Not all objectives are a good fit**
   - Some transformations are easiest expressed as an AST rewrite or whole-file regeneration.

## 4. Proposal: Patch-based objective loop

This proposal extends the existing resolver loop (§`internal/ai/autofix/resolver.go`) with an additional phase:

1. **Prompt → patch text**
2. **Parse patch** (strict output contract)
3. **Apply patch to base Dockerfile** (pure Go)
4. **Patch-level acceptance heuristics** (cheap)
5. **Post-apply validation** (existing: parse + invariants + lint)
6. If failures remain: **iterate** (bounded rounds) with structured feedback

Key property: the agent is always asked for **one patch for one objective for one file**.

This already matches tally’s fix pipeline: async fixes are resolved sequentially within a file (`internal/fix/fixer.go`), so each objective sees the
current content.

## 5. Patch output contract (wire format)

### 5.1 Input provided to the agent

- The full current Dockerfile content (normalized to LF) in a fenced block.
- Any rule-specific signals/registry context as today.
- Explicit file identity:
  - `filePath` (full path) for the client.
  - `fileName` (basename) for the agent.

### 5.2 Output required from the agent

The agent MUST output exactly one of:

1. `NO_CHANGE` (exact, trimmed)
2. Exactly one fenced code block:

```text
```

```diff
<git/unified diff patch>
```

```text

Constraints on the patch:

- Patch must modify **exactly one file**.
- Patch must not create or delete the file (`/dev/null` is rejected).
- Patch must be text-only (no git binary patches).
- Patch should be minimal and focused:
  - no unrelated whitespace-only churn unless required
  - preserve comments as part of the transformation

Accepted header shapes (be liberal in what we accept; strict in what we apply):

- `diff --git a/<name> b/<name>` style, with `---`/`+++` headers and hunks
- plain unified diff with `---`/`+++` headers and hunks

### 5.3 File path matching

To reduce “patch aimed at a different file” errors:

- Tally accepts patch file names equal to:
  - `<basename(filePath)>`
  - `a/<basename>` / `b/<basename>`
  - the normalized `filePath` (rare, but sometimes generated)

If mismatch: reject with a structured blocking issue: `patch_file_mismatch`.

### 5.4 Size limits

Reuse the existing hard limits philosophy:

- Prompt size: `ai.max-input-bytes` (existing)
- Response size: hard cap (existing concept in doc 13; ensure enforced for patch too)
- Patch changed-line limits: rule-specific and optional (see §7.2)

## 6. Patch parsing and application

### 6.1 Library choice

Use a pure-Go implementation; avoid spawning `git`.

Recommended: `github.com/bluekeyes/go-gitdiff/gitdiff`.

- Parses patches from `git diff`, `git show`, `git format-patch`.
- Applies patches in Go (`gitdiff.Apply`).
- Designed to match `git apply` semantics.

Alternatives:

- `sourcegraph/go-diff`: good parser, no built-in apply.
- Spawning `git apply`: non-portable in minimal environments; harder to sandbox; adds toolchain dependency.

### 6.2 Application algorithm (high-level)

Given:

- `base` = the exact Dockerfile content used in the prompt (LF-normalized)
- `patchText` = the model output from the `diff` fenced block

Steps:

1. Parse patch into file diffs.
2. Enforce: exactly one file diff; file name matches (§5.3).
3. Apply patch to `base` to obtain `proposed`.
4. Re-apply trailing newline policy (match original file newline behavior).

If apply fails:

- Convert error into a blocking issue `patch_apply_failed` with a short message.
- Feed that into the next round prompt.

### 6.3 Strict vs fuzzy apply

Start strict.

Because tally provides the exact base content to the agent each round, strict apply should usually succeed; failures are mostly output-format issues.

If strict apply causes too many failures in practice, introduce an explicit “fuzzy apply” mode later:

- tolerate nearby context shifts
- tolerate whitespace-only differences in context lines

…but only if bounded and testable.

## 7. Heuristic acceptance criteria

### 7.1 Two layers of validation

1. **Patch-level validation** (cheap, before apply or immediately after apply)
2. **Post-apply validation** (existing)
   - BuildKit parse
   - objective-specific invariants (stage count, runtime settings)
   - re-lint with AI disabled + safe sync normalization (existing behavior)

Patch-level validation exists to reject obviously-wrong changes early and provide higher-quality feedback to the model.

### 7.2 Patch-level validator interface (proposal)

Add a small abstraction (names illustrative):

```

```go
// internal/ai/autofix/acceptance

type PatchCheck interface {
  ID() string
  // ValidatePatch inspects the patch text and/or parsed patch structure.
  ValidatePatch(p Patch) error
}

type PostApplyCheck interface {
  ID() string
  ValidateAfter(base, proposed []byte, meta Meta) error
}
```

Where `Patch` is the parsed patch representation (e.g., a wrapper around go-gitdiff types).

The resolver can be configured with:

- generic checks (single file, size limits)
- objective-specific checks (provided by the rule / objective)

### 7.3 Example checks for `tally/prefer-multi-stage-build`

**Patch-level** (fast):

- `must-add-from`: ensure at least one added line matches `^\+FROM\b`.
  - This detects no-op patches and encourages the intended structural change.

Optionally:

- `limit-churn`: reject if the patch changes > N% of lines (configurable).
  - Caveat: multi-stage conversion can legitimately be large; this should start lenient.

**Post-apply** (existing + new wiring):

- `stage-count`: ensure 2+ stages when original had 1 (already implemented: `validateStageCount`).
- `runtime-invariants`: ensure final-stage runtime settings unchanged (already implemented: `validateRuntimeSettings`).

### 7.4 Heuristic feedback format

When a patch fails acceptance, represent it as a blocking issue (same mechanism used today):

```json
{ "rule": "patch/must-add-from", "message": "Patch does not add a FROM instruction" }
```

The next-round prompt should instruct the model to fix *only* the blocking items.

## 8. Prompting strategy

### 8.1 Round 1 prompt changes

Replace the “output full Dockerfile” requirement with “output a patch”.

Keep the same:

- strict rules about not rewriting unrelated parts
- signals
- registry insights
- final-stage runtime summary

Add:

- explicit patch format rules
- an example minimal patch snippet (short) to improve compliance

### 8.2 Round 2 (“fix blocking issues”) changes

Currently round 2 includes:

- blocking issues JSON
- current proposed Dockerfile
- request: output full corrected Dockerfile

With patch contract:

- base becomes “Current Dockerfile (the patch must apply to this exact content)”
- request: output a patch that fixes *only* the blocking issues

This preserves the existing “bounded rounds” model while making each iteration more targeted.

### 8.3 Malformed output retry

Keep `maxMalformedRetries` but switch the simplified prompt to request a patch.

## 9. Secret handling: does `redact-secrets` help?

### 9.1 What problem we’re actually solving

`ai.redact-secrets` does **not** make the *Docker image* or the *Dockerfile* more secure. If a Dockerfile contains real secrets, that is already a
security bug (and tally should flag it via dedicated rules).

What `redact-secrets` can help with is **data exfiltration risk** when AI AutoFix is enabled:

- The Dockerfile content (including accidental secrets) is sent to an external agent process.
- That agent may forward content to a hosted LLM provider, store logs, cache prompts, etc.

So the control is about **privacy / minimizing accidental disclosure**, not about “fixing the fact that secrets are present”.

### 9.2 Threat model realities (why redaction has limited security value)

Even as a privacy control, naive redaction has sharp limits:

- AI AutoFix is explicitly opt-in and the agent is **user-provided**. If the user points tally at an agent that reads the filesystem or uploads data,
  prompt redaction is not a sandbox.
- Redaction is heuristic (gitleaks-based): false positives and false negatives are unavoidable.
- With either output contract (whole-file or patch), hiding secret values from the agent makes it impossible for the agent to reliably preserve them.
  - Whole-file output: the agent can’t reproduce what it can’t see.
  - Patch output: the patch is authored against redacted text and may not apply to the real file.

### 9.3 Recommendation: deprecate “redact in prompt” and replace with an explicit **secrets policy**

Given the above, *pretending* we can “securely redact” while still asking for deterministic transformations is counterproductive.

Recommended direction:

- Replace `ai.redact-secrets` with a more honest setting, e.g.:

  - `ai.secrets-policy = "allow" | "deny" | "tokenize"` (names illustrative)

Where:

- `deny` (recommended default): if secrets are detected in the Dockerfile payload we would send to the agent, **do not run AI AutoFix** and surface a
  clear skip reason.
  - This avoids patch-apply breakage and avoids silently deleting/changing secrets.
  - It also aligns with the principle: if secrets are present, stop and fix that first.

- `allow`: run AI AutoFix even if secrets are detected. This is for users running a trusted, local/offline agent.

- `tokenize` (future): use placeholder tokens + reinjection (see §9.4). This attempts to preserve determinism while minimizing disclosure.

In the short term, if we keep `ai.redact-secrets` for compatibility, its behavior should effectively become:

- **Deny-by-default** for patch mode (skip when secrets detected), rather than mutating the base Dockerfile text.

### 9.4 Placeholder/token strategy (future)

If we want a middle ground between `deny` and `allow`, tokenization is the only approach that can work with patches:

1. Replace each detected secret with a stable token (`TALLY_SECRET_1`, ...)
2. Keep a mapping `{token -> originalSecret}` in-memory (never log)
3. Send tokenized Dockerfile to the agent
4. Apply patch to tokenized base
5. Re-inject secrets by reversing the mapping

This is feasible but must be implemented carefully:

- avoid partial/overlapping matches
- preserve quoting/escaping
- ensure tokens cannot collide with real content
- ensure secrets never appear in logs/errors/snapshots

## 10. Implementation plan (code changes)

This doc is a design+implementation spec; below is the concrete code-level plan.

### 10.1 Core resolver changes

Touch points:

- `internal/ai/autofix/prompt.go`
  - update output format instructions to `diff` code block
  - update round 2 prompt accordingly

- `internal/ai/autofix/parse.go`
  - replace `parseAgentResponse` to accept `NO_CHANGE` or a single ````diff` block

- `internal/ai/autofix/resolver.go`
  - after parsing: apply patch to the current round input to produce `proposed`
  - attach patch-parse/apply failures as blocking issues (like syntax/runtime failures today)

- new helper package (suggested): `internal/patch`
  - `ParseAndApply(base []byte, patchText string, expectedName string) ([]byte, PatchMeta, error)`
  - hides go-gitdiff types behind a local API

### 10.2 Rule-specific acceptance

For `tally/prefer-multi-stage-build`:

- define a patch-level check `must-add-from` (cheap)
- keep existing post-apply checks:
  - stage count increases
  - runtime invariants unchanged

### 10.3 Dependencies

- Add `github.com/bluekeyes/go-gitdiff` to `go.mod`.

### 10.4 Testing updates

- Unit tests:
  - patch response parser (`NO_CHANGE`, single diff block, reject malformed)
  - patch apply success/failure fixtures
  - patch acceptance checks for `must-add-from`

- Integration tests:
  - update fake ACP agent (`internal/ai/acp/testdata/testagent`) to return a patch instead of full Dockerfile
  - update `internal/integration/fix_cases_test.go` / scenario fixtures as needed

- Snapshot tests:
  - if CLI output changes, update snapshots accordingly

## 11. Extensibility: shared AI AutoFix infrastructure (multiple objectives)

### 11.1 Goal

Make it cheap to add a second (and third, …) ACP-powered fix without duplicating:

- agent execution + retry loop
- response parsing
- patch parsing/application
- common validation plumbing
- failure reporting

Current state: `internal/ai/autofix/*` is effectively a **single-objective** implementation tailored to `tally/prefer-multi-stage-build`.

### 11.2 Proposed refactor: “objective-driven” engine

Keep **one** FixResolver (`ResolverID = "ai-autofix"`) but refactor it into:

1. A small, reusable **engine** that runs the patch loop.
2. A set of **objectives** (per-rule implementations) that plug into the engine.

Suggested package layout:

- `internal/ai/autofix/engine/`
  - `Engine` (runs rounds, calls agent, parses output, applies patch, runs checks)
  - shared prompt scaffolding helpers
  - shared blocking issue types

- `internal/ai/autofix/objectives/`
  - `multistage/` (existing prefer-multi-stage-build logic migrated here)
  - `healthcheck/` (new DL3057 objective)

- `internal/patch/`
  - pure-Go patch parse/apply wrapper (backed by `bluekeyes/go-gitdiff`)

Core interfaces (illustrative):

```go
// Objective is implemented per AI-enabled rule.
type Objective interface {
  ID() string // usually the rule code, e.g. "tally/prefer-multi-stage-build"

  // BuildPrompt emits the prompt for a given round.
  // The engine provides current base content and any blocking issues.
  BuildPrompt(ctx PromptContext) (string, error)

  // ParseAgentOutput returns either NO_CHANGE or a patch string.
  ParseAgentOutput(text string) (PatchResult, error)

  // PatchChecks run on the patch (or parsed patch) before expensive validation.
  PatchChecks() []PatchCheck

  // PostApplyChecks run on the proposed Dockerfile.
  PostApplyChecks() []PostApplyCheck

  // RetryPolicy controls max rounds + malformed retries.
  RetryPolicy() RetryPolicy
}
```

The resolver becomes mostly:

- decode `sf.ResolverData` into a typed objective request
- select objective implementation
- run `engine.RunObjective(...)`

Objective implementations own:

- prompt content (including “when to say NO_CHANGE” guidance)
- objective-specific acceptance checks
- objective-specific semantic invariants

### 11.3 Shared validation building blocks

To keep objective code small and consistent, provide shared helpers:

- `ValidateBuildkitParse(proposed)`
- `ValidateFinalStageRuntimeInvariant(orig, proposed, allow Allowlist)`
  - For multistage: allowlist is empty (no changes allowed)
  - For DL3057: allowlist includes HEALTHCHECK (adding/changing allowed), but everything else must match
- `ValidateWithLint(proposed)` (existing: lint with AI disabled + apply safe sync normalization)

### 11.4 New objective: AI AutoFix for `hadolint/DL3057` (HEALTHCHECK instruction missing)

#### 11.4.1 Objective intent

When DL3057’s **missing HEALTHCHECK** violation is present (after async base-image refinement), attempt to add a **final-stage**
`HEALTHCHECK CMD ...`.

This objective is inherently uncertain; the design should expect:

- In many cases we cannot infer a correct check.
- The agent should frequently return `NO_CHANGE` (target: >= 50% of cases, per your expectation).

#### 11.4.2 Prompt contract (high level)

Input context should be explicitly structured and conservative:

- Dockerfile content
- Final-stage runtime summary (ENTRYPOINT/CMD/WORKDIR/USER/EXPOSE/ENV/LABEL)
- Signals extracted heuristically from the Dockerfile (examples):
  - exposed ports (e.g., `EXPOSE 8080`)
  - presence of `curl`/`wget` install commands
  - hints of common servers (nginx, apache, caddy, node, uvicorn, gunicorn, dotnet aspnet)

Strict instructions:

- Do **not** add new dependencies/packages just to implement a healthcheck.
- Prefer simple checks using already-present tools.
- If you cannot infer a high-confidence healthcheck, output `NO_CHANGE`.
- Output a single `diff` patch.

#### 11.4.3 Acceptance criteria (heuristics)

Patch-level checks:

- `must-add-healthcheck`: patch must add a line matching `^\+HEALTHCHECK\b`.
- `must-add-healthcheck-cmd`: after apply, the final stage must contain a HEALTHCHECK whose test is CMD/CMD-SHELL (not NONE).
- `runtime-invariants-except-healthcheck`: do not change final-stage settings other than adding/modifying HEALTHCHECK.

Post-apply checks:

- BuildKit parse
- Re-lint with AI disabled and ensure no new SeverityError regressions (existing pipeline)

Safety classification:

- Treat as `FixUnsafe` (adding HEALTHCHECK can change runtime behavior and resource use).

#### 11.4.4 What “NO_CHANGE” means for this objective

`NO_CHANGE` is a successful, expected outcome:

- It should not be treated as an error.
- The violation remains, and the user can add HEALTHCHECK manually.

This is important to avoid incentivizing the model to hallucinate a healthcheck.

#### 11.4.5 Example “confident” cases (non-exhaustive)

- `EXPOSE 80` or `EXPOSE 8080` and `curl` is present → HTTP GET probe to `http://localhost:<port>/`.
- `EXPOSE 5432` and `pg_isready` is present (rare in app images, more in postgres images) → use it.

The prompt should also explicitly forbid “guessing” endpoints beyond generic `/`.

### 11.5 Testing strategy for DL3057 objective

- Add fixtures where:
  - The fake ACP agent returns `NO_CHANGE` (uncertain)
  - The fake ACP agent returns a minimal patch adding a HEALTHCHECK (confident)

- Unit test the invariant checker that allows HEALTHCHECK changes but rejects:
  - changes to CMD/ENTRYPOINT/EXPOSE/USER/WORKDIR/ENV/LABEL

## 12. Rollout strategy

Because AI AutoFix is opt-in, we can change the contract with minimal ecosystem risk, but we should still stage it:

1. Add patch contract behind an internal flag/config knob (e.g. `ai.autofix-output = "patch"|"dockerfile"`).
2. Convert `prefer-multi-stage-build` to use patch output by default.
3. Keep whole-file output as fallback during transition.
4. Once stable, remove the fallback or keep it as a “compatibility mode”.

## 13. Open questions

1. Should the patch contract be global for all AI objectives, or per-objective?
2. What should we do when the agent outputs a patch that applies cleanly but changes almost everything?
   - accept (post-apply validation passes)
   - or reject based on churn heuristics?
3. How strict should file name matching be for patches?
4. What is the best MVP behavior for secrets detected in Dockerfile content?

---

### Appendix A: Example prompt excerpt

```text
Output format:
- Either output exactly: NO_CHANGE
- Or output exactly one fenced block:
```

  ```diff
  diff --git a/Dockerfile b/Dockerfile
  --- a/Dockerfile
  +++ b/Dockerfile
  @@ ...
  ...
  ```

```text

### Appendix B: Example acceptance heuristic for prefer-multi-stage-build

- Reject if patch contains no line starting with `+FROM `.
- Reject if patch touches more than X lines without increasing stage count (post-apply validation will also catch this).
```
