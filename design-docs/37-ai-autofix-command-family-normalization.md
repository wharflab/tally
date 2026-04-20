# Command-Family Normalization: Semantic Lift/Lower With ACP Fallback

> Status: proposal (revision 2), with DL4001 MVP partially implemented
>
> Working name: `ato-fix`
>
> Pilot rule: `hadolint/DL4001`
>
> Broader target: reusable command-family normalization across families such as `curl/wget/iwr` and `npm/bun`
>
> Companion docs:
>
> - `design-docs/13-ai-autofix-acp.md`
> - `design-docs/19-ai-autofix-diff-contract.md`

For command-family normalization, this document takes precedence over the diff-output recommendation in doc 19: replacement-window output is the
required contract for this flow, and diff output is intentionally out of scope here.

## 1. Decision

The primary fix path for command-family normalization should be deterministic semantic transpilation, not ACP.

For one offending shell command:

1. parse the shell structure
2. recognize the command family
3. lift the source command into a family-specific abstract operation
4. lower that operation into the preferred target tool
5. validate Dockerfile-relevant equivalence mechanically
6. emit a heuristic unsafe fix if validation succeeds
7. fall back to ACP only if lift, lower, or validation fails

The lift step should not be implemented separately inside each rule. It should run once per file as reusable derived analysis, so rules can consume
family IR for detection, diagnostics, heuristics, and autofix without rebuilding the same interpretation logic repeatedly.

This is the key revision from the prior proposal. The earlier conclusion, "fully heuristic conversion is too hard, therefore ACP should be primary,"
was too coarse. What is not credible is broad flag-to-flag translation. What is credible is outcome-oriented semantic translation inside explicitly
bounded command families.

## 2. Why This Is Workable

The important shift is to stop treating commands as bags of flags and start treating them as descriptions of operations.

Examples:

- `curl -fsSL https://example.com/app.tgz | tar -xz -C /opt`
  The operation is not "a sequence of curl flags." The operation is "perform an HTTP transfer and feed the response body into `tar`."

- `curl -o /tmp/app.tgz https://example.com/app.tgz`
  The operation is "materialize one HTTP response body into `/tmp/app.tgz`."

- `npm install express`
  The operation is "mutate Node dependency state to add `express`."

- `npm config set foo bar`
  This is not the kind of operation this concept should model. It mutates tool-local configuration, not a portable package-management operation.

If a source command can be represented as one of these operations, and the target tool can realize the same operation, then heuristic conversion is
credible. If not, it should stop and fall back to ACP.

## 3. Design Goals and Non-Goals

### 3.1 Goals

- Handle the common Dockerfile cases with deterministic unsafe fixes.
- Keep the architecture reusable across command families.
- Build command-family IR once per file and share it read-only across rules.
- Separate source parsing from target serialization.
- Make blockers explicit instead of guessing through them.
- Preserve provenance back to the Dockerfile lines and files that influenced effective behavior.
- Validate outcome equivalence using shell parsing and family-specific checks.

### 3.2 Non-goals

- Exact flag symmetry between tools.
- Exact log formatting parity.
- Tool-self-configuration commands such as `npm config set ...` or other tool-specific control-plane mutations.
- Arbitrary shell-program rewriting.
- Whole-instruction or whole-file diff generation for this class of fix.

## 4. What To Learn From `curlconverter`

`curlconverter` is the closest useful reference, but it should be copied selectively.

At the pinned commit used in this document:

- [`src/shell/Parser.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/shell/Parser.ts) wires
  `tree-sitter` with `tree-sitter-bash`
- [`src/curl/opts.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/curl/opts.ts) lifts parsed
  argv into `GlobalConfig` and `OperationConfig`
- [`src/Request.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/Request.ts) normalizes that into
  `Request` and `RequestUrl`
- [`src/generators/wget.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/generators/wget.ts)
  lowers the normalized request model into `wget`, with many warnings when `wget` cannot preserve curl behavior

That gives tally the right architectural lesson:

- parse shell syntax with a real parser
- lift concrete tool syntax into a normalized internal model
- lower through target-specific serializers
- use target capability boundaries explicitly

But it also shows what tally must do differently.

`curlconverter` is primarily request-oriented. Tally needs to be build-outcome-oriented. For Dockerfile fixes, preserving the request alone is not
enough. The model must also preserve:

- where the response body goes
- whether stdout purity matters
- whether shell topology changes
- whether filesystem state changes
- whether package/install state changes
- whether contextual config inputs affect effective behavior

In other words, tally should borrow the lift/lower architecture, but not the warning-only acceptance model.

## 5. Core Concept: Operation Families

There should not be one universal IR for all commands. There should be small family-specific IRs built on a shared core.

Examples of families:

- `http-transfer`
- `node-package-management`
- later: `python-package-management`, `archive-extraction`

The central question for a family adapter is:

> Can this command be represented as one operation from this family without losing Dockerfile-relevant meaning?

If yes, it is liftable. If no, it is blocked.

Shell or platform variants do not define new families on their own. They define additional parsers, serializers, and validation context for the
same family.

Examples:

- PowerShell `Invoke-WebRequest` and POSIX `curl` are both tools for the same `http-transfer` family
- `npm`, `bun`, and later `pnpm` are tools for the same `node-package-management` family

The family describes the operation space. Tool adapters describe how a specific shell/tool spelling maps into or out of that family.

### 5.1 Shared core concepts

Illustrative shared types:

