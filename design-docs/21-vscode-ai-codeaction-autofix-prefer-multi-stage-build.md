# VS Code: AI CodeAction AutoFix for `tally/prefer-multi-stage-build` (Copilot / built-in assistant)

> Status: proposal

## 0. Context

Today, tally ships an **AI AutoFix** path for `tally/prefer-multi-stage-build` that runs via **ACP** when invoked from the CLI. See:

- `design-docs/13-ai-autofix-acp.md`
- `design-docs/19-ai-autofix-diff-contract.md` (future output contract improvement)

Separately, tally already has a VS Code LSP-based extension (`extensions/vscode-tally`) and supports code actions for deterministic fixes. See:

- `design-docs/11-vscode-extension-architecture.md`

This document proposes an editor-first alternative: when the user triggers a quick fix in VS Code, leverage VS Code’s **built-in AI assistant**
(GitHub Copilot Chat via the VS Code **Language Model API**) instead of ACP.

Key constraint: **tally should remain the source of truth for the strict objective prompt and validation rules**. The editor provides the “LLM
backend” and the UX surface.

## 1. Goals / non-goals

### Goals

- Provide a VS Code **CodeAction / Quick Fix** for `tally/prefer-multi-stage-build` that uses the user’s in-IDE assistant (Copilot Chat).
- Reuse tally’s existing **strict prompt** logic (signals, runtime invariants, “NO_CHANGE” escape hatch).
- Reuse tally’s existing **validation** logic (parse, stage-count, final-stage runtime invariants, lint/retry loop as appropriate).
- Keep this fix **explicit + unsafe** (never auto-apply on save / never part of `source.fixAll.tally`).

### Non-goals (for this first step)

- A general framework for all AI fixes across all IDEs (IntelliJ/Zed come later).
- Replacing ACP for CLI usage.
- Full “agentic” multi-file refactors. This is single-file, single-objective.
- Perfect control over Copilot’s *internal* prompt/context selection.

## 2. Upstream research (what VS Code / Copilot supports)

### 2.1 “Code action opens Editor Chat” pattern

Copilot Chat itself provides AI quick-fixes by registering `CodeActionProvider`s and invoking:

- `vscode.editorChat.start` (with `autoSend: true`, an initial selection/range, and a message).

Example in `microsoft/vscode-copilot-chat`:

- `src/extension/inlineChat/vscode-node/inlineChatCodeActions.ts` (`Fix` action uses `vscode.editorChat.start` with `/fix ...`)

This is a low-effort integration path, but it provides **low control** over the output contract and programmatic application.

### 2.2 VS Code Language Model API (`vscode.lm`)

VS Code exposes a stable API for extensions to:

- detect available chat model providers via `vscode.lm.selectChatModels(...)`
- send a request via `model.sendRequest([...messages])`
- stream response text via `response.text`

Copilot Chat registers as `vendor: "copilot"` (see `microsoft/vscode-copilot-chat`:
`src/extension/log/vscode-node/extensionStateCommand.ts` uses `vscode.lm.selectChatModels({ vendor: "copilot" })`).

This is the path that enables **strict output contracts** and **controlled application** (because the tally extension can parse the response and
apply a workspace edit itself).

## 3. Proposal (high-level)

Implement a new editor-only AI AutoFix flow:

1. **tally LSP server** produces/returns the *exact* strict prompt for the objective (same logic as ACP flow).
2. **VS Code extension** sends that prompt to the built-in model (`vscode.lm`, preferring `vendor: "copilot"`).
3. **VS Code extension** returns the model output back to **tally LSP server** for validation + conversion into a `WorkspaceEdit`.
4. VS Code applies the edit (optionally after a preview step).

In other words: **tally owns “what to do” and “is this acceptable?”**; VS Code owns “ask the model” and UX.

## 4. Detailed design

### 4.1 User experience

When a Dockerfile triggers `tally/prefer-multi-stage-build`, show a quick fix:

- **Title:** `AI AutoFix (Copilot): convert to multi-stage build`
- **Kind:** `QuickFix` (but not “preferred”)
- **Safety:** treated as **unsafe**; always user-triggered

Execution flow:

1. User triggers the action.
2. Extension confirms an AI model is available (Copilot Chat installed, signed in, SKU permits).
3. Extension requests a strict prompt from the tally LSP server (for the *current in-editor content*, including unsaved changes).
4. Extension calls the model, shows progress, and collects output.
5. Extension asks tally LSP server to validate/convert the output into edits.
6. Extension applies edits (or shows a diff preview and then applies).

### 4.2 VS Code extension changes (`extensions/vscode-tally`)

Add:

1. A `CodeActionProvider` scoped to Dockerfiles that:
   - filters diagnostics to `source === "tally"` and `code === "tally/prefer-multi-stage-build"`
   - returns the AI quick fix code action (command-based)
2. A command handler (e.g. `tally.aiAutofixPreferMultiStageBuild`) that:
   - selects a model: `vscode.lm.selectChatModels({ vendor: "copilot" })`
   - calls a new LSP request to get the prompt
   - runs `sendRequest` and collects response text
   - calls a new LSP request to validate/convert output into a `WorkspaceEdit`
   - applies the edit

Notes:

- **Do not** rely on proposed `CodeAction.isAI` (`codeActionAI`) initially; it is still a VS Code proposal and would force proposed-API packaging.
- The extension currently hides the `LanguageClient` instance; we likely need a small refactor to expose a safe `sendRequest` method on
  `TallyLanguageClient` for custom requests.

### 4.3 LSP server changes (`internal/lspserver`)

Add two custom LSP requests (names are illustrative; final naming should follow existing `tally.*` command patterns):

