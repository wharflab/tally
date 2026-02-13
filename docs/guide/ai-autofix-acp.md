# AI AutoFix via ACP

tally supports **opt-in AI AutoFix** for the kinds of Dockerfile improvements that are hard to express as a purely mechanical rewrite (or too risky
to apply without extra validation).

Instead of asking you for an API key, tally integrates with **ACP (Agent Client Protocol)** — a protocol created by the
[Zed editor](https://zed.dev/) to standardize how tools talk to “coding agents”.

From a user perspective, that means:

- You choose **which agent** you want to use (Gemini CLI, OpenCode, GitHub Copilot CLI, …).
- You keep **credentials and model choice** inside that agent.
- tally stays a **linter first** — fast, deterministic where possible — and uses AI only when you explicitly opt in.

## Recommended setup (low latency)

Dockerfiles are a mature, relatively stable domain that most modern models are already trained on. For AI fixes, you usually don’t need external
tools or context servers — you want fast, predictable transformations.

For that reason, we recommend:

- A **fast/smaller model** with solid general reasoning.
- Disabling any agent-side tool integrations (for example, MCP servers) unless you *know* you need them.

Example (Gemini CLI):

```bash
gemini --experimental-acp --allowed-mcp-server-names=none --model=gemini-3-flash-preview
```

Note: `--allowed-mcp-server-names` is an allowlist. Using a name you don’t have configured (like `none`) effectively disables all MCP servers.
tally doesn’t provide any MCP servers to the agent today, so enabling MCP is usually just extra startup/latency overhead.

## Quick Start

### 1) Pick an ACP agent

The simplest way to get started is an ACP-capable CLI agent, such as:

- Gemini CLI (native ACP): <https://agentclientprotocol.com/agents/gemini-cli>
- OpenCode (native ACP): <https://agentclientprotocol.com/agents/opencode>
- Kiro CLI (native ACP): <https://agentclientprotocol.com/agents/kiro>
- Cline (CLI v2, native ACP): <https://agentclientprotocol.com/agents/cline>
- GitHub Copilot CLI (native ACP): <https://agentclientprotocol.com/agents/github-copilot>

You can always browse the latest registry here:

- ACP Registry: <https://agentclientprotocol.com/get-started/registry>

### 2) Enable AI in `.tally.toml`

Create or update `.tally.toml`:

```toml
[ai]
enabled = true
timeout = "90s"
max-input-bytes = 262144
redact-secrets = true

# Example: Gemini CLI (recommended: fast model + no MCP servers)
command = [
  "gemini",
  "--experimental-acp",
  "--allowed-mcp-server-names=none",
  "--model=gemini-3-flash-preview",
]
```

### 3) Run an AI-powered fix

AI fixes are intentionally marked as **unsafe**. That means they require `--fix-unsafe` in addition to `--fix`.

For best results, narrow the blast radius to a single rule:

```bash
tally lint \
  --fix --fix-unsafe \
  --fix-rule tally/prefer-multi-stage-build \
  path/to/Dockerfile
```

Tip: Consider enforcing “explicit only” at the rule level so you never run AI fixes accidentally:

```toml
[rules.tally.prefer-multi-stage-build]
fix = "explicit"
```

## How It Works (User Mental Model)

tally treats AI AutoFix as a normal part of its existing fix pipeline:

1. A rule reports a violation and may attach a **SuggestedFix**.
2. For “simple” fixes, tally applies edits locally (no AI).
3. For AI fixes, the SuggestedFix is marked **async** — tally will:
   - Build a prompt (including the Dockerfile text + structured evidence)
   - Run your configured agent via **ACP over stdio**
   - Parse the response using a strict output contract (either `NO_CHANGE` or a full Dockerfile)
   - Validate the proposal (syntax + sanity checks + lint feedback)
   - Apply a **whole-document replacement** if it passes validation

If the agent output is malformed, unsafe, or fails validation, tally skips the fix and continues linting. Linting should still work even when AI is
misconfigured or unavailable.

### “Why did it say Skipped N fixes?”

Common reasons:

- You didn’t pass `--fix` (no fixes run at all).
- You didn’t pass `--fix-unsafe` (AI fixes won’t run).
- You set `--fix-rule ...` and the rule you picked **didn’t trigger** for that Dockerfile.
- Example: `tally/prefer-multi-stage-build` only triggers for Dockerfiles with **exactly one `FROM`**.
- Your AI agent timed out or failed. tally prints the reason on stderr (and keeps stdout clean for JSON/SARIF).

## Why ACP Is a Better Fit Than API Keys

Lots of tools bolt AI onto a linter by asking for an OpenAI/Anthropic API key. That’s easy to ship, but it’s a poor long-term UX for a linter:

- **Provider lock-in**: the linter becomes a mini “AI platform” that must track models, pricing, retries, and auth.
- **Secret sprawl**: you end up storing API keys in dotfiles, CI secrets, and team docs.
- **Enterprise friction**: organizations often standardize on a specific gateway, proxy, or provider policy.
- **Inconsistent experience**: your editor agent knows your preferences, but your linter uses a totally different stack.

ACP flips that around:

- tally stays **agent-agnostic**.
- You bring your own agent (and your existing auth setup).
- You can switch models/providers without waiting for tally to add a new integration.

Many ACP agents also support tool/context integrations (for example, MCP servers). For Dockerfile fixes, we recommend keeping those disabled for
lower latency and fewer surprises — the Dockerfile text and rule evidence is usually enough.

## Configuration Reference

### Config file (`.tally.toml`)

All AI settings live under `[ai]`:

```toml
[ai]
enabled = false                 # Default: false
command = ["gemini", "--experimental-acp", "--allowed-mcp-server-names=none", "--model=gemini-3-flash-preview"]
timeout = "90s"                 # Per-fix timeout
max-input-bytes = 262144        # Prompt size limit
redact-secrets = true           # Default: true
```

| Setting | Default | Meaning |
|---------|---------|---------|
| `ai.enabled` | `false` | Master kill-switch for AI features |
| `ai.command` | *(empty)* | ACP agent argv (stdio). If empty, AI fixes can’t run |
| `ai.timeout` | `"90s"` | Per-fix timeout for the ACP interaction |
| `ai.max-input-bytes` | `262144` | Maximum prompt size to send to the agent |
| `ai.redact-secrets` | `true` | Redact obvious secrets in prompts (best-effort) |

### Environment variables

- `TALLY_AI_ENABLED=true`
- `TALLY_ACP_COMMAND="gemini --experimental-acp --allowed-mcp-server-names=none --model=gemini-3-flash-preview"`
- `TALLY_AI_TIMEOUT=90s`
- `TALLY_AI_MAX_INPUT_BYTES=262144`
- `TALLY_AI_REDACT_SECRETS=true`

### CLI flags

AI flags (typically used together with `--fix` / `--fix-unsafe`):

- `--ai` — enable AI (useful when `ai.command` is already configured in `.tally.toml`)
- `--acp-command "..."` — set the ACP agent command line (also enables AI)
- `--ai-timeout 90s` — override `ai.timeout`
- `--ai-max-input-bytes 262144` — override `ai.max-input-bytes`
- `--ai-redact-secrets=false` — override `ai.redact-secrets`

Important fix flags:

- `--fix` — apply safe fixes
- `--fix-unsafe` — also apply unsafe fixes (includes AI)
- `--fix-rule <rule>` — limit which rules are allowed to fix

Tip: If your agent command needs complex quoting, prefer `ai.command = ["arg1", "arg2", ...]` in `.tally.toml` rather than `--acp-command`.

## ACP Agents (Native vs Adapters)

In ACP terminology, **tally is an ACP client** and your chosen tool is an **ACP agent**.

Some tools implement ACP natively, others are wired in via adapters maintained by the Zed community.

### Native ACP agents

- Gemini CLI: <https://agentclientprotocol.com/agents/gemini-cli>
- OpenCode: <https://agentclientprotocol.com/agents/opencode>
- Kiro CLI: <https://agentclientprotocol.com/agents/kiro>
- Cline (CLI v2): <https://agentclientprotocol.com/agents/cline>
- GitHub Copilot CLI: <https://agentclientprotocol.com/agents/github-copilot>

### Zed-maintained adapters

- Claude Code: <https://agentclientprotocol.com/agents/claude-code> (adapter: <https://github.com/zed-industries/claude-code-acp>)
- OpenAI Codex CLI: <https://agentclientprotocol.com/agents/codex> (adapter: <https://github.com/zed-industries/codex-acp>)

For background on ACP and the registry, see:

- ACP docs: Agents: <https://agentclientprotocol.com/get-started/agents>
- Zed docs: External Agents: <https://zed.dev/docs/ai/external-agents>
- Zed blog: ACP Registry: <https://zed.dev/blog/acp-registry>

## Security, Privacy, and Predictability

AI fixes are powerful — and risky. tally deliberately adds guardrails:

- **Explicit opt-in**: AI is off unless you enable it.
- **Unsafe gating**: AI fixes are unsafe and require `--fix-unsafe`.
- **Minimal capabilities**: tally advertises no filesystem and no terminal capabilities via ACP.
- **Secret redaction**: prompts are best-effort redacted by default.
- **Strict output contract**: only `NO_CHANGE` or a full Dockerfile in a fenced code block.
- **Validation loop**: tally re-parses and re-lints proposed output before applying it.

One important note: ACP is a protocol, **not a sandbox**. If you run a local agent process that can access your machine, it can still do so
outside of ACP. Treat the agent like any other executable you run locally.

## Why This Is a Big Deal (Yes, We’re Proud)

ACP has been primarily discussed in the context of editors and “agentic IDE” workflows — but tally is applying it to a different problem:

**turning linter guidance into high-leverage, validated refactors.**

The core idea isn’t “ask an LLM to improve my Dockerfile”. It’s closer to:

- the linter detects a very specific, high-signal situation,
- it asks the agent to do **one narrow transformation** with a strict contract,
- then it **verifies** the output before applying anything.

That combination is what makes the result consistently better than a generic prompt like “Improve my Dockerfile”.

### Prompts as a product

Most people don’t get great results from an agent because they don’t know what to ask for. tally does.

When a rule triggers, tally can send a prompt that is:

- **concise**: no long conversations, just the task + the Dockerfile + minimal evidence
- **precise**: “convert to multi-stage” (not “optimize everything”)
- **bounded**: “don’t change unrelated parts unless required for the conversion”
- **machine-checkable**: the agent must output either `NO_CHANGE` or one full Dockerfile in a fenced code block

In other words, the prompt encodes the *best practice question* you would have asked if you were an expert at prompting and Dockerfiles.

### Validation beats vibes

Even a good model can produce plausible-but-wrong output. tally treats the agent’s response as an untrusted proposal and validates it:

- Parse the returned Dockerfile (syntax must be valid).
- Check invariants that matter for the transformation (e.g. “multi-stage” should actually produce multiple stages).
- Re-lint the proposed output and feed back any blocking issues.
- If the agent can’t produce a safe refactor, it must respond `NO_CHANGE` (tally skips the fix and continues linting).

This “heuristics → focused prompt → validation loop” is the key: you get the speed of rules and the flexibility of an agent, without turning your
lint run into a free-form AI chat session.

This is one of the first steps toward a new class of tooling where:

- linters don’t just point at problems — they can propose complete, reviewable changes,
- fixes can be as complex as the real-world Dockerfiles we ship,
- and users can “vibe” their way through improvements without sacrificing engineering rigor.