```go
type ValueKind string

const (
    ValueLiteral       ValueKind = "literal"
    ValueShellFragment ValueKind = "shell-fragment"
    ValueOpaque        ValueKind = "opaque"
)

type Value struct {
    Raw  string
    Kind ValueKind
}

type OutcomeSurface string

const (
    SurfaceFilesystem  OutcomeSurface = "filesystem"
    SurfaceStream      OutcomeSurface = "stream"
    SurfaceExitStatus  OutcomeSurface = "exit-status"
    SurfaceConfigState OutcomeSurface = "config-state"
    SurfacePkgState    OutcomeSurface = "package-state"
)

type OutputTopology string

const (
    TopologyFileSink         OutputTopology = "file-sink"
    TopologyStdoutPipe       OutputTopology = "stdout-pipe"
    TopologyStdoutUncaptured OutputTopology = "stdout-uncaptured"
    TopologyShellCapture     OutputTopology = "shell-capture"
    TopologyComplexRedirect  OutputTopology = "complex-redirect"
)
```

`ValueKind` matters because many source commands contain shell interpolation. The system does not need every value to be literal. It needs to know
whether the value is safe to preserve textually, only partially understandable, or too opaque for deterministic conversion.

### 5.2 Lift and lower results

Illustrative decision types:

```go
type Blocker struct {
    Code       string
    Reason     string
    Surfaces   []OutcomeSurface
    Compound   bool // true when the blocker comes from an unsafe combination, not one isolated feature
}

type Difference struct {
    Code     string
    Reason   string
    Severity string // "log-only", "cosmetic", "unsafe"
}

type LiftStatus string

const (
    Liftable        LiftStatus = "liftable"
    PartiallyLifted LiftStatus = "partially-lifted"
    NotLiftable     LiftStatus = "not-liftable"
)

type LiftDecision[T any] struct {
    Status          LiftStatus
    Operation       *T
    HardBlockers    []Blocker
    SoftDifferences []Difference
}

type LowerDecision struct {
    ReplacementText string
    HardBlockers    []Blocker
    SoftDifferences []Difference
}
```

`Compound` does not require different control flow from consumers. It exists so diagnostics, corpus analysis, and ACP payloads can distinguish
single hard blockers from combinations that only become unsafe together.

This makes the contract explicit:

- `liftable` means "the source command can be represented as a bounded family operation"
- `partially-lifted` means "enough structure was recognized to help ACP, but not enough for deterministic conversion"
- `not-liftable` means "do not attempt deterministic conversion"

### 5.3 Adapter contract

Illustrative interface:

```go
type CommandFamilyAdapter interface {
    Family() string
    Recognizes(cmd shell.CommandInfo) bool
    Lift(cmd shell.CommandInfo, ctx ShellContext) LiftDecision[any]
    Lower(op any, targetTool string, ctx LowerContext) LowerDecision
    Validate(sourceOp any, replacement string, ctx ValidationContext) []Blocker
}
```

The exact Go shape is not important. The separation of responsibilities is. In real implementation, rule call sites should not traffic in raw `any`
if a sealed union or family-typed wrapper can carry the operation more safely; `any` is only shorthand in this document.

### 5.4 Placement and lifecycle: facts layer, not per-rule

This IR should be built once per file in the facts layer, not rebuilt independently by each rule.

That matches the repository's current architecture:

- the semantic model is the source of truth for stage inheritance, effective shell, stage env, OS, and package-manager signals
- the facts layer already projects that state onto concrete `RUN` instructions
- rules already consume `FileFacts` and `RunFacts` as shared read-only derived analysis
- like the rest of `FileFacts` / `RunFacts`, command-operation facts should be immutable after construction and safe for concurrent rule reads

The recommended split is:

- semantic model stays responsible for foundational stage semantics
- facts layer consumes semantic state plus shell parsing plus observable-file state
- command-family adapters run during `FileFacts` / `RunFacts` construction
- rules consume lifted operations from facts instead of reparsing commands

This is important beyond autofix. The same lifted operation should be reusable by rules that only need effective behavior, for example:

- `curl` or `wget` policy rules that reason about redirects, retries, or progress output
- package-manager rules that inject `-y`, `--no-cache`, or equivalent policy flags
- config rules that check whether behavior is already implied by `.curlrc`, `wgetrc`, `.npmrc`, or environment

Illustrative direction:

```go
type CommandOperationFact struct {
    Family       string
    CommandIndex int
    Topology     OutputTopology
    LiftStatus   LiftStatus
    Operation    any
    Provenance   OperationProvenance
    HardBlockers []Blocker
}

type RunFacts struct {
    // existing fields...
    CommandOperationFacts []CommandOperationFact
}
```

Rules should read these facts. They should not author or mutate them.

The illustrative `Operation any` field should likewise become a typed wrapper or sealed union before implementation so rules do not need ad hoc
type assertions.

### 5.5 Context-aware lift inputs and provenance

Family lifting must have access to prior Dockerfile context, not just raw argv.

At minimum, the lift context should be able to read:

- effective shell and shell variant
- effective environment and env bindings
- stage OS and package-manager signals
- observable in-image files and their writer lines
- build-context sources when configuration files came from `COPY` / `ADD`
- command source text and location via `SourceMap`

This is necessary because effective behavior may come from configuration outside the command itself.

Examples:

- `curl` semantics can be influenced by `${CURL_HOME}/.curlrc`, `/root/.curlrc`, or proxy-related env
- `wget` semantics can be influenced by `WGETRC`, `/etc/wgetrc`, or `~/.wgetrc`
- `npm` semantics can be influenced by `.npmrc`, `NPM_CONFIG_*`, or secret-mounted config paths

The operation fact should therefore include provenance, not just a normalized operation.

Illustrative shape:

