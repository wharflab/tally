# tally-cli

[![Documentation](https://img.shields.io/badge/docs-tally.wharflab.com-blue)](https://tally.wharflab.com/)
[![License: AGPL-3.0-only](https://img.shields.io/badge/license-AGPL--3.0--only-blue)](https://www.gnu.org/licenses/agpl-3.0.html)

**tally** is a production-grade Dockerfile/Containerfile linter and formatter that keeps build files clean, modern, and consistent. It uses
BuildKit's official parser and checks (the same foundation behind `docker buildx`) plus a safe auto-fix engine, runs as a single Go binary, and
needs no Docker daemon.

This RubyGem ships the same `tally` binary used by Tally's Homebrew, mise, npm, PyPI, and WinGet distributions, packaged so a `gem install` is
enough.

## Installation

```bash
gem install tally-cli
```

Then call `tally` directly:

```bash
# Lint everything in the repo (recursive)
tally lint .

# Apply all safe fixes automatically
tally lint --fix Dockerfile

# JSON / SARIF / GitHub Actions output for CI
tally lint --format sarif . > tally.sarif
```

## Why Ruby developers should care

The Ruby and Rails container story is unusually concrete in 2026 — Rails 7.1+ ships a generated production Dockerfile out of the box, and the wider
ecosystem has converged on Bundler 2.x, the official `ruby:*-slim` images, jemalloc, YJIT, and Kamal-style deployments. There is, in other words,
an unusually clear *right answer* for what a Rails Dockerfile should look like — and a corpus of real-world Dockerfiles that drift from it in
predictable ways.

Tally's research surveyed **196 Ruby/Rails Dockerfiles across 144 application repositories** (Mastodon, Discourse, Chatwoot, GitLab, Forem, Spree,
Solidus, Decidim, Errbit, Loomio, Manyfold, OpenStreetMap, OpenProject, Zammad, Basecamp's Once/Writebook/Fizzy, and many more). The recurring
drift, despite the Rails generator template being widely (often imperfectly) copied:

- **Bundler production env vars are inconsistent.** Only 37 / 196 set `BUNDLE_DEPLOYMENT=1`; only 41 / 196 set `BUNDLE_WITHOUT`.
- **`gem install bundler` is reinstalled** on top of a base image that already ships Bundler 2.x in 55 / 196 files.
- **Rails generator's bundler-cache cleanup is dropped on copy** in roughly 75% of files that run `bundle install`.
- **`assets:precompile` runs without `SECRET_KEY_BASE_DUMMY=1`** in 27 / 67 cases — forcing `RAILS_MASTER_KEY` to be present at build time (and into
  image history if passed via `ARG`/`ENV`).
- **`bootsnap precompile` runs without `-j 1`** in 39 / 44 cases. The Rails generator explicitly calls out the QEMU multi-arch crash this prevents.
- **`libjemalloc` is installed but never preloaded** in 16 / 28 cases — the package size is paid for and the allocator never loads.
- **Ruby version drift is severe.** 48 / 196 still pin Ruby 2.x; 15 / 196 pin Ruby 3.0 or 3.1 — all fully out of upstream support as of 2026.
- **YJIT is essentially unused** outside Mastodon. Ruby 3.3+ is widespread; YJIT is a near-free 15-30% production speedup; corpus uptake is 3 / 196.
- **Modern BuildKit + Bundler patterns are essentially absent.** 0 / 196 use `RUN --mount=type=bind` for `Gemfile`; 1 / 196 uses
  `RUN --mount=type=secret` for private gem auth; 0 / 196 use `RUN --network=none` for an offline install phase; 2 / 196 wire a `HEALTHCHECK` against
  Rails 7.1's `/up` endpoint.

Tally's `tally/ruby/*` rules target exactly these drifts.

## Ruby-specific rules

Tally ships **17 Ruby/Rails Dockerfile rules** under the [`tally/ruby/*`](https://tally.wharflab.com/rules/tally/ruby/) namespace. Highlights:

| Rule | What it catches |
|---|---|
| [`tally/ruby/jemalloc-installed-but-not-preloaded`](https://tally.wharflab.com/rules/tally/ruby/jemalloc-installed-but-not-preloaded/) | `libjemalloc2` installed but no `LD_PRELOAD` / `MALLOC_CONF` — paying image-size cost for an unused allocator. |
| [`tally/ruby/asset-precompile-without-dummy-key`](https://tally.wharflab.com/rules/tally/ruby/asset-precompile-without-dummy-key/) | `bin/rails assets:precompile` without `SECRET_KEY_BASE_DUMMY=1` — forces `RAILS_MASTER_KEY` into image history. |
| [`tally/ruby/bootsnap-precompile-without-j1`](https://tally.wharflab.com/rules/tally/ruby/bootsnap-precompile-without-j1/) | `bootsnap precompile` without `-j 1` — known QEMU multi-arch hang. |
| [`tally/ruby/missing-bundle-deployment`](https://tally.wharflab.com/rules/tally/ruby/missing-bundle-deployment/) | Production stage missing `BUNDLE_DEPLOYMENT=1`. |
| [`tally/ruby/missing-bundle-without-development`](https://tally.wharflab.com/rules/tally/ruby/missing-bundle-without-development/) | Production stage installing development/test gems because `BUNDLE_WITHOUT` isn't set. |
| [`tally/ruby/redundant-bundler-install`](https://tally.wharflab.com/rules/tally/ruby/redundant-bundler-install/) | `gem install bundler` on a base image that already ships Bundler 2.x. |
| [`tally/ruby/leftover-bundler-cache`](https://tally.wharflab.com/rules/tally/ruby/leftover-bundler-cache/) | `bundle install` without the cache-cleanup step from the Rails generator (`~/.bundle`, `cache/`, `bundler/gems/*/.git`). |
| [`tally/ruby/eol-ruby-version`](https://tally.wharflab.com/rules/tally/ruby/eol-ruby-version/) | Base image pinned to a Ruby branch that Ruby core no longer supports (Ruby 2.x, 3.0, 3.1 as of 2026). |
| [`tally/ruby/yjit-not-enabled-on-supported-runtime`](https://tally.wharflab.com/rules/tally/ruby/yjit-not-enabled-on-supported-runtime/) | Ruby 3.3+ runtime image without `RUBY_YJIT_ENABLE=1`. |
| [`tally/ruby/state-paths-not-writable-as-non-root`](https://tally.wharflab.com/rules/tally/ruby/state-paths-not-writable-as-non-root/) | Rails app runtime sets `USER` non-root but `tmp/`, `log/`, `storage/`, `db/` aren't `chown`'d. |
| [`tally/ruby/secrets-in-arg-or-env`](https://tally.wharflab.com/rules/tally/ruby/secrets-in-arg-or-env/) | `SECRET_KEY_BASE` / `RAILS_MASTER_KEY` declared via `ARG` or `ENV`. |
| [`tally/ruby/deprecated-bundler-install-flags`](https://tally.wharflab.com/rules/tally/ruby/deprecated-bundler-install-flags/) | `bundle install --without` / `--deployment` / `--path` (Bundler 2.x deprecated; use env vars). |
| [`tally/ruby/prefer-bundler-cache-mount`](https://tally.wharflab.com/rules/tally/ruby/prefer-bundler-cache-mount/) | `bundle install` on BuildKit without `RUN --mount=type=cache` for `${BUNDLE_PATH}/cache` — gem builds re-fetch every layer rebuild. |
| [`tally/ruby/prefer-gemfile-bind-mounts`](https://tally.wharflab.com/rules/tally/ruby/prefer-gemfile-bind-mounts/) | `COPY Gemfile Gemfile.lock` then `bundle install` — bind-mount instead so the manifests don't bake into image history. |
| [`tally/ruby/prefer-network-none-install`](https://tally.wharflab.com/rules/tally/ruby/prefer-network-none-install/) | Encourages the `bundle cache` + `RUN --network=none bundle install --local` two-phase pattern for hermetic, offline installs. |
| [`tally/ruby/prefer-secret-mounts-for-build-credentials`](https://tally.wharflab.com/rules/tally/ruby/prefer-secret-mounts-for-build-credentials/) | `BUNDLE_GITHUB__COM` / `GEM_HOST_API_KEY` / etc. via `ARG`/`ENV` — leaks into image cache key data; use `RUN --mount=type=secret` instead. |
| [`tally/ruby/healthcheck-rails-up-endpoint`](https://tally.wharflab.com/rules/tally/ruby/healthcheck-rails-up-endpoint/) | Rails web server runtime image with no `HEALTHCHECK` — Rails 7.1+ ships `/up` for free; probe it via the Ruby stdlib's `Net::HTTP` (no extra `apt-get install curl` needed). |

### Example: what tally fixes

Before:

```dockerfile
FROM ruby:3.3-slim

RUN gem install bundler
COPY Gemfile Gemfile.lock ./
ARG RAILS_MASTER_KEY
ENV RAILS_MASTER_KEY=$RAILS_MASTER_KEY
RUN bundle install --without development:test
RUN bundle exec bootsnap precompile app/ lib/
RUN bin/rails assets:precompile
CMD ["bin/rails", "server"]
```

`tally lint Dockerfile` flags every line above. Each finding cites the rule code, the corpus base rate, and the upstream documentation that
motivated the rule (Rails generator template, Bundler upgrade notes, bootsnap issue #495, Rails 7.1 release notes, etc.). `--fix` applies the safe
mechanical edits; `--fix-unsafe` unlocks the rest.

After (the Rails generator's canonical shape, derived):

```dockerfile
# syntax=docker/dockerfile:1
FROM ruby:3.3-slim AS base
WORKDIR /rails
ENV RAILS_ENV=production \
    BUNDLE_DEPLOYMENT=1 \
    BUNDLE_PATH=/usr/local/bundle \
    BUNDLE_WITHOUT="development:test" \
    RUBY_YJIT_ENABLE=1

FROM base AS build
RUN --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install --jobs=4 \
 && rm -rf ~/.bundle/ "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git \
 && bundle exec bootsnap precompile -j 1 --gemfile

COPY . .
RUN bundle exec bootsnap precompile -j 1 app/ lib/ \
 && SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile

FROM base
RUN useradd -u 1000 rails
COPY --chown=rails:rails --from=build /usr/local/bundle /usr/local/bundle
COPY --chown=rails:rails --from=build /rails /rails
USER rails:rails
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["ruby", "-rnet/http", "-e", \
    "exit Net::HTTP.get_response(URI('http://127.0.0.1:3000/up')).is_a?(Net::HTTPSuccess) ? 0 : 1"]
CMD ["bin/rails", "server"]
```

## Why tally

Modern Dockerfiles deserve modern tooling. tally is opinionated in the right places:

- **BuildKit-native**: understands modern syntax like heredocs, `RUN --mount=...`, `COPY --link`, and `ADD --checksum=...`.
- **Fixes, not just findings**: `--fix` applies safe, mechanical rewrites; `--fix-unsafe` unlocks opt-in risky fixes (including AI).
- **Modernizes on purpose**: converts eligible `RUN`/`COPY` instructions to heredocs, prefers BuildKit `ADD` sources for archives and git repos.
- **Broad rule coverage**: combines Docker's official BuildKit checks, embedded ShellCheck for shell snippets, Hadolint-compatible rules, and
  tally-specific rules — including the `tally/ruby/*` namespace covered above.
- **PowerShell-aware**: parses full PowerShell syntax for semantic tokens and rule analysis.
- **Windows-container aware**: detects Windows container OS, understands Windows paths and default shells.
- **Registry-aware without Docker**: uses a Podman-compatible registry client for image metadata checks (no daemon required).
- **Editor + CI friendly**: VS Code extension (`wharflab.tally`, powered by `tally lsp`) and outputs for JSON, SARIF, and GitHub Actions annotations.
- **Single fast Go binary** with **92% code coverage on Codecov** and **2,900+ Go tests in CI**.

## Documentation

For installation, usage, configuration, the full rules reference, and per-rule examples and rationale, visit
**[tally.wharflab.com](https://tally.wharflab.com/)**.

The Ruby rules are documented at **[tally.wharflab.com/rules/tally/ruby](https://tally.wharflab.com/rules/tally/ruby/)**.

## License

AGPL-3.0-only. See LICENSE for the full license text.
