# AI AutoFix via ACP: Command-Family Normalization

> Status: proposal
>
> Working name: `ato-fix`
>
> Pilot rule: `hadolint/DL4001`
>
> Broader target: reusable AI-assisted command-family normalization, later extensible to migrations such as `npm -> bun`
>
> Companion docs:
>
> - `design-docs/13-ai-autofix-acp.md`
> - `design-docs/19-ai-autofix-diff-contract.md`

## 1. Decision

Add a new ACP-powered AI AutoFix objective for command-family normalization.

The first objective should target `hadolint/DL4001`:

- detect mixed `curl` / `wget` usage as today,
- choose a preferred tool heuristically,
- send a tightly scoped ACP prompt for one command or one supported bounded pipeline,
- require **replacement text output for one exact window**,
- validate that replacement mechanically with tally's shell parser and command facts,
- splice it back into the file,
- and apply it only as an explicit unsafe fix.

This is not a general shell-command translator. It is a bounded AI repair path for cases where:

- detection is reliable,
- rewrite intent is clear,
- but deterministic translation is not credible.

This document refines the general ACP architecture from `13-ai-autofix-acp.md` for a narrower objective family and intentionally rejects the
patch-oriented contract proposed in `19-ai-autofix-diff-contract.md` for this class of fixes.

## 2. Why Not Diff

For this objective family, diff output is the wrong contract.

Tally already knows:

- which file is being edited,
- the exact line/column window to replace,
- the shell variant,
- the platform OS,
- and the extracted command facts.

That means the important question is not:

- "did the model produce a well-formed patch?"

It is:

- "did the model produce valid replacement text for this exact command window?"

So the right contract is:

- `NO_CHANGE`
- or exactly one fenced `text` block with replacement text for the declared window

Tally can compute a diff afterward for preview, logging, or UX. The model does not need to generate one.

## 3. Compressed Motivation

The hard part of `curl <-> wget` is not discovery. It is preserving behavior:

- output defaults differ,
- redirect behavior differs,
- retry behavior differs,
- recursive and timestamping modes differ,
- and surrounding shell structure may depend on side effects such as implicit filenames.

So the design split should be:

1. heuristics choose scope and gather evidence,
2. AI proposes a local rewrite,
3. tally validates the replacement mechanically,
4. tally rejects anything that does not satisfy exact shape-preservation rules.

The same pattern applies even more strongly to future families like `npm -> bun`.

### 3.1 External Evidence: `curlconverter`