```go
type SourceRefKind string

const (
    SourceRefCommand        SourceRefKind = "command"
    SourceRefEnv            SourceRefKind = "env"
    SourceRefObservableFile SourceRefKind = "observable-file"
    SourceRefBuildContext   SourceRefKind = "build-context"
)

type SourceRef struct {
    Kind      SourceRefKind
    Line      int
    Location  []parser.Range
    Key       string
    Path      string
    Detail    string
}

type OperationProvenance struct {
    PrimaryCommand SourceRef
    Related        []SourceRef
}
```

The intent is not to burden every rule with location plumbing. The intent is to let one lift step resolve behavior and preserve where it came from.

That provenance is useful for:

- explaining why a command was or was not considered representable
- linking blockers back to specific `ENV`, `COPY`, `ADD`, or `RUN` lines
- letting future fixes or diagnostics update the right config source instead of guessing
- making ACP payloads explain the relevant context without dumping the whole Dockerfile

### 5.6 Current in-tree beneficiaries

This architecture is not justified only by future normalization work. Several rules already implement slices of the same reasoning and would become
simpler or more consistent if they consumed shared family IR instead of reconstructing behavior ad hoc.

Immediate `http-transfer` beneficiaries:

- [`internal/rules/tally/curl_should_follow_redirects.go`](../internal/rules/tally/curl_should_follow_redirects.go)
  This rule currently decides from raw `curl` flags and URL heuristics whether redirects should be followed, including request-method exceptions,
  scheme filtering, and IP-only targets. A shared `HTTPTransferOperation` could expose effective redirect policy, request method, sink, and
  URL/provenance once, instead of each rule reading raw `curl` syntax.

- [`internal/rules/hadolint/dl3047.go`](../internal/rules/hadolint/dl3047.go)
  This rule currently re-derives wget observability semantics from raw flags such as `--progress`, `-q`, `-nv`, and output-log redirection.
  `HTTPTransferOperation.Observability` plus output-topology facts would let it reason in terms of effective progress/log behavior rather than
  individual `wget` spellings.

- [`internal/rules/tally/prefer_curl_config.go`](../internal/rules/tally/prefer_curl_config.go)
  This rule currently mixes `InstallCommands`, `CommandInfos`, and stage observable-file checks to decide whether curl is installed, invoked, or
  already configured. A shared HTTP/tool fact with provenance could expose curl presence, effective config inputs, and whether retry behavior is
  already provided by observable `.curlrc` state.

- [`internal/rules/tally/prefer_wget_config.go`](../internal/rules/tally/prefer_wget_config.go)
  Same pattern as curl config. The rule currently infers wget usage from raw command/install detection and checks `WGETRC`-style files separately.
  A shared HTTP/tool fact could centralize effective wget usage and config provenance.

- [`internal/rules/tally/prefer_add_unpack.go`](../internal/rules/tally/prefer_add_unpack.go)
  This rule currently recognizes download-and-extract flows with direct calls to shell helpers like `DownloadOutputFile`, URL/archive heuristics,
  and tar detection. `HTTPTransferOperation` plus output topology and a companion archive-extract fact would remove much of this bespoke
  detection.

- [`internal/rules/hadolint/dl4001.go`](../internal/rules/hadolint/dl4001.go)
  Today this rule only knows "wget appears" or "curl appears." The proposed normalization design is effectively a richer successor: it needs
  family-aware lift/lower/validate for the concrete conflicting command, not just command-name presence.

Current package-manager rules that point in the same direction:

- [`internal/rules/hadolint/dl3014.go`](../internal/rules/hadolint/dl3014.go)
- [`internal/rules/hadolint/dl3030.go`](../internal/rules/hadolint/dl3030.go)
- [`internal/rules/hadolint/dl3034.go`](../internal/rules/hadolint/dl3034.go)
- [`internal/rules/hadolint/dl3038.go`](../internal/rules/hadolint/dl3038.go)
- [`internal/rules/runcheck/runcheck.go`](../internal/rules/runcheck/runcheck.go)

These rules already share a generic "command + subcommand + required flag" framework for non-interactive package-manager installs. A richer
package-operation fact would not replace all of that immediately, but it could centralize:

- which commands are install-like operations
- whether the operation is interactive by default
- which token span is the correct insertion anchor
- manager-specific subcommand aliases and effective mode

That would reduce the amount of per-manager flag interpretation spread across individual rules.

Rules already consuming partial package facts that a richer operation IR could subsume or extend:

- [`internal/rules/tally/sort_packages.go`](../internal/rules/tally/sort_packages.go)
  This rule already consumes `RunFacts.InstallCommands`. A package-operation fact could evolve from that same substrate instead of creating a
  second parallel install-command model.

- [`internal/rules/tally/prefer_package_cache_mounts.go`](../internal/rules/tally/prefer_package_cache_mounts.go)
  This rule currently combines `CommandInfos`, cache-path env overrides, and cleanup heuristics to infer package-manager cache behavior. A richer
  package-operation fact could expose effective cache directories and cleanup semantics directly.

- [`internal/rules/tally/prefer_multi_stage_build.go`](../internal/rules/tally/prefer_multi_stage_build.go)
  This rule already scores stages using `RunFacts.InstallCommands` as signals. Richer package-operation facts would give it better structured
  evidence for "this stage installs build tooling" without regex-style fallback.

Adjacent but intentionally separate:

- [`internal/rules/hadolint/dl3027.go`](../internal/rules/hadolint/dl3027.go)
  This is a real command-family rewrite already in-tree: `apt` to `apt-get` or `apt-cache`. It is a useful signal that command-family
  normalization is broader than `curl`/`wget`, but it belongs to a future `apt` family, not to the initial `http-transfer` family.

- [`internal/rules/tally/require_secret_mounts.go`](../internal/rules/tally/require_secret_mounts.go)
  This rule currently matches raw command names against configured secret-mount requirements. It could eventually benefit from richer
  package-operation provenance, but it does not need the first HTTP/package IR rollout.

