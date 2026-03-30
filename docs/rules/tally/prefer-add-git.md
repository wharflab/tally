# tally/prefer-add-git

Prefer `ADD <git source>` over `git clone` inside `RUN`.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Security |
| Default | Enabled |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Flags `RUN` instructions that fetch source code with `git clone`, recommending BuildKit git sources such as
[`ADD --link https://github.com/user/repo.git /src/repo`](https://docs.docker.com/reference/dockerfile/#add).

Moving repository acquisition out of `RUN` makes the fetch explicit in the Dockerfile dependency graph, reduces mutable network behavior inside
shell steps, and improves hermeticity for supply-chain-sensitive builds.

## Detected Patterns

The rule reports remote `git clone` usage in shell-form `RUN` instructions, including:

1. Plain clones: `RUN git clone https://github.com/NVIDIA/apex`
2. Branch or tag selection: `RUN git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}`
3. Clone flows in chained commands: `RUN echo foo && git clone ... && cd repo && git checkout <full-commit-sha> && make`
4. GitLab HTTP remotes that need the generic selector form:
   `RUN git clone https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git -b ${GHC_WASM_META_COMMIT}`

## Examples

### Before (violation)

```dockerfile
FROM alpine:3.20

RUN git clone https://github.com/NVIDIA/apex

RUN echo before && git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo after
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM alpine:3.20

ADD --link https://github.com/NVIDIA/apex.git /apex

RUN echo before
ADD --link --checksum=0123456789abcdef0123456789abcdef01234567 https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567 /apex
RUN cd /apex && echo after
```

## Auto-fix Conditions

The rule emits a sync `FixSuggestion` when it can safely isolate the clone flow into:

- optional leading `RUN` commands that stay before the fetch
- one `ADD <git source> <destination>`
- optional trailing `RUN` commands that continue after the fetch

Current auto-fix coverage supports:

- simple POSIX shell-form `RUN` instructions
- `&&` chains where the clone flow can be isolated cleanly
- optional `-b` / `--branch`
- optional explicit destination directory
- optional `cd <repo>` followed by `git checkout <full-commit-sha>`
- optional recursive clone flags, mapped to `submodules=true`
- GitLab HTTP remotes via the generic `?ref=` selector form used in [`_tools/shellcheck-wasm/Dockerfile`](../../../_tools/shellcheck-wasm/Dockerfile)
- `ADD --link` for better cache reuse on extracted git-source layers
- `ADD --keep-git-dir=true` when later commands in the rewritten flow still run `git`
- `ADD --checksum=<full-commit-sha>` when the selected ref is a full commit ID

## Report-Only Cases

The rule still reports, but does not auto-fix, when the clone appears in a shape that currently cannot be rewritten without dropping execution
context, such as:

- `RUN` instructions with non-mount BuildKit flags like `--network=...`
- `RUN` instructions using mounts
- complex shell constructs outside a simple `&&` chain
- abbreviated hex `git checkout` values like `aa756ce`, because BuildKit git URLs safely encode full commit IDs, not abbreviated checkout SHAs
- clone flows with unsupported git flags or unresolved destination paths

## Limitations

- Current auto-fix targets POSIX shell parsing; non-POSIX shells are report-only
- The generated fix uses BuildKit git-source URLs, so it requires BuildKit-enabled builds
- The fixer emits `ref=` as the git-source selector. It does not guess between `branch=` and `tag=` because `git clone -b <name>` can refer to either
  one.
- When the selected ref is a full commit ID, the fixer emits `--checksum=<sha>` as a verifier.
- The fixer only rewrites the first clone flow in a matching `RUN`; additional clone flows can be fixed on a later run

## References

- [Dockerfile `ADD` reference](https://docs.docker.com/reference/dockerfile/#add)
- [Docker build context git URL queries (`branch`, `ref`, `commit`, `submodules`)](https://docs.docker.com/build/concepts/context/#url-queries)
- [GitLab remote URL format using `ref=` selectors](https://docs.gitlab.com/editor_extensions/visual_studio_code/remote_urls/)