The closest serious external reference is [`curlconverter/curlconverter`](https://github.com/curlconverter/curlconverter).

It is useful evidence, but for a narrower conclusion than "heuristic conversion is solved."

What `curlconverter` demonstrates credibly:

- shell-aware parsing is mandatory, not optional,
- conversion benefits from an intermediate normalized request model,
- target-specific support tables are healthier than pretending full parity,
- and lossy behavior must be surfaced explicitly through warnings.

The repository structure makes that clear:

- [`src/parse.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/parse.ts) parses shell input into
  structured `Request` objects,
- [`src/shell/tokenizer.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/shell/tokenizer.ts)
  handles Bash quoting, expansions, redirects, heredocs, and pipeline discovery,
- [`src/generators/wget.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/generators/wget.ts)
  emits a fresh `wget` command from the normalized request model,
- [`src/Warnings.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/Warnings.ts) records ignored or
  unsupported parts explicitly.

The important negative evidence is equally strong:

- the README states it knows about all `curl` arguments but that most are ignored,
- the `wget` generator is explicitly warning-driven rather than parity-driven,
- the converter preserves request intent better than shell topology,
- and several tested `curl -> wget` cases accept semantic drift that is fine for a transpiler but not acceptable for an auto-fix.

Concrete examples from the fixture corpus:

- [`curl -L http://localhost:28139`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/test/fixtures/curl_commands/get_follow_redirect.sh)
  becomes
  [`wget --output-document - http://localhost:28139`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/test/fixtures/wget/get_follow_redirect.sh),
  which drops the explicit redirect-following signal,
- [`curl 'http://localhost:28139' -x 'http://localhost:8080'`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/test/fixtures/curl_commands/get_proxy.sh)
  also becomes plain
  [`wget --output-document - http://localhost:28139`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/test/fixtures/wget/get_proxy.sh),
  while the generator only warns that Wget expects proxy configuration elsewhere,
- [`test/fixtures/curl_commands/pipelines.sh`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/test/fixtures/curl_commands/pipelines.sh)
  proves the parser understands pipelines, but there is no corresponding `wget` fixture showing pipeline-preserving shell rewrites,
- issue [#703](https://github.com/curlconverter/curlconverter/issues/703) shows a shell-parsing edge case around unquoted URLs with `&`, reinforcing
  that prompt scope should be a pre-isolated command window rather than arbitrary script.

So the practical lesson is:

- borrow the architecture,
- borrow the warning taxonomy mindset,
- borrow the quoting discipline,
- but do not borrow the acceptance model.

For tally, warnings are not enough. We need parser-backed acceptance rules that reject rewrites unless shell shape and statically observable behavior
stay within a narrow validated subset.

## 4. Goals and Non-Goals

### Goals

- Provide a credible AI auto-fix path for `DL4001`.
- Keep the AI context command-local.
- Make command validity checkable mechanically with the shell parser.
- Reuse tally's ACP resolver model.
- Generalize to later command families.

### Non-Goals

- A universal shell-command translator.
- Broad Dockerfile-aware prompting for this objective.
- Whole-file rewrites.
- Model-generated patches.
- Broad terminal or filesystem access.
- Warning-only acceptance of semantically lossy translations.

## 5. UX and Safety Contract

### CLI behavior

This should behave like the current unsafe AI fix path:

- `--fix --fix-unsafe`
- strongly recommended with `--fix-rule hadolint/DL4001`
- recommended rule config: `fix = "explicit"`

Suggested user-facing fix text:

- `AI AutoFix: normalize mixed curl/wget usage to one tool`

### Safety level

The fix must be:

- `NeedsResolve = true`
- `ResolverID = "ai-autofix"`
- `Safety = FixUnsafe`

### Output contract

For this objective, the model must output exactly one of:

- `NO_CHANGE`
- one fenced `text` block containing replacement text for the declared window only

No patch mode. No whole-file mode. No fallback to either.

## 6. Core Design

### 6.1 Scope

The model should operate on:

- one command, or
- one supported bounded pipeline / archive-extraction pattern

It should not be asked to reason about the Dockerfile as a whole.

Dockerfile context remains tally's concern for:

- locating the window,
- applying the replacement,
- and running post-apply validation.

### 6.2 Objective request

Add a new objective kind under `internal/ai/autofixdata`.

Recommended kind:

- `command-family-normalize`

Illustrative facts payload:

```json
{
  "family": "download-client",
  "ruleCode": "hadolint/DL4001",
  "platformOS": "linux",
  "shellVariant": "bash",
  "preferredTool": "curl",
  "preferredReason": "curl is installed in this stage and .curlrc is present",
  "window": {
    "startLine": 12,
    "endLine": 12,
    "startColumn": 4,
    "endColumn": 50,
    "originalText": "wget -q -O /tmp/file https://example.com/file",
    "validationMode": "single-command"
  },
  "candidate": {
    "sourceTool": "wget",
    "raw": "wget -q -O /tmp/file https://example.com/file",
    "urls": ["https://example.com/file"],
    "outputMode": "file",
    "outputPath": "/tmp/file"
  },
  "context": {
    "boundedPattern": "none"
  },
  "blockers": [],
  "relatedRuleSignals": ["tally/curl-should-follow-redirects"]
}
```

Key point: tally does the deterministic prep work.

The model gets:

- exact OS,
- exact shell,
- exact window text,
- exact target tool,
- and extracted command facts.

This is where tally should diverge from `curlconverter`:

- `curlconverter` normalizes into a request model and then re-emits a target command,
- tally should normalize into command facts and preserve shell shape as a first-class constraint.

That distinction matters for pipelines, output redirection, and archive extraction, where request equivalence alone is insufficient.

### 6.3 Prompt model

Do not frame the prompt as "you are editing a Dockerfile".

The prompt should frame the task as:

- one shell command in,
- one shell command out,
- one exact replacement window,
- one declared shell and OS,
- one declared target tool.

Illustrative prompt skeleton:

````text
You are rewriting one shell command to normalize one command family.

Task:
- Rewrite the command below so it uses only: curl
- Preserve behavior as closely as possible
- If you cannot do that safely, output exactly: NO_CHANGE

Strict rules:
- Only rewrite the provided command text
- Do not change text outside the declared replacement window
- Output a command that is valid for the declared OS and shell
- Do not introduce unrelated flags, wrappers, or shell constructs

Context:
- Rule: hadolint/DL4001
- Platform OS: linux
- Shell: bash
- Preferred tool: curl
- Reason: curl is installed in this stage and .curlrc is present
- Validation mode: single-command
- Optional bounded pattern: none

Input command:
```text
wget -q -O /tmp/file https://example.com/file
```

Output format:
- Either exactly NO_CHANGE
- Or exactly one ```text code block containing replacement text for this window only
````

### 6.4 Restricted tool introspection

For this objective family, broad terminal access should remain disabled.

Allowed exception:

- objective-scoped allowlisted help/version invocations for the target tool only

Examples:

- `curl --help`
- `curl -V`
- `wget --help`
- `wget --version`

Everything else remains disallowed:

- networked invocations
- test runs
- builds
- `docker` / `podman` / `buildctl`
- package manager commands
- shell wrappers
- file writes
- repo exploration

This is enough for flag spelling and local capability confirmation, without opening a large execution surface.

## 7. Validation Model

Validation should happen in two layers:

1. replacement-window acceptance
2. post-apply file validation

### 7.1 Replacement-window acceptance

Because tally already knows the exact replacement window, acceptance should be based on the replacement text itself.

For the DL4001 pilot, validate all of the following:

- output is either `NO_CHANGE` or one fenced replacement block
- replacement parses successfully for the declared shell variant
- replacement remains compatible with the declared platform OS
- validation mode is satisfied exactly:
  - `single-command`: exactly one command after parse
  - `bounded-pipeline`: same pipeline arity and same operator skeleton
- target command changes from the non-preferred tool to the preferred tool
- static URLs are preserved
- explicit output mode is preserved (`file` vs `stdout`)
- explicit output path is preserved when present
- no new shell metaprogramming is introduced

For bounded pipeline / archive mode, additionally validate when statically extractable:

- downstream command family is preserved
- extraction target is preserved
- extraction destination is preserved
- the non-target pipeline segment remains AST-identical or command-fact-identical after rewrite

### 7.2 What we can validate confidently

With the existing shell parser and helpers, the following are realistic confidence checks for POSIX shells:

- exact command count preservation
- exact pipeline arity preservation
- exact operator skeleton preservation
- presence and identity of static URLs
- explicit output file path
- stdout vs file mode
- tar extraction detection
- tar extraction destination
- non-target segment identity for bounded pipelines
- stdout-preserving download mode for pipe rewrites such as `curl ... | tar ...` -> `wget -O- ... | tar ...`

This is also where tally can be stricter than `curlconverter`:

- we can validate shell topology directly,
- we can reject conversions that only preserve approximate request semantics,
- and we can require downstream pipeline segments to stay structurally unchanged.

ShellCheck should be used as a secondary guard for POSIX shells, not as the primary acceptance gate.

### 7.3 Supported bounded pipeline modes in v1

Pipeline support should be explicit, not open-ended.

Recommended v1 allowlist:

- `download | tar-extract`
- `download | tar-extract | <nothing else>` only

Where:

- left segment is exactly one `curl` or `wget` download command
- right segment is exactly one `tar` extraction command
- there are no additional pipeline stages
- there is no shell metaprogramming around the pipeline

Examples that should be in scope:

- `curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt`
- `wget -q -O- https://example.com/app.tar.gz | tar -xz -C /opt`

Examples that should stay out of scope in v1:

- `curl ... | tee file | tar ...`
- `curl ... | sh`
- `curl ... | gunzip | tar ...`
- `curl ... | awk ...`
- any pipeline with more than two stages
- any pipeline where the non-download segment is not a statically understood extractor

### 7.4 What we should not trust in v1

Block or refuse when the command depends on semantics we cannot confidently validate:

- `curl -X`, `--data`, `--form`, uploads, custom auth-heavy flows
- `wget` recursive, mirror, or timestamping modes
- implicit filename behavior such as `curl -O`
- multi-URL transfers
- pipelines outside the explicit bounded-pipeline allowlist
- dynamic shell constructs hiding argv
- Windows / PowerShell / cmd rewrites without equivalent parser-backed invariants

These refusal rules are not theoretical. They follow directly from the `curlconverter` evidence that a broad `curl -> wget` converter eventually
falls back to warnings, ignored flags, or request-level approximations.

### 7.5 Post-apply validation

After splicing the replacement back into the file, tally should still run:

1. Dockerfile parse
2. objective-specific validation
3. re-lint with AI disabled and normal fix policy
4. objective-specific re-validation on normalized content

For DL4001 specifically, require:

- the scoped command now uses the preferred tool
- the scoped stage no longer mixes `curl` and `wget` for the targeted issue
- no newly introduced violation of `tally/curl-should-follow-redirects` when the result uses `curl`
- no parse errors

## 8. Resolver Changes

### 8.1 Output mode

Add a new output mode:

```go
const (
    OutputReplacement OutputMode = "replacement"
    OutputPatch       OutputMode = "patch"
    OutputDockerfile  OutputMode = "dockerfile"
)
```

For this objective family:

- `Primary = OutputReplacement`
- `AllowFallback = false`

### 8.2 Replacement window

The resolver should support objective-supplied replacement windows directly.

Illustrative shape:

```go
type ReplacementWindow struct {
    StartLine    int
    EndLine      int
    StartColumn  int
    EndColumn    int
    OriginalText string
}
```

Flow:

1. parse model output as replacement text
2. validate it against the shell/OS contract
3. splice it into the known window
4. compute a diff internally if needed for UX/debugging

## 9. DL4001 Pilot Policy

Offer the AI fix only when:

- exactly one command or bounded pipeline is chosen as scope
- a preferred tool is selected
- the candidate command is statically identifiable
- shell variant is known and parseable
- there are no hard blockers

In v1, do not let the AI:

- change package installation commands
- add or remove config files
- rewrite multiple stages
- rewrite text outside the single replacement window

That means the first ACP objective is:

- normalize one command invocation
- not environment setup

## 10. Generalization

This design should generalize through family adapters, not through one giant prompt.

Examples of later families:

- `npm -> bun`
- `npm -> pnpm`
- `wget/curl -> ADD <url>` in tightly bounded cases

But each family should provide its own:

- extracted facts
- validation mode
- exact invariants
- refusal rules

`curlconverter` is another useful signal here: support tables and target-specific emitters scale better than a universal translator prompt. Tally
should keep that idea, but replace "best-effort emit + warnings" with "AI proposal + hard validator + refusal."

## 11. Implementation Plan

### Phase 1: scaffolding

- add new objective kind
- add `OutputReplacement`
- add replacement-window parsing and splice support in the resolver
- add objective-scoped capability policy for restricted tool introspection

### Phase 2: DL4001 evidence pack

- refactor `DL4001` detection to emit structured AI facts
- add preferred tool selection
- add command-local shell/OS extraction
- add bounded pipeline/archive classification
- add explicit pipeline-mode classification for `download | tar-extract`
- add blocker classification

### Phase 3: validation

- replacement-window validator for exact shape preservation
- shell-parser validation of rewritten command text
- secondary ShellCheck guard for POSIX shells
- bounded-pipeline validator for left/right segment identity and stdout-preserving semantics
- post-apply DL4001 invariants

### Phase 4: tests

- unit tests for preferred tool policy
- prompt snapshot tests
- replacement-output parsing tests
- validation tests for single-command and bounded-pipeline modes
- integration tests for `curl | tar` -> `wget -O- | tar`
- resolver tests for `NO_CHANGE`, malformed replacement output, invalid-shape replacement, and successful replacement
- integration fixtures for representative Dockerfiles

## 12. Recommendation

Proceed, but with this contract:

- command-local prompt context only
- explicit shell and OS context
- replacement-window output, not diff
- parser-first validation
- optional allowlisted help/version introspection for the target tool only
- unsafe explicit application only

That is cleaner than diff mode for this objective and matches what tally can already validate confidently.

## Appendix A: End-to-End Pipe Example

This appendix demonstrates the proposed workflow for a concrete bounded pipeline case.

### A.1 Original Dockerfile snippet

```Dockerfile
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y wget ca-certificates
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app
```

Assume `DL4001` fires because the stage already standardized on `wget`.

### A.2 Tally detection and fact extraction

Tally detects:

- preferred tool: `wget`
- reason: `wget` is installed in the stage; `curl` is the outlier
- shell variant: `bash`
- platform OS: `linux`
- validation mode: `bounded-pipeline`

Illustrative extracted facts:

```json
{
  "family": "download-client",
  "ruleCode": "hadolint/DL4001",
  "platformOS": "linux",
  "shellVariant": "bash",
  "preferredTool": "wget",
  "preferredReason": "wget is installed in this stage",
  "window": {
    "startLine": 3,
    "endLine": 3,
    "startColumn": 4,
    "endColumn": 63,
    "originalText": "curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app",
    "validationMode": "bounded-pipeline"
  },
  "candidate": {
    "sourceTool": "curl",
    "raw": "curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app",
    "urls": ["https://example.com/app.tar.gz"],
    "outputMode": "stdout"
  },
  "context": {
    "boundedPattern": "download|tar-extract",
    "pipelineArity": 2,
    "downstreamCommand": "tar",
    "tarExtract": true,
    "tarDestination": "/opt/app"
  }
}
```

### A.3 Prompt sent to the model

Tally sends a command-local prompt, not a Dockerfile-refactor prompt.

Illustrative content:

````text
You are rewriting one shell command to normalize one command family.

Task:
- Rewrite the command below so it uses only: wget
- Preserve behavior as closely as possible
- If you cannot do that safely, output exactly: NO_CHANGE

Strict rules:
- Only rewrite the provided command text
- Do not change text outside the declared replacement window
- Output a command that is valid for the declared OS and shell
- Preserve the pipeline shape and the downstream tar extraction behavior

Context:
- Rule: hadolint/DL4001
- Platform OS: linux
- Shell: bash
- Preferred tool: wget
- Validation mode: bounded-pipeline
- Bounded pattern: download|tar-extract
- Pipeline arity: 2
- Downstream command: tar
- Tar destination: /opt/app

Input command:
```text
curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app
```

Output format:
- Either exactly NO_CHANGE
- Or exactly one ```text code block containing replacement text for this window only
````

### A.4 Model output

Expected successful output:

```text
wget -q -O- https://example.com/app.tar.gz | tar -xz -C /opt/app
```

### A.5 Mechanical validation

Tally then validates the replacement without trusting the model's reasoning:

1. Parse the replacement with the Bash shell parser.
2. Assert `bounded-pipeline` shape:
   - pipeline arity is still 2
   - operator skeleton is still a single `|`
3. Assert left segment is now `wget`.
4. Assert right segment is still `tar` extraction.
5. Assert URL is unchanged.
6. Assert output mode remains `stdout`.
   - original `curl` piped to stdout
   - rewritten `wget` must therefore use `-O-` or equivalent stdout form
7. Assert tar destination remains `/opt/app`.
8. Optionally run ShellCheck as a secondary guard.

If any of those fail, the resolver rejects the output and either retries with blocking issues or returns `NO_CHANGE`.

### A.6 Splice back into the file

After successful validation, tally replaces only the known window:

Before:

```Dockerfile
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app
```

After:

```Dockerfile
RUN wget -q -O- https://example.com/app.tar.gz | tar -xz -C /opt/app
```

If the CLI or editor wants a diff preview, tally computes it after the splice. The model does not generate that diff.

### A.7 Post-apply checks

Tally then runs normal post-apply validation:

- Dockerfile parse succeeds
- scoped command now uses `wget`
- targeted stage is no longer mixed for the relevant command-family issue
- no new violation is introduced by the rewrite

This is the intended end-to-end workflow for bounded pipe cases in v1.

## 13. Sources

### Local tally sources

- [`internal/rules/hadolint/dl4001.go`](../internal/rules/hadolint/dl4001.go)
- [`internal/rules/hadolint/dl4001_test.go`](../internal/rules/hadolint/dl4001_test.go)
- [`internal/shell/command.go`](../internal/shell/command.go)
- [`internal/shell/chain.go`](../internal/shell/chain.go)
- [`internal/shell/archive.go`](../internal/shell/archive.go)
- [`internal/facts/facts.go`](../internal/facts/facts.go)
- [`internal/fix/fixer.go`](../internal/fix/fixer.go)
- [`internal/ai/autofix/resolver.go`](../internal/ai/autofix/resolver.go)
- [`internal/ai/autofixdata/objective.go`](../internal/ai/autofixdata/objective.go)
- [`internal/ai/autofixdata/prompt.go`](../internal/ai/autofixdata/prompt.go)
- [`design-docs/13-ai-autofix-acp.md`](../design-docs/13-ai-autofix-acp.md)

### External references

- [curl man page](https://curl.se/docs/manpage.html)
- [GNU Wget manual](https://www.gnu.org/software/wget/manual/wget.html)
- [`curlconverter` README](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/README.md)
- [`curlconverter` `src/parse.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/parse.ts)
- [`curlconverter` `src/shell/tokenizer.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/shell/tokenizer.ts)
- [`curlconverter` `src/generators/wget.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/generators/wget.ts)
- [`curlconverter` `src/Warnings.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/Warnings.ts)
- [`curlconverter` issue #703](https://github.com/curlconverter/curlconverter/issues/703)