The duplication is already visible in current helpers:

- [`internal/shell/archive.go`](../internal/shell/archive.go)
- [`internal/shell/install_packages.go`](../internal/shell/install_packages.go)
- [`internal/rules/runcheck/runcheck.go`](../internal/rules/runcheck/runcheck.go)

Those helpers are useful and should remain, but they are also evidence that tally already has multiple partial interpretations of the same command
families. The proposed IR is a way to unify those interpretations at the facts layer rather than letting each rule grow its own variant.

## 6. Dockerfile-Relevant Observation Model

This design is intentionally Dockerfile-specific.

Command equivalence should be judged by what a `RUN` instruction makes observable to the image layer or to downstream commands, not by whether the
target CLI looks similar.

### 6.1 What matters

| Concern | Relevant | Why |
|---|---|---|
| files created or modified | yes | persists in the image layer |
| bytes sent to a downstream pipeline command | yes | changes behavior of the next command |
| stdout captured by shell constructs | yes | becomes data, not logs |
| exit behavior | yes | determines whether the build fails |
| package graph / lockfile / manifest state | yes | persists in the image |
| contextual config inputs such as env or observable rc files | yes | can change effective command behavior |
| tool-self-configuration commands | no for this concept | these are tool-specific control-plane operations, not portable command-family operations |
| uncaptured stdout body | usually no | generally becomes build log only |
| progress bars / verbosity / styling | usually no | log-only unless consumed structurally |

This is why semantic transpilation is practical. Many tool differences are log-only from the Dockerfile point of view.

### 6.2 Output topology classes

For shell-form `RUN`, the system should classify the source command into one of these topologies:

- `file-sink`
  Example: `curl -o /tmp/file.tgz ...`

- `stdout-pipe`
  Example: `curl ... | tar -xz -C /opt`

- `stdout-uncaptured`
  Example: `curl ...`

- `shell-capture`
  Example: `VAR=$(curl ...)`

- `complex-redirect`
  Example: `curl ... 2>&1 | grep foo`

This topology must become part of the family operation or of the validation context. `curlconverter` does not preserve enough of this on its own,
which is why tally needs a slightly richer model.

### 6.3 Compound blockers

Single flags are not the only reason to refuse deterministic conversion. Some combinations are only unsafe in context.

Examples:

- verbose output plus `stdout-pipe`
  Extra chatter can corrupt a data stream.

- remote-name destination plus downstream file assumption
  The target may pick a different filename or require an explicit filename.

- env assignment or command substitution plus any log-affecting option
  Now stdout content is data, not disposable output.

- dynamic destination path plus target-only lowering behavior
  The system may know the operation family but still not be able to prove equivalent filesystem state.

These should be represented as explicit blockers, not left as vague caution text.

## 7. HTTP Transfer Family

This is the first family that should power `hadolint/DL4001`.

### 7.1 Family scope

The family covers commands whose essential operation is:

- perform one HTTP or HTTPS transfer
- optionally apply bounded request semantics such as headers, auth, method, redirects, retries, or body
- deliver the response body to one Dockerfile-relevant sink

Initial tools:

- `curl`
- `wget`
- later: PowerShell `Invoke-WebRequest` / `iwr`

It does not cover every `curl` feature. It covers the subset that can be normalized as a transfer operation with a bounded sink.

PowerShell does not need a separate operation family here. It needs:

- a PowerShell-aware parser/lifter into `HTTPTransferOperation`
- a PowerShell-aware serializer from `HTTPTransferOperation`
- validation rules that respect the declared shell and platform context

That keeps one shared transfer IR while still allowing shell-specific syntax and behavior at the adapter layer.

### 7.2 IR shape

Illustrative IR:

```go
type HTTPBodySourceKind string

const (
    BodyNone      HTTPBodySourceKind = "none"
    BodyLiteral   HTTPBodySourceKind = "literal"
    BodyFile      HTTPBodySourceKind = "file"
    BodyStdin     HTTPBodySourceKind = "stdin"
)

type HTTPResponseSinkKind string

const (
    SinkFile             HTTPResponseSinkKind = "file"
    SinkStdoutPipe       HTTPResponseSinkKind = "stdout-pipe"
    SinkStdoutUncaptured HTTPResponseSinkKind = "stdout-uncaptured"
)

type HTTPFailurePolicy struct {
    FailOnTransportError bool
    FailOnHTTPStatus     bool
}

type HTTPDownstreamHint string

const (
    HTTPDownstreamUnknown    HTTPDownstreamHint = "unknown"
    HTTPDownstreamTarExtract HTTPDownstreamHint = "tar-extract"
)

type HTTPRequestSpec struct {
    Scheme         string
    Method         string
    URL            Value
    Headers        []HTTPHeader
    BodyKind       HTTPBodySourceKind
    BodyValue      *Value
    Auth           *HTTPAuth
    RedirectPolicy string
    Compression    string
    TimeoutSeconds *float64
    RetryPolicy    *HTTPRetryPolicy
    TLS            *HTTPTLSOptions
    FailurePolicy  HTTPFailurePolicy
}

type HTTPResponseSink struct {
    Kind           HTTPResponseSinkKind
    FilePath       *Value
    DownstreamHint HTTPDownstreamHint
}

type HTTPTransferSideEffects struct {
    CookieReadFiles  []Value
    CookieWriteFile  *Value
    RemoteName       bool
}

type HTTPObservability struct {
    Quiet            bool
    Verbose          bool
    OutputPurityNeed bool
}

type HTTPTransferOperation struct {
    Request      HTTPRequestSpec
    ResponseSink HTTPResponseSink
    SideEffects  HTTPTransferSideEffects
    Observability HTTPObservability
}
```