1. `tally/aiAutofixPrompt`
   - **Input:** `{ uri: string, rule: "tally/prefer-multi-stage-build" }`
   - **Output:** `{ prompt: string, outputContract: "dockerfile" | "diff", promptVersion: string }`
   - Implementation:
     - resolve current content for `uri` from the document store (not disk)
     - run the same prompt builder used by `internal/ai/autofix` (Round 1 for now)
     - return the prompt string verbatim (no editor-specific rewriting)

2. `tally/aiAutofixValidate`
   - **Input:** `{ uri: string, rule: "tally/prefer-multi-stage-build", modelOutput: string }`
   - **Output:** `{ edit?: WorkspaceEdit, noChange: boolean, diagnostics?: Array<{ message: string }> }`
   - Implementation:
     - parse the model output using the current contract (today: `NO_CHANGE` or one ` ```Dockerfile ` block; later: diff contract from doc 19)
     - run the existing validation logic (`internal/ai/autofix/checkProposal` and invariants)
     - convert the accepted result into a `WorkspaceEdit`
       - MVP: whole-file replacement (one text edit)
       - later: diff-based edits if/when we adopt patch output as default

Why custom requests instead of `workspace/executeCommand`?

- Requests allow a structured response payload and make it easier to add richer validation errors without overloading “command return” semantics.
- This flow is “interactive and long-running”; in future we may want progress notifications and cancellation.

### 4.4 Output contract (what we ask the model to return)

MVP should keep the **current contract** (because we want “tally provides the same strict prompt”):

- `NO_CHANGE` (exact)
- or one fenced ` ```Dockerfile ` code block containing the full updated Dockerfile

However, editor-integrated application strongly benefits from the patch contract proposed in `design-docs/19-ai-autofix-diff-contract.md`.
If/when we switch the objective to patch output, this VS Code flow becomes simpler and safer:

- fewer “rewrite unrelated parts” failures
- smaller diffs and cleaner undo/preview
- cheap patch-level acceptance heuristics

Recommendation: keep MVP on the existing Dockerfile output contract, but design the request/response types to support `outputContract="diff"` later.

### 4.5 Model selection + availability

Preferred model selection:

- `await vscode.lm.selectChatModels({ vendor: "copilot" })`
  - if empty, show an actionable error: “Install and sign in to GitHub Copilot Chat to use this fix.”

#### 4.5.1 Practical detection: installed / enabled / logged-in-with-plan

There is no single stable “Copilot status” API. The most practical approach is to:

1. **Installed (best-effort):** `vscode.extensions.getExtension("github.copilot-chat") !== undefined`
2. **Enabled (best-effort):** `await ext.activate()` succeeds
3. **Has usable Copilot models (strong signal):** `await vscode.lm.selectChatModels({ vendor: "copilot" })` returns `>= 1` model
4. **Can actually run (definitive, on user action):** call `model.sendRequest(...)` and handle `vscode.LanguageModelError`
   - `NoPermissions`: missing consent *or* no Copilot permissions/subscription
   - `Blocked`: blocked/quota/policy
   - `NotFound`: model disappeared; re-select

For better UX when `models.length === 0`, optionally check GitHub sign-in without prompting:

- `await vscode.authentication.getSession("github", ["read:user"], { createIfNone: false })`
  - no session → likely “not signed in”
  - session exists → likely “Copilot not available for this user/plan” (or Copilot Chat disabled by policy)

Optional enhancements:

- If multiple Copilot models are available, show a `QuickPick` (or store a per-user preference in settings).
- Timeouts and cancellation:
  - propagate `CancellationToken` from VS Code to the LSP request and to `sendRequest`.

### 4.6 Security / privacy

This flow sends Dockerfile content to a model provider (Copilot). Guardrails:

- It is always **user-triggered** (no background AI).
- Keep using tally’s best-effort secret redaction where applicable, but note:
  - redaction must not break validation (see `design-docs/19-ai-autofix-diff-contract.md` §3.2 / §3.2.3).
- Add extension setting: `tally.aiAutofix.enableCopilot` (default: `false` or “experimental”) to ensure explicit opt-in.
- Always log the action to the extension output channel (what happened; not the raw prompt/output unless debug mode).

### 4.7 Failure modes and UX handling

- No Copilot model available → show error + link/steps.
- Model returns malformed output → show error, include a “Show raw output” button (logs to Output channel).
- Validation fails (runtime invariants, parse errors, stage-count) → show a concise failure summary and offer:
  - “Open Copilot Chat with the strict prompt + validation errors” (fallback path using `vscode.editorChat.start`)
  - “Try again” (bounded retries; extension-controlled)

## 5. Implementation plan (incremental)

1. Add VS Code extension CodeAction + command skeleton (hidden behind `tally.aiAutofix.enableCopilot`).
2. Add LSP custom request `tally/aiAutofixPrompt` for Round 1 prompt generation.
3. Add LSP custom request `tally/aiAutofixValidate` for parsing + validation + `WorkspaceEdit` generation.
4. Add minimal UX: progress reporting + apply edit.
5. Add tests:
   - Go: request handlers return stable prompt and reject malformed outputs.
   - VS Code: integration test that stubs `vscode.lm` with a test model provider (mirroring VS Code’s own API tests) and asserts edit application.

## 6. Open questions

- Should we treat Copilot as the only vendor, or allow any `vscode.lm` provider (Anthropic, local, etc.)?
  - MVP: prefer `vendor: "copilot"`, but keep the selector pluggable.
- Preview UX: apply directly (undoable) vs always show diff before apply?
  - For multi-stage conversion (large rewrite), a preview-first UX is likely better.
- How should we share the “retry loop” between CLI (ACP) and editor flow to avoid duplication?
  - Likely requires the engine refactor proposed in `design-docs/19-ai-autofix-diff-contract.md`.