Validators should treat unknown downstream hints conservatively and fall back rather than guessing.

The important design choice is that this is not merely an HTTP request model. It is a Dockerfile-relevant transfer model. That is the difference
from using `curlconverter`'s `Request` model directly.

The operation fields should describe effective behavior after merging command arguments with context-derived config, not just literal argv.

### 7.3 Context-aware effective behavior

For HTTP tools, effective behavior may come from prior Dockerfile state rather than the command line alone.

The lifter should therefore consult `RunFacts`, `StageFacts`, and semantic context when resolving the operation.

Examples:

- a `curl` command may inherit redirect or retry behavior from `${CURL_HOME}/.curlrc` or `/root/.curlrc`
- a `wget` command may inherit retry or output behavior from `WGETRC`, `/etc/wgetrc`, or `~/.wgetrc`
- env-based proxy or timeout settings may materially affect whether the transfer is representable

That means the lift step should produce:

- effective operation fields
- provenance for the env bindings or observable files that supplied those fields
- blockers when configuration exists but cannot be parsed confidently enough to derive effective behavior

Example:

- if the command omits `-L`, but an observable `.curlrc` enables location-following, the lifted operation should still record effective redirect
  behavior, and provenance should point to the `.curlrc` writer line and any `CURL_HOME` env binding that resolved the path

### 7.4 What the lifter decides

The `curl` lifter should not try to "convert flags." It should decide whether the command is representable as `HTTPTransferOperation`.

Questions it should answer:

- is there exactly one URL?
- is the scheme within supported scope?
- what is the request method?
- what body semantics exist?
- where does the response body go?
- does stdout purity matter?
- are there response-side side effects such as cookie jars or remote-name behavior?
- can the failure semantics be made explicit?

### 7.5 Liftability rules for v1

The initial deterministic subset for `DL4001` should be deliberately narrow but useful:

- one URL only
- `http` or `https`
- explicit sink:
  - explicit file path
  - stdout pipe
  - uncaptured stdout
- simple `GET` or `HEAD`
- no command substitution
- no env assignment from command output
- no merged file descriptors
- no cookie jar or other session mutation
- no recursive or mirror behavior
- no remote-name destination
- no protocol-specific features outside the target capability table

This already covers the common Dockerfile download cases:

- `curl -o /tmp/file.tgz URL`
- `curl -fsSL URL | tar -xz -C /path`
- `curl -fsS URL`

### 7.6 What counts as representable

For the HTTP family, "representable" should mean:

- the request semantics that materially affect output can be captured
- the sink can be captured precisely
- any side state that matters is either captured or absent
- any context-derived config that materially affects behavior is either resolved into the operation or treated as a blocker
- any log-only differences are isolated from data-bearing streams

Representable does not mean "all curl flags have been modeled." It means "the source command's Dockerfile-relevant behavior fits the family
operation."

### 7.7 Target capability tables

Lowering must use target capability tables, not flag mapping tables.

Illustrative capability shape:

```go
type HTTPTargetCapabilities struct {
    Schemes            map[string]bool
    Methods            map[string]bool
    OutputFile         bool
    OutputStdout       bool
    CustomHeaders      bool
    RequestBody        bool
    FollowRedirects    bool
    DisableRedirects   bool
    FailOnHTTPStatus   bool
    RetryControl       bool
    TLSOptions         bool
    CookieRead         bool
    CookieWrite        bool
    RemoteName         bool
}
```

Examples:

- if the lifted sink is `stdout-pipe`, the `wget` serializer may emit `-O-` even if the source command did not literally specify stdout
- if redirects are followed by default in the target and that matches the operation, the serializer may omit an explicit flag
- if the operation requires `FailOnHTTPStatus=true` and the target cannot preserve that contract, deterministic lowering must fail

This is the correct level of abstraction. The serializer chooses the target spelling that realizes the operation. It does not mirror the source
syntax.

### 7.8 Validation contract

Validation should parse the replacement command again and check family-specific invariants.

For the HTTP family, validation should assert:

- for a simple command window, the replacement still contains exactly one command
- for a supported pipeline window, pipeline segment count remains equal
- exactly one relevant transfer command still exists
- the URL is preserved as the same literal or as the same shell fragment class
- the response sink class is preserved
- the output file path is preserved exactly when sink is `file`
- the pipeline shape is preserved when sink is `stdout-pipe`
- all non-target pipeline segments that are part of the supported pattern remain semantically unchanged
- any downstream extractor semantics that we explicitly support remain preserved
- any material behavior previously supplied by env or observable config is preserved explicitly or proven to remain true in the target context
- no new file writes are introduced
- no ignored blocker from the target capability table slipped through

For POSIX-family shells, ShellCheck can run as a secondary guard, but the family validator remains the primary acceptance boundary.

This validator is the real safety boundary for heuristic fixes.

### 7.9 Hard blockers

Examples of HTTP-family hard blockers:

- multiple URLs
- unsupported scheme
- recursive or mirror semantics
- cookie jar read or write in v1
- remote-name or implicit filename behavior
- protocol constraints unsupported by target capabilities
- config-driven behavior that materially affects the transfer but cannot be resolved from env or observable files confidently enough
- command substitution or env capture
- complex fd redirection
- multipart or upload semantics outside target support

### 7.10 Soft differences

Allowed soft differences are limited to log-only behavior:

- progress meter shape
- verbosity formatting
- omitted explicit flags when target defaults already match the operation

Soft differences become hard blockers when the shell topology or operation requires output purity.

## 8. Node Package Management Family

The same architecture should apply to `npm -> bun` and similar migrations.

### 8.1 Family scope

This family should cover operations whose essential outcome is mutation or realization of Node package state.

Examples:

- add dependencies
- remove dependencies
- install from manifest / lockfile

### 8.2 Family operations

Illustrative operation types:

```go
type NodeDependencyInstallOperation struct {
    Mode           string // "add-packages", "manifest-install", "clean-install"
    Packages       []PackageSpec
    Global         bool
    ProductionOnly bool
    FrozenLockfile bool
    IgnoreScripts  bool
    Registry       *Value
    WorkspaceScope *Value
}

type NodeDependencyRemoveOperation struct {
    Packages       []PackageSpec
    Global         bool
    WorkspaceScope *Value
}
```

These are intentionally outcome-oriented. They describe desired package/install state, not source CLI syntax.

### 8.3 Context-aware effective behavior

Node package-manager operations also need prior-context resolution.

The lifter should use stage env, observable files, and command-local context to determine effective package-manager behavior.

Examples:

- `.npmrc` written earlier in the stage can change registry, auth, script policy, or lockfile behavior
- `NPM_CONFIG_*` env can override registry and other install semantics
- `BUN_INSTALL_CACHE_DIR` or similar env can influence cache behavior even when the command line is silent

Fields like `IgnoreScripts`, `FrozenLockfile`, and `Registry` should therefore represent effective behavior after merging command flags with
configuration context. Provenance should explain whether those values came from the command line, env, or config files.

### 8.4 Why this family fits the same model

For Dockerfiles, the relevant outcomes are:

- manifest changes
- lockfile behavior
- installed dependency graph
- install semantics after applying contextual config inputs such as `.npmrc` or `NPM_CONFIG_*`

Usually irrelevant:

- progress spinners
- colorized output
- most log formatting flags

### 8.5 Representable subset for v1

Good initial deterministic cases:

- `npm install express`
- `npm uninstall express`
- `npm ci`

Probable ACP or no-fix cases:

- `npm run build`
- `npm exec ...`
- workspace-sensitive commands without explicit target equivalence
- tool-self-configuration commands such as `npm config set ...`
- commands whose meaningful outcome is a script side effect rather than package state

### 8.6 Target capability logic

Exactly the same rule applies:

- if the operation can be lifted
- and the target package manager can realize that operation
- and validation can prove the relevant outcome contract

then heuristic conversion is sound enough for an unsafe fix.

Otherwise, it should fall back to ACP or no-fix.

## 9. Fix Contract

This revision changes the decision tree, not the ACP output format.

### 9.1 Deterministic path

When lift, lower, and validation succeed:

- emit a normal unsafe `SuggestedFix`
- do not invoke ACP
- treat the family validator as the acceptance gate

### 9.2 ACP fallback path

When lift, lower, or validation fails:

- emit an ACP-based unsafe fix
- use the same exact replacement window
- pass structured family context into the ACP objective
- keep replacement-window output
- do not switch to diff output

This is the scoped precedence decision for this family: where doc 19 recommends diff output, this design overrides it and keeps replacement-window
output because the validator and replacement window are the safety boundary.

ACP remains useful, but it should no longer be the first resort for this family.

## 10. ACP Fallback Payload

ACP should receive a better starting point than raw shell text.

Illustrative payload:

```json
{
  "family": "http-transfer",
  "ruleCode": "hadolint/DL4001",
  "preferredTool": "wget",
  "shellVariant": "bash",
  "platformOS": "linux",
  "window": {
    "startLine": 12,
    "endLine": 12,
    "startColumn": 4,
    "endColumn": 67,
    "originalText": "curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app"
  },
  "outputTopology": "stdout-pipe",
  "liftStatus": "partially-lifted",
  "operation": {
    "method": "GET",
    "url": "https://example.com/app.tar.gz",
    "responseSink": "stdout-pipe"
  },
  "relatedSources": [
    {
      "kind": "env",
      "line": 5,
      "key": "CURL_HOME"
    },
    {
      "kind": "observable-file",
      "line": 6,
      "path": "/etc/curl/.curlrc"
    }
  ],
  "hardBlockers": [
    {
      "code": "fail-on-http-status-not-lowerable",
      "reason": "target capability table cannot preserve the source failure contract"
    }
  },
  "provenanceSummary": "redirect behavior may be influenced by prior curl config"
}
```

That gives ACP structured intent, structured blockers, and an exact replacement boundary.

### 10.1 ACP tool limits

Even on the ACP fallback path, execution should stay narrow.

Allowed:

- shell parsing and family validation inside tally
- objective-scoped local help or version introspection for the declared source or target tool
  - examples: `curl --help`, `curl -V`, `wget --help`, `wget --version`

Disallowed:

- network access
- long-running tests
- container builds
- arbitrary filesystem mutation
- unrelated repo exploration

The point of ACP here is localized rewrite help, not open-ended agent execution.

## 11. `DL4001` Pilot Policy

`hadolint/DL4001` should become the first rule using this architecture.

For each offending command:

1. detect the mixed `curl`/`wget` usage and choose the preferred tool using existing stage heuristics
2. isolate one candidate command window
3. run the HTTP-family lifter
4. if liftable, lower to the preferred tool
5. run HTTP-family validation
6. emit heuristic unsafe fix if accepted
7. otherwise emit ACP fallback for that same window

The likely common deterministic wins are:

- explicit file downloads
- transfer piped directly to `tar`
- one-off uncaptured fetches with no side-state

## 12. Implementation Plan

### 12.1 Current MVP status

The current PR implements the first vertical slice of this design for `hadolint/DL4001`. It does not yet implement the full generalized framework
described above, but it does prove the core lift/lower/validate/fallback flow on the initial `http-transfer` family.

Implemented in the current PR as MVP:

- shared `RunFacts.CommandOperationFacts` for the initial `http-transfer` family, including lifted vs blocked status and source replacement ranges
- bounded `HTTPTransferOperation` normalization for current `curl`/`wget` GET-transfer cases
- deterministic `curl <-> wget` lowering where the shared IR can preserve Dockerfile-relevant behavior
- `DL4001` integration that prefers deterministic focused rewrites first and falls back to ACP only for commands that cannot be lowered safely
- ACP replacement-window objective plumbing for one focused command window, including resolved-edit building instead of whole-file replacement
- focused validator checks for:
  - Dockerfile locality outside the target command window
  - command-count preservation inside the containing `RUN`
  - preservation of non-target commands
  - literal URL preservation
  - output destination preservation via sink/output-path comparison
  - multiline `RUN` handling when the target command starts on a continuation line
- tests for facts lifting/lowering, resolver behavior, focused objective validation, and `DL4001` suggested-fix behavior

Still intentionally left out of the MVP:

- generic family-adapter interfaces extracted as a reusable framework instead of the current HTTP-first implementation
- shared `Difference` modeling and richer partial-lift reporting beyond blockers
- standalone topology classification separate from family IR
- context-aware effective-behavior resolution from `EffectiveEnv`, observable files, `.curlrc`, `wgetrc`, and similar config inputs
- provenance/source-ref graphs for command windows, env bindings, observable files, and build-context inputs
- structured partial-lift/provenance payloads passed through ACP objective data
- corpus measurement and rollout gating
- PowerShell `Invoke-WebRequest` / `iwr`
- next family work such as `npm -> bun`

This split is intentional. The MVP takes the minimum HTTP-first path needed to validate the architecture on one real rule before extracting broader
generic abstractions.

### 12.2 Phase 1: shared framework

Status: partially implemented in MVP.

Completed in MVP:

- extend `FileFacts` / `RunFacts` with reusable `CommandOperationFacts`
- wire deterministic family fixes before ACP resolver dispatch for the `DL4001` pilot

Remaining after MVP:

- add explicit family adapter interfaces
- add shared blocker and difference types at the framework level
- add topology classification for command windows as a first-class shared fact
- plumb semantic-model, env, shell, and observable-file context into family lift inputs
- add provenance/source-ref structures for env bindings, observable files, and command windows

### 12.3 Phase 2: HTTP pilot

Status: partially implemented in MVP.

Completed in MVP:

- implement `HTTPTransferOperation`
- implement `curl` lifter
- implement `wget` lowerer
- implement initial validator coverage for bounded focused rewrites
- wire the HTTP family into `DL4001`

Remaining after MVP:

- resolve effective behavior from `EffectiveEnv` and observable `curlrc` / `wgetrc` files
- separate explicit topology/downstream hints from the current sink-focused validation
- broaden validator coverage with measured rollout data rather than only hand-picked fixtures
- add additional shell/tool adapters such as PowerShell `Invoke-WebRequest`

### 12.4 Phase 3: ACP upgrade

Status: partially implemented in MVP.

Completed in MVP:

- keep the replacement-window output contract
- pass focused command-window metadata and blocker summaries into ACP objective data

Remaining after MVP:

- pass lift status and partial operation into ACP objective data as structured fields
- pass blocker lists into ACP objective data as richer typed payloads rather than just summarized strings
- pass provenance/source refs into ACP objective data

### 12.5 Phase 4: corpus evaluation

Status: not started in MVP.

- collect a Dockerfile corpus with `curl` and `wget`
- measure:
  - recognizable by family
  - liftable
  - lowerable
  - validator-accepted
  - ACP fallback
  - top blocker categories
- use blocker distribution and validator acceptance as a rollout gate for Phase 2 broad enablement, not just as post-facto reporting

### 12.6 Phase 5: next family

Status: not started in MVP.

- prototype `npm -> bun`
- start with install/remove/clean-install operations
- defer config/script operations until capability tables are explicit

## 13. Coverage Hypothesis

The claim that this approach should cover roughly 95% of `curl` usage in Dockerfiles is plausible for the narrow "download transfer" subset, but it
should not be assumed without measurement.

This proposal recommends treating coverage as a corpus question, not as an article of faith.

The architecture is sound either way:

- high-coverage commands become deterministic unsafe fixes
- lower-coverage commands still benefit from structured ACP fallback

## 14. Recommendation

Proceed with semantic family normalization as the primary design:

- build it once in the facts layer, not once per rule
- family-specific abstract operations first
- target capability tables instead of flag mapping
- contextual lift using semantic state, env bindings, and observable files
- Dockerfile-relevant outcome equivalence instead of CLI symmetry
- provenance preserved for the lines and files that influenced effective behavior
- deterministic unsafe fix when lift + lower + validate succeeds
- ACP replacement-window fallback when it does not

For `DL4001`, this means the common `curl`/`wget` cases should stop being treated as "probably AI." They should become a bounded deterministic
transpilation problem.

## Appendix A: End-to-End Pipe Example

Original Dockerfile instruction:

```Dockerfile
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/app
```

### A.1 Shell/topology classification

- shell variant: `bash` or POSIX shell
- command family: `http-transfer`
- output topology: `stdout-pipe`
- downstream command: `tar`
- relevant surfaces:
  - `stream`
  - `filesystem`
  - `exit-status`

### A.2 Lift step

The `curl` lifter extracts:

- one URL: `https://example.com/app.tar.gz`
- method: `GET`
- failure policy: transport failure plus HTTP-status failure
- response sink: `stdout-pipe`
- output purity need: `true`
- downstream hint: `tar-extract`

Illustrative lifted operation:

```json
{
  "family": "http-transfer",
  "request": {
    "scheme": "https",
    "method": "GET",
    "url": {
      "raw": "https://example.com/app.tar.gz",
      "kind": "literal"
    },
    "failurePolicy": {
      "failOnTransportError": true,
      "failOnHTTPStatus": true
    }
  },
  "responseSink": {
    "kind": "stdout-pipe",
    "downstreamHint": "tar-extract"
  },
  "observability": {
    "quiet": true,
    "verbose": false,
    "outputPurityNeed": true
  }
}
```

### A.3 Lower step

The target selector says this stage prefers `wget`.

The `wget` capability table says:

- stdout output is supported
- a quiet mode is available
- this request shape is representable

So the serializer emits:

```text
wget -nv -O- https://example.com/app.tar.gz | tar -xz -C /opt/app
```

### A.4 Validation step

The validator reparses the replacement and confirms:

- there is still exactly one relevant transfer command in the replacement window
- the transfer still feeds stdout into a pipeline
- the URL is preserved
- the downstream command is still `tar`
- the extraction destination is still `/opt/app`
- no intermediate file is introduced
- no blocker from the capability table was ignored

If all of those checks pass, tally emits a heuristic unsafe fix.

If any check fails, tally does not guess. It falls back to ACP for that same replacement window.

## Appendix B: `npm -> bun` Example

Original:

```Dockerfile
RUN npm install express
```

Lifted operation:

```json
{
  "family": "node-package-management",
  "kind": "dependency-install",
  "mode": "add-packages",
  "packages": [
    { "name": "express" }
  ],
  "global": false
}
```

If the `bun` capability table confirms the same operation is representable, the serializer may emit:

```text
bun add express
```

Now contrast that with:

```Dockerfile
RUN npm config set fund false
```

This should not be lifted into the `node-package-management` family at all. It is a tool-self-configuration command, not a portable package-state
operation. It may matter to the Dockerfile, but it belongs to separate tool-specific logic or to no-fix, not to this normalization concept.

## 15. Sources

### Local tally sources

- [`internal/rules/hadolint/dl4001.go`](../internal/rules/hadolint/dl4001.go)
- [`internal/rules/hadolint/dl4001_test.go`](../internal/rules/hadolint/dl4001_test.go)
- [`internal/rules/hadolint/dl3014.go`](../internal/rules/hadolint/dl3014.go)
- [`internal/rules/hadolint/dl3030.go`](../internal/rules/hadolint/dl3030.go)
- [`internal/rules/hadolint/dl3034.go`](../internal/rules/hadolint/dl3034.go)
- [`internal/rules/hadolint/dl3038.go`](../internal/rules/hadolint/dl3038.go)
- [`internal/rules/hadolint/dl3047.go`](../internal/rules/hadolint/dl3047.go)
- [`internal/rules/hadolint/dl3027.go`](../internal/rules/hadolint/dl3027.go)
- [`internal/facts/doc.go`](../internal/facts/doc.go)
- [`internal/facts/facts.go`](../internal/facts/facts.go)
- [`internal/facts/observable_files.go`](../internal/facts/observable_files.go)
- [`internal/semantic/builder.go`](../internal/semantic/builder.go)
- [`internal/semantic/stage_info.go`](../internal/semantic/stage_info.go)
- [`internal/shell/command.go`](../internal/shell/command.go)
- [`internal/shell/chain.go`](../internal/shell/chain.go)
- [`internal/shell/archive.go`](../internal/shell/archive.go)
- [`internal/shell/install_packages.go`](../internal/shell/install_packages.go)
- [`internal/rules/tally/prefer_add_unpack.go`](../internal/rules/tally/prefer_add_unpack.go)
- [`internal/rules/tally/curl_should_follow_redirects.go`](../internal/rules/tally/curl_should_follow_redirects.go)
- [`internal/rules/tally/prefer_curl_config.go`](../internal/rules/tally/prefer_curl_config.go)
- [`internal/rules/tally/prefer_wget_config.go`](../internal/rules/tally/prefer_wget_config.go)
- [`internal/rules/tally/prefer_package_cache_mounts.go`](../internal/rules/tally/prefer_package_cache_mounts.go)
- [`internal/rules/tally/sort_packages.go`](../internal/rules/tally/sort_packages.go)
- [`internal/rules/tally/prefer_multi_stage_build.go`](../internal/rules/tally/prefer_multi_stage_build.go)
- [`internal/rules/tally/require_secret_mounts.go`](../internal/rules/tally/require_secret_mounts.go)
- [`internal/rules/runcheck/runcheck.go`](../internal/rules/runcheck/runcheck.go)
- [`internal/fix/fixer.go`](../internal/fix/fixer.go)
- [`internal/ai/autofix/resolver.go`](../internal/ai/autofix/resolver.go)
- [`internal/ai/autofixdata/objective.go`](../internal/ai/autofixdata/objective.go)
- [`design-docs/13-ai-autofix-acp.md`](../design-docs/13-ai-autofix-acp.md)
- [`design-docs/19-ai-autofix-diff-contract.md`](../design-docs/19-ai-autofix-diff-contract.md)

### External references

- [`curlconverter` `src/shell/Parser.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/shell/Parser.ts)
- [`curlconverter` `src/curl/opts.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/curl/opts.ts)
- [`curlconverter` `src/Request.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/Request.ts)
- [`curlconverter` `src/generators/wget.ts`](https://github.com/curlconverter/curlconverter/blob/06636420d2af2b28f78203dd7915ce0ba8fcdbba/src/generators/wget.ts)
- [`curlconverter` issue #703`](https://github.com/curlconverter/curlconverter/issues/703)
