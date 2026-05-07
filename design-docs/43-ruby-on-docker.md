# 43. Ruby Container Rules (`tally/ruby/*`)

**Status:** Draft

**Design Focus:** Propose a dedicated `tally/ruby/*` namespace targeting the patterns that are unique to Ruby — and especially Ruby on Rails —
production containers in 2026. Ruby's "modern" container story is unusually concrete: Rails 7.1+ ships a generated `Dockerfile` out of the box, and
the wider ecosystem has converged on Bundler 2.x, the official `ruby:*-slim` images, jemalloc, and Kamal-style deployments. That gives us a rare
luxury for lint design: a canonical reference Dockerfile that real apps measurably drift from.

The rules below are intentionally niche. Generic advice (multi-stage builds, `apt-get` cleanup, non-root `USER`) is already covered by other tally
namespaces and by hadolint, and is intentionally excluded.

In addition to catching mistakes, this namespace plays an explicit educational role. Several rules promote modern Ruby/BuildKit features that are
well-documented but almost completely unused in the corpus — `bundle cache` + `--network=none` install, `RUN --mount=type=secret` for private gem
auth, `RUN --mount=type=bind` for `Gemfile`/`Gemfile.lock` instead of layer-baking COPY, and Rails 7.1's built-in `/up` health endpoint. These rules
are advisory rather than corrective: their job is to surface the modern pattern at the moment a user writes the older one.

This namespace is also explicitly designed to take advantage of tally's context-aware linting infrastructure. Bake/Compose/`--context` entrypoints
already give the linter an `InvocationContext` whose `ContextRef().Value` resolves to the build-context directory; `internal/context.BuildContext`
already exposes `ReadFile`/`FileExists`/`PathExists` against that directory with `.dockerignore` semantics applied. Ruby projects ship a particularly
rich set of build-context artifacts (`Gemfile`, `Gemfile.lock`, `.ruby-version`, `config/credentials.yml.enc`, `config/routes.rb`, `config/puma.rb`,
`bin/docker-entrypoint`, `vendor/cache/`, `tmp/`, `log/`, `storage/`, `db/`) that — when observable — let rules upgrade from heuristics to
ground-truth claims. Section 4.5 below maps each rule to the specific context-file evidence that sharpens it.

---

## 1. Decision

Introduce a dedicated `tally/ruby/*` namespace with an initial batch of **17 Ruby/Rails-specific rules**.

The namespace should focus on:

1. Bundler defaults that are specifically wrong for production images (`BUNDLE_DEPLOYMENT`, `BUNDLE_WITHOUT`, deprecated install flags, redundant
   bundler reinstalls).
2. Rails-specific build-time patterns that the Rails generator already encodes correctly but real Dockerfiles repeatedly forget
   (`SECRET_KEY_BASE_DUMMY`, bootsnap parallelism, jemalloc preload, asset/node cleanup).
3. Ruby-specific runtime hardening that is invisible from a generic Dockerfile lens (YJIT, `MALLOC_ARENA_MAX`, EOL runtime versions).
4. Secret hygiene that is Rails-specific (`SECRET_KEY_BASE` and `RAILS_MASTER_KEY` in `ARG`/`ENV`).
5. **Modern BuildKit + Bundler patterns the corpus barely uses** — bind-mounted manifests, `bundle cache` + `RUN --network=none`, secret mounts for
   private gem auth, and Rails 7.1's `/up` health endpoint. These rules promote features that already exist in the ecosystem but have ~0 % adoption.
6. Rules with a credible fix story that anchors on the Rails generator template, not on opinionated novelty.

The namespace should **not** absorb generic Docker advice such as broad `apt-get` cleanup, generic multi-stage guidance, generic non-root rules, or
generic image-pinning rules. That advice is well-covered by existing `tally/*` rules and hadolint.

---

## 2. Ground Truth

### 2.1 Corpus methodology

Discovery used `gh search code` and `gh search repositories` (via the GitHub CLI) to identify Ruby projects that ship a real `Dockerfile`. Repository
discovery seeded from:

- Top-starred repos with `language:ruby` plus topics `rails`, `rails-application`, `rails8`, `rails7`, `sinatra`, `kamal`, `hotwire`, `turbo-rails`,
  `saas`, `cms`, `ecommerce`, `goodjob`, `sidekiq`, `redis`, `postgresql`, `graphql`, `heroku`, `fly`, `starter-kit`, `docker`.
- Hand-curated reference repos (`rails/rails`, `mastodon/mastodon`, `discourse/discourse`, `chatwoot/chatwoot`, `forem/forem`, `gitlabhq/gitlabhq`,
  `decidim/decidim`, `huginn/huginn`, `solidusio/solidus`, `spree/spree`, `opf/openproject`, `basecamp/once-campfire`, `basecamp/writebook`,
  `basecamp/fizzy`, `basecamp/kamal`, `inaturalist/inaturalist`, `manyfold3d/manyfold`, `loomio/loomio`, `errbit/errbit`,
  `openstreetmap/openstreetmap-website`, `pupilfirst/pupilfirst`, `maybe-finance/maybe`, `nickjj/docker-rails-example`,
  `lipanski/ruby-dockerfile-example`, `Shopify/graphql-batch`, `bullet-train-co/bullet_train`, `bensheldon/good_job`, `theforeman/foreman`,
  `zammad/zammad`, `freescout-help-desk/freescout`, `openemr/openemr`, `SUSE/Portus`, `manageiq/manageiq`).

For each candidate repo, every file matching `Dockerfile` or `*.Dockerfile` (recursively) was downloaded directly via `raw.githubusercontent.com`.
Files that did not contain `FROM (.../)?ruby[:@]` (or `passenger`, `phusion`, `jruby`) were dropped to keep the analysis Ruby-specific.

The raw corpus contained **286 Dockerfile/Containerfile variants across 159 repositories**. As with the PHP namespace research
([35-php-container-rules.md](35-php-container-rules.md)), a small number of template-style repositories dominate the raw set:

| Repository | Dockerfile-ish files |
|---|---:|
| `fluent/fluentd-docker-image` | 48 |
| `bugsnag/bugsnag-ruby` (Rails fixture matrix) | 15 |
| `lipanski/ruby-dockerfile-example` (educational) | 14 |
| `YACS-RCOS/yacs` | 9 |
| `openHPI/xikolo-core` (microservice fanout) | 9 |

To avoid template farms drowning out application signal, this draft uses an **app subset of 196 files across 144 unique repositories**. The app
subset excludes:

- `fluent/fluentd-docker-image` (CI version matrix per Debian variant).
- `lipanski/ruby-dockerfile-example` (educational walk-through with intentional anti-examples).
- `YACS-RCOS/yacs` (legacy version matrix).
- Files whose path contains `devcontainer` (these are local-dev images and use a different image lineage, e.g.
  `ghcr.io/rails/devcontainer/images/ruby` and `mcr.microsoft.com/vscode/devcontainers/ruby`).

Representative application repositories in the subset include:

- `mastodon/mastodon`
- `chatwoot/chatwoot`
- `forem/forem`
- `discourse/discourse`
- `decidim/decidim`
- `solidusio/solidus`
- `spree/spree`
- `errbit/errbit`
- `openstreetmap/openstreetmap-website`
- `inaturalist/inaturalist`
- `huginn/huginn`
- `basecamp/once-campfire`
- `basecamp/writebook`
- `basecamp/fizzy`
- `manyfold3d/manyfold`
- `loomio/loomio`
- `gitlabhq/gitlabhq`
- `maybe-finance/maybe`
- `pupilfirst/pupilfirst`
- `nickjj/docker-rails-example`
- `openHPI/xikolo-core`
- `opf/openproject`
- `zammad/zammad`
- `freescout-help-desk/freescout`
- `openemr/openemr`

### 2.2 External guidance that aligns with the corpus

Ruby's official guidance is unusually concrete and unusually consistent with the corpus.

- **The Rails generator emits a canonical production Dockerfile.** Since Rails 7.1, `rails new` produces a multi-stage `Dockerfile.tt` template that
  uses `ruby:$RUBY_VERSION-slim`, sets `RAILS_ENV=production`, `BUNDLE_DEPLOYMENT=1`, `BUNDLE_PATH=/usr/local/bundle`,
  `BUNDLE_WITHOUT="development"`, links and preloads `libjemalloc.so` via `LD_PRELOAD`, runs `bundle install` then strips
  `~/.bundle/`, `${BUNDLE_PATH}/ruby/*/cache`, and `${BUNDLE_PATH}/ruby/*/bundler/gems/*/.git`, runs
  `bundle exec bootsnap precompile -j 1 --gemfile` then `app/ lib/`, runs `SECRET_KEY_BASE_DUMMY=1 ./bin/rails assets:precompile`, removes
  `node_modules` after the asset stage, and creates a non-root `rails:rails` (uid/gid 1000) user in the final stage.
  Source:
  [Rails Dockerfile.tt](https://github.com/rails/rails/blob/main/railties/lib/rails/generators/rails/app/templates/Dockerfile.tt)
- **Bundler documents the deployment-mode contract.** `bundle config set deployment 'true'` (or `BUNDLE_DEPLOYMENT=1`) makes Bundler refuse to mutate
  `Gemfile.lock`, requires the lockfile to exist, and installs gems into `BUNDLE_PATH`. `BUNDLE_WITHOUT` excludes gem groups; Bundler 2.x deprecated
  `bundle install --without`/`--deployment`/`--path` in favor of `bundle config`.
  Sources:
  [Bundler `bundle install`](https://bundler.io/v2.5/man/bundle-install.1.html),
  [Bundler `bundle config`](https://bundler.io/v2.5/man/bundle-config.1.html),
  [Bundler upgrade-from-1.x notes](https://bundler.io/guides/bundler_2_upgrade.html).
- **The Docker Hub Ruby image already ships Bundler 2.x.** Re-installing `bundler` via `gem install bundler` in a Dockerfile is therefore redundant on
  modern bases. Source:
  [docker-library/ruby Dockerfile.template](https://github.com/docker-library/ruby/blob/master/Dockerfile-debian.template).
- **Rails autoloader and bootsnap have a documented QEMU multi-arch hazard.** The Rails generator explicitly comments that `bootsnap precompile -j 1`
  is required to avoid a known QEMU bug under cross-architecture builds.
  Source: [bootsnap issue #495](https://github.com/Shopify/bootsnap/issues/495).
- **Asset precompile must not depend on real production secrets at build time.** Rails honors `SECRET_KEY_BASE_DUMMY=1` at asset compilation time
  precisely so that `RAILS_MASTER_KEY` does not need to be present (and therefore baked into image history) during `docker build`.
  Sources:
  [Rails 7.1 release notes](https://guides.rubyonrails.org/7_1_release_notes.html#assets-precompile-no-longer-requires-credentials),
  [Rails security guide](https://guides.rubyonrails.org/security.html#environmental-security).
- **YJIT is documented as a near-free production speedup on Ruby 3.2+.** `RUBY_YJIT_ENABLE=1` is the documented switch, and Mastodon's production
  Dockerfile turns it on by default. Sources:
  [Ruby 3.3 release notes](https://www.ruby-lang.org/en/news/2023/12/25/ruby-3-3-0-released/),
  [YJIT documentation](https://github.com/ruby/ruby/blob/master/doc/yjit/yjit.md).
- **jemalloc is the Ruby community's standard memory allocator for long-running Rails workers.** The Rails Dockerfile and Mastodon both pull
  `libjemalloc2` and either `LD_PRELOAD` it or set `MALLOC_CONF`. Without one of those, installing the package buys nothing.
  Sources:
  [discourse/discourse Dockerfile](https://github.com/discourse/discourse/blob/main/Dockerfile),
  [GitHub Engineering: Ruby memory allocators](https://github.blog/engineering/scaling-monolith-software-development-with-rust/).
- **Ruby support policy.** The Ruby core team retires Ruby branches on a published cadence: Ruby 2.x is fully out of support; Ruby 3.0 was retired
  2024-03-31; Ruby 3.1 was retired 2025-03-31; Ruby 3.2, 3.3, and 3.4 remain supported.
  Source: [Ruby maintenance branches](https://www.ruby-lang.org/en/downloads/branches/).
- **Kamal and Thruster have specific runtime expectations.** Both 37signals projects target the Rails generator Dockerfile as their input contract.
  Sources:
  [basecamp/kamal](https://github.com/basecamp/kamal),
  [basecamp/thruster](https://github.com/basecamp/thruster).

### 2.3 Representative examples worth preserving

Good patterns worth teaching:

- `basecamp/once-campfire:Dockerfile`
  - Canonical Rails-generator-derived multi-stage with linked + preloaded jemalloc, deployment Bundler config, dummy `SECRET_KEY_BASE`, and a
    non-root `rails:rails` runtime user.
- `mastodon/mastodon:Dockerfile`
  - Production-grade multi-stage with `RUBY_YJIT_ENABLE=1`, jemalloc 5 `MALLOC_CONF` tuning (`narenas:2,background_thread:true,thp:never`), and
    multi-arch awareness.
- `nickjj/docker-rails-example:Dockerfile`
  - Multi-stage Rails 8 / Trixie example with explicit non-root `ruby:ruby` user, `--chown=ruby:ruby` on every relevant `COPY`, and disciplined
    bundle and yarn separation.
- `manyfold3d/manyfold:docker/base.dockerfile`
  - Rails generator descendant with proper bundler cache cleanup and bootsnap precompile.
- `loomio/loomio:Dockerfile`
  - Sets `MALLOC_ARENA_MAX=2` and the canonical jemalloc `LD_PRELOAD`.

Common copy-paste problems worth catching:

- `basecamp/writebook:Dockerfile` — installs `libjemalloc2` in the final stage but never sets `LD_PRELOAD` (jemalloc never actually loads).
- `errbit/errbit:Dockerfile` — bootsnap precompile without `-j 1`; redundant `gem install bundler`.
- `chatwoot/chatwoot:docker/Dockerfile` — `assets:precompile` without `SECRET_KEY_BASE_DUMMY=1` (forces real `RAILS_MASTER_KEY` at build).
- `huginn/huginn:docker/multi-process/Dockerfile` — Ruby 2.x base; bootsnap precompile without `-j 1`.
- `archonic/limestone:Dockerfile` — Ruby 2.x base; missing dummy key on asset precompile.
- `basecamp/kamal:Dockerfile` — explicit `gem install bundler` redundant with the modern `ruby:*-slim` base; broad `COPY .` before
  `bundle install`.
- `cossacklabs/acra-engineering-demo:rails/rails.dockerfile` — Ruby 2.x base; no dummy key; very dated Bundler patterns.

---

## 3. Corpus Findings

### 3.1 High-signal patterns

The table below uses the **app subset (196 files / 144 repos)** as the main baseline.

| Finding | App subset |
|---|---:|
| Multi-stage Dockerfiles | 78 / 196 |
| `RAILS_ENV=production` set | 76 / 196 |
| `BUNDLE_DEPLOYMENT=1` set | 37 / 196 |
| `BUNDLE_WITHOUT` set | 41 / 196 |
| `bundle install` invoked | 155 / 196 |
| `bundle install` with `--jobs` flag | 32 / 196 |
| `bundle install --without` (deprecated 2.x flag) | 3 / 196 |
| `bundle install --deployment` (deprecated 2.x flag) | 1 / 196 |
| `gem install bundler` (redundant on modern bases) | 55 / 196 |
| Bundler cache cleanup (`~/.bundle`, `BUNDLE_PATH/cache`, `bundler/gems/*/.git`) | 38 / 196 |
| `assets:precompile` invoked | 67 / 196 |
| ↳ with `SECRET_KEY_BASE_DUMMY=1` (correct) | 40 / 67 |
| `bootsnap precompile` invoked | 44 / 196 |
| ↳ with `-j 1` flag (QEMU-safe) | 5 / 44 |
| `libjemalloc` package installed | 28 / 196 |
| ↳ with `LD_PRELOAD` or `MALLOC_CONF` set | 12 / 28 |
| `RUBY_YJIT_ENABLE=1` | 3 / 196 |
| `ruby:2.x` base (EOL) | 48 / 196 |
| `ruby:3.0` or `ruby:3.1` base (EOL) | 15 / 196 |
| `ruby:3.2`–`ruby:3.4` base (supported) | 46 / 196 |
| BuildKit `RUN --mount=type=cache` | 7 / 196 |
| BuildKit `RUN --mount=type=bind` for `Gemfile`/`Gemfile.lock` | 0 / 196 |
| BuildKit `RUN --mount=type=secret` (any) | 1 / 196 |
| `RUN --network=none` (any stage) | 0 / 196 |
| `bundle cache` / `bundle package` (offline-prep) | 2 / 196 |
| `bundle install --local` (offline install) | 1 / 196 |
| `HEALTHCHECK` instruction (any) | 6 / 196 |
| `HEALTHCHECK` against Rails `/up` endpoint | 2 / 196 |
| `COPY Gemfile`/`COPY Gemfile.lock` (instead of bind mount) | 94 / 196 |
| Any explicit `USER` | 72 / 196 |
| `USER` set to a non-root identity | 53 / 196 |

### 3.2 What this means

The Ruby container ecosystem has a stronger canonical reference than most. The Rails generator's `Dockerfile.tt` is widely (often imperfectly) copied,
and Bundler/jemalloc/YJIT defaults are well-documented. That gives this namespace an unusually clean target shape for lint rules.

But the corpus still shows the same recurring drift:

1. **Bundler production env vars are set inconsistently.** `BUNDLE_DEPLOYMENT` (37 / 196) and `BUNDLE_WITHOUT` (41 / 196) are missing far more often
   than they are present, even when the rest of the Dockerfile clearly intends to be a production build.
2. **`gem install bundler` is reinstalled on top of a base image that already ships Bundler 2.x** in 55 / 196 files.
3. **The bundler cache cleanup that the Rails generator performs is dropped on copy** in roughly 75 % of files that run `bundle install`.
4. **`assets:precompile` is run without `SECRET_KEY_BASE_DUMMY=1`** in 27 / 67 cases, which either forces `RAILS_MASTER_KEY` to be present at build
   (where it ends up in image history if passed via `ARG`/`ENV`) or causes the asset pipeline to fail at runtime.
5. **`bootsnap precompile` runs without `-j 1`** in 39 / 44 cases. The Rails generator explicitly calls out the QEMU multi-arch crash that this flag
   prevents — and yet the comment is one of the first things that gets stripped in derivative Dockerfiles.
6. **`libjemalloc` is installed but never preloaded** in 16 / 28 cases. The package is roughly 350 KiB; the symlink and `LD_PRELOAD` are the two lines
   that actually matter, and they are routinely lost.
7. **Ruby version drift is severe.** 48 / 196 still pin Ruby 2.x, and 15 / 196 pin Ruby 3.0 or 3.1 — both fully out of upstream support as of 2026.
8. **YJIT is essentially unused** outside Mastodon. Ruby 3.3+ is widespread; YJIT is a near-free production speedup; corpus uptake is 3 / 196.
9. **BuildKit cache mounts for Bundler are essentially unused** (7 / 196). This is a much sharper Bundler win than it is in many other ecosystems
   because gem builds frequently compile native extensions.
10. **Modern BuildKit + Bundler patterns are essentially absent from the corpus.** Of the 196 application Dockerfiles, **0** use
    `RUN --mount=type=bind` for `Gemfile`/`Gemfile.lock` (94 still `COPY` them), **1** uses `RUN --mount=type=secret` for private gem auth, **0** use
    `RUN --network=none` for an offline install phase, and only **2** wire a `HEALTHCHECK` against Rails 7.1's built-in `/up` endpoint. Each of these
    is a single Dockerfile-level edit away. Each is well-documented in the upstream sources. The lint surface here is almost entirely educational —
    surface the modern pattern at the moment a user writes the older one.

The PHP-style "well-known canonical, widely copied imperfectly" pattern repeats here — which is the environment where lint rules are most useful.

---

## 4. Proposed Rules

This draft proposes **17 rules** in four batches. The first six are the highest-value implementation batch; the last four are deliberately
educational and ship advisory-only.

### 4.1 Batch 1: should ship first

#### `tally/ruby/jemalloc-installed-but-not-preloaded`

**Problem**

Installing `libjemalloc2` (or `libjemalloc1`, or `jemalloc-dev`, or alpine's `jemalloc`) without then setting `LD_PRELOAD` (or `MALLOC_CONF`) means
the allocator is paid for in image size but never loaded by the process. Ruby keeps using glibc malloc, which on long-lived Rails worker processes is
the exact memory-fragmentation problem jemalloc was added to fix.

**Why this is grounded**

- The Rails generator template performs both halves of the fix in the base stage:
  `apt-get install ... libjemalloc2 ... && ln -s ... /usr/local/lib/libjemalloc.so` followed by
  `ENV ... LD_PRELOAD="/usr/local/lib/libjemalloc.so"`.
- Mastodon goes a step further with `MALLOC_CONF="narenas:2,background_thread:true,thp:never,..."`, which has the side effect of confirming jemalloc
  is loaded.
- In the app subset, **16 of 28** Dockerfiles that install jemalloc never preload it.

**Trigger shape**

- A stage installs a jemalloc package (`libjemalloc2`, `libjemalloc1`, `libjemalloc-dev`, alpine `jemalloc`).
- The same Dockerfile (in any stage that contributes to the final image) has neither:
  - `ENV LD_PRELOAD=...jemalloc...`, nor
  - `ENV MALLOC_CONF=...` with jemalloc-specific knobs (`narenas:`, `background_thread:`, `dirty_decay_ms:`, `muzzy_decay_ms:`, `thp:`).

**Guardrails**

- Ignore stages whose only purpose is to build `libjemalloc` for copy-out.
- Ignore non-final stages.
- Treat a `MALLOC_CONF` set with jemalloc-specific keys as evidence that jemalloc is loaded another way (e.g. via `LD_PRELOAD` set elsewhere or via
  the base image already linking it).

**Fix story**

- `FixSuggestion`: insert the canonical `ln -s /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so` step (when the apt
  variant is in use) plus `ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"`.
- Not `FixSafe` because the symlink path depends on the base image variant (Debian `slim` vs Alpine vs custom), which is more than a single-line
  edit can guarantee.

**Representative misses**

- `basecamp/writebook`
- `errbit/errbit`
- `bugsnag/bugsnag-ruby` (Rails 8 fixture)
- `CanineHQ/canine`
- `gesteves/denali`
- `miharekar/visualizer`
- `tarunvelli/rails-tabler-starter`

#### `tally/ruby/asset-precompile-without-dummy-key`

**Problem**

Rails 7.1+ honors `SECRET_KEY_BASE_DUMMY=1` at asset compile time so that `bin/rails assets:precompile` works without a real `secret_key_base` and
without `RAILS_MASTER_KEY`. When this flag is absent, projects are pushed toward exactly the wrong workaround: passing `RAILS_MASTER_KEY` (or a real
`SECRET_KEY_BASE`) into the build via `ARG`/`ENV`, which bakes the secret into the image history layer-by-layer.

**Why this is grounded**

- The Rails generator template runs `RUN SECRET_KEY_BASE_DUMMY=1 ./bin/rails assets:precompile` and explicitly comments that this avoids requiring
  the real `RAILS_MASTER_KEY` at build time.
- Rails 7.1 release notes call this out as the supported path.
- In the app subset, **27 of 67** Dockerfiles that run `assets:precompile` do not set `SECRET_KEY_BASE_DUMMY`. Of those, 3 actively pass
  `RAILS_MASTER_KEY` or `SECRET_KEY_BASE` through `ARG`/`ENV` (this overlap is the strongest signal for the rule below in 4.3).

**Trigger shape**

- A `RUN` instruction invokes `rails assets:precompile`, `bin/rails assets:precompile`, `bundle exec rake assets:precompile`, or
  `rake assets:precompile`.
- That same `RUN` does not set `SECRET_KEY_BASE_DUMMY=1` (or `SECRET_KEY_BASE=1`, which Rails also accepts as a placeholder).
- The stage is not explicitly a development/test stage.

**Guardrails**

- Skip development/test stages.
- Treat `--mount=type=secret,id=rails_master_key` together with `RAILS_MASTER_KEY=$(cat /run/secrets/...)` as a compliant alternative path
  (BuildKit-secret-driven precompile).
- Do not fire when `assets:precompile` is invoked with `SECRET_KEY_BASE_DUMMY=1` or with an inline `SECRET_KEY_BASE=...` constant placeholder.

**Fix story**

- Usually `FixSafe`: prepend `SECRET_KEY_BASE_DUMMY=1` to the `RUN` command. This is the exact patch suggested by the Rails 7.1 release notes.

**Representative misses**

- `chatwoot/chatwoot`
- `archonic/limestone`
- `archonic/limestone-accounts`
- `cossacklabs/acra-engineering-demo`
- `elovation/elovation`
- `AgileVentures/WebsiteOne`
- `diowa/icare`

#### `tally/ruby/bootsnap-precompile-without-j1`

**Problem**

`bundle exec bootsnap precompile` defaults to parallel compilation (`-j N` where N is host CPU count). Inside `docker buildx`'s QEMU emulation —
which is how almost every multi-arch CI build of a Rails app runs today — that parallelism crashes the emulator on a known QEMU bug. The Rails
generator explicitly carries a `-j 1` flag and a code comment about this.

**Why this is grounded**

- [bootsnap issue #495](https://github.com/Shopify/bootsnap/issues/495) is open and tracked.
- The Rails generator template both invocations carry `-j 1`:
  `RUN bundle install && ... bundle exec bootsnap precompile -j 1 --gemfile` and
  `RUN bundle exec bootsnap precompile -j 1 app/ lib/`.
- In the app subset, **39 of 44** files that run `bootsnap precompile` do so without `-j 1`. That is overwhelmingly the wrong default for any CI
  doing multi-arch builds.

**Trigger shape**

- A `RUN` instruction executes `bootsnap precompile` (with or without `bundle exec`) and does **not** carry `-j 1` (or `-j1`).

**Guardrails**

- Skip if `bootsnap precompile` is gated behind a `BUILDPLATFORM == TARGETPLATFORM` shell check (i.e. the user has explicitly avoided emulated paths).
- Do not fire on `bootsnap` invocations other than `precompile`.

**Fix story**

- Usually `FixSafe`: insert `-j 1` directly after `bootsnap precompile`. This matches the Rails generator's exact wording.

**Representative misses**

- `mastodon/mastodon`
- `errbit/errbit`
- `huginn/huginn` (multi-process)
- `maybe-finance/maybe`
- `zammad/zammad`
- `MindscapeHQ/raygun4ruby` (Rails 7.1 fixture)
- `bugsnag/bugsnag-ruby` (Rails 8 fixture)

#### `tally/ruby/missing-bundle-deployment`

**Problem**

A production-shaped Ruby Dockerfile that runs `bundle install` without `BUNDLE_DEPLOYMENT=1` (or `bundle config set --local deployment 'true'`)
silently allows Bundler to mutate `Gemfile.lock` at build time, install gems outside the project, and skip the lockfile-required check. That defeats
the "the lockfile is the build input" property and pushes Bundler back into Bundler-1.x semantics.

**Why this is grounded**

- The Rails generator sets `ENV ... BUNDLE_DEPLOYMENT="1"` in the base stage.
- Bundler's own docs describe deployment mode as the production contract: lockfile is required and frozen, `BUNDLE_PATH` is honored, dev/test-only
  gems are not installed.
- In the app subset, only **37 of 196** Dockerfiles set `BUNDLE_DEPLOYMENT=1` (or `bundle config set deployment`). Even strictly production-shaped
  Dockerfiles miss it more often than not.

**Trigger shape**

- A stage runs `bundle install` (in any form, including inside heredocs).
- The Dockerfile has `RAILS_ENV="production"` (or `RACK_ENV="production"`) somewhere, *or* the stage has no explicit non-production marker and the
  final stage is shaped like an app runtime.
- Neither `ENV BUNDLE_DEPLOYMENT=1` nor `bundle config set [--local|--global] deployment 'true'` is present in the same stage or in an inherited base
  stage.

**Guardrails**

- Skip stages explicitly marked as `dev`, `development`, `test`, `testing`, or `ci`.
- Treat `bundle install --deployment` (deprecated, see 4.3) as compliant for this rule but still flagged by the deprecated-flags rule.
- Treat `bundle config set --global frozen 'true'` as a partial-compliance signal but **not** sufficient on its own; deployment mode is the broader
  contract and the corpus shows that frozen-only setups still drift.

**Fix story**

- `FixSafe`: insert `ENV BUNDLE_DEPLOYMENT="1"` at the top of the stage that runs `bundle install`. Insertion point is intentionally early, matching
  the `tally/php/composer-no-interaction-in-build` precedent (insert before the first `RUN` so any nested wrappers also see it).

**Representative misses**

- `chatwoot/chatwoot`
- `errbit/errbit`
- `openstreetmap/openstreetmap-website`
- `huginn/huginn`
- `inaturalist/inaturalist`
- `freescout-help-desk/freescout`

#### `tally/ruby/missing-bundle-without-development`

**Problem**

A production stage that runs `bundle install` without `BUNDLE_WITHOUT="development"` (or `BUNDLE_WITHOUT="development:test"`) ships every gem in the
`development` and `test` groups — typically including `web-console`, `rspec-rails`, `byebug`, `pry`, `listen`, `spring`, `bullet`, `letter_opener`,
and similar — into the production image. Beyond image size, several of those gems are well-known security-attack-surface multipliers (`web-console`
in particular has had RCE history when it leaks into production).

**Why this is grounded**

- The Rails generator sets `ENV ... BUNDLE_WITHOUT="development"` in the base stage.
- Bundler 2.x docs document `BUNDLE_WITHOUT` and `bundle config set without` as the supported mechanism.
- `web-console` is bundled by default in the Rails generator's `Gemfile` `development` group, and Rails security advisories
  ([CVE-2018-16476 / GHSA-rgcj-wfg5-2v4h](https://github.com/rails/rails/security/advisories) family) repeatedly document the risk of leaking it.
- In the app subset, only **41 of 196** Dockerfiles set `BUNDLE_WITHOUT`. **155** run `bundle install`.

**Trigger shape**

- A stage runs `bundle install`.
- The Dockerfile is shaped like a production runtime (per `missing-bundle-deployment` heuristics).
- Neither `ENV BUNDLE_WITHOUT=` nor `bundle config set [--local|--global] without` excludes the `development` group.

**Guardrails**

- Skip explicit dev/test stages.
- Treat any `BUNDLE_WITHOUT` value containing `development` as compliant; do not opinion-fight on whether `test` is included.
- Treat `BUNDLE_ONLY="default:production"` (the inverse selector, supported by Bundler 2.5+) as compliant.

**Fix story**

- `FixSafe`: insert `ENV BUNDLE_WITHOUT="development"` at the top of the stage. Match the Rails generator's exact wording.

**Representative misses**

- `chatwoot/chatwoot`
- `errbit/errbit`
- `huginn/huginn`
- `gitlabhq/gitlabhq`
- `inaturalist/inaturalist`
- `solidusio/solidus_starter_frontend`

#### `tally/ruby/redundant-bundler-install`

**Problem**

`gem install bundler` (or `gem install bundler -v X`) inside a `Dockerfile` that uses `ruby:*-slim`, `ruby:*-alpine`, or any modern `ruby:*` base
re-downloads, recompiles, and re-installs Bundler on top of the Bundler 2.x that ships with the official image already. It also reliably introduces
Bundler version drift between local development and CI builds.

**Why this is grounded**

- The official `docker-library/ruby` image's `Dockerfile.template` installs and pre-resolves Bundler before publishing the tag. That Bundler is on
  `$PATH` immediately.
- In the app subset, **55 of 196** Dockerfiles still run `gem install bundler` after `FROM ruby:...` — including `basecamp/kamal`,
  `basecamp/fizzy`, `chatwoot/chatwoot`, and `doubtfire-lms/doubtfire-api`.

**Trigger shape**

- A `RUN` invokes `gem install bundler` (with or without `-v`).
- The current stage's effective base image is `ruby:*` (any tag), `docker.io/library/ruby:*`, `registry.docker.com/library/ruby:*`, or
  `ghcr.io/rails/devcontainer/images/ruby:*`.

**Guardrails**

- Skip if the base image is **not** an official `ruby` image (e.g. `debian:slim` with manual Ruby compilation, or `alpine` plus `ruby` from
  `apk add ruby`).
- Skip if the install is gated behind a check that already detects mismatch (e.g. `bundle --version | grep` then conditional install).
- Allow the deliberate downgrade case: `gem install bundler -v <X>` where the `Gemfile.lock`'s `BUNDLED WITH` block pins a Bundler version older
  than what the base image ships. The rule should advise using `bundle _<version>_ install` (Bundler's version-aware shim) instead.

**Fix story**

- `FixSuggestion`: delete the `gem install bundler` step. If the user genuinely needs a specific Bundler version, the suggested replacement is to
  rely on the `BUNDLED WITH` block in `Gemfile.lock` plus Bundler's own version-shim behavior, which the official images already support.

**Representative misses**

- `basecamp/kamal`
- `basecamp/fizzy`
- `chatwoot/chatwoot`
- `doubtfire-lms/doubtfire-api`
- `errbit/errbit`
- `archonic/limestone`

### 4.2 Batch 2: valuable follow-up rules

#### `tally/ruby/leftover-bundler-cache`

**Problem**

After `bundle install` runs in deployment mode, Bundler leaves three pieces of bloat in the image:
`~/.bundle/`, `${BUNDLE_PATH}/ruby/*/cache` (gem `.gem` archives), and `${BUNDLE_PATH}/ruby/*/bundler/gems/*/.git` (full git histories of
git-sourced gems). For non-trivial Rails apps this routinely costs 50–200 MiB of final-image weight.

**Why this is grounded**

- The Rails generator template runs:
  `bundle install && rm -rf ~/.bundle/ "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git`.
- In the app subset, only **38 of 196** files perform any of these cleanups.

**Trigger shape**

- A stage runs `bundle install` and does not delete `~/.bundle/`, `${BUNDLE_PATH}/ruby/*/cache`, or `${BUNDLE_PATH}/ruby/*/bundler/gems/*/.git` in
  the same `RUN` (or in a later `RUN` within the same stage).

**Guardrails**

- Skip when the stage is purely a builder stage that exports gems via `COPY --from=...` (the cache only matters in stages whose layers ship in the
  final image).
- Skip when `bundle clean --force` is run (Bundler's own cleanup mechanism that produces an equivalent result for `cache/`).

**Fix story**

- `FixSuggestion`: append the canonical Rails-generator cleanup to the `bundle install` step.

#### `tally/ruby/prefer-bundler-cache-mount`

**Problem**

Native Ruby gems (nokogiri, pg, sassc, grpc, ffi, mysql2, sqlite3, rugged, oj, bcrypt, `puma`'s C extension, `nio4r`, `protobuf`, etc.) recompile from
scratch on every cache-busted build. BuildKit's `RUN --mount=type=cache,target=/usr/local/bundle/cache` is the documented Bundler win for CI; Bundler
detects the cached `.gem` archives and skips download/extraction.

**Why this is grounded**

- Tally already understands BuildKit `RUN --mount` options (`internal/runmount`).
- Tally already has `tally/prefer-package-cache-mounts` for ecosystem package managers; this rule is the Bundler-shaped variant.
- In the app subset, **only 7 of 196** Dockerfiles use cache mounts at all.

**Trigger shape**

- A stage runs `bundle install`.
- The same `RUN` does not bind a cache mount onto `BUNDLE_PATH` or `${BUNDLE_PATH}/cache`.

**Guardrails**

- Skip Windows stages.
- Be advisory rather than a hard correctness rule — the gem compile time win matters most in CI; on local builds there are no measurable downsides
  but the user may not run BuildKit.
- The rule should be a no-op when `# syntax=docker/dockerfile:1` (or higher) is **not** present at the top of the file, since BuildKit cache mounts
  are syntax-gated.

**Fix story**

- `FixSuggestion`: rewrite the `RUN bundle install` to add
  `--mount=type=cache,id=bundler,target=${BUNDLE_PATH}/cache,sharing=locked` (or, on systems without BUNDLE_PATH, the resolved literal path).
- This is a structural edit; do not make it `FixSafe`.

#### `tally/ruby/eol-ruby-version`

**Problem**

Ruby 2.x is fully out of upstream support. Ruby 3.0 retired 2024-03-31. Ruby 3.1 retired 2025-03-31. Production Dockerfiles pinned to those versions
ship without security patches and cannot pick up YJIT, jemalloc 5 awareness, or modern Rails performance work.

**Why this is grounded**

- Ruby's own [maintenance branches page](https://www.ruby-lang.org/en/downloads/branches/) is the upstream truth.
- In the app subset:
  - **48 of 196** files pin Ruby 2.x.
  - **15 of 196** pin Ruby 3.0 or 3.1.
  - Only **46 of 196** pin a currently-supported branch (3.2/3.3/3.4).

**Trigger shape**

- The first `FROM` (or any `FROM` that contributes to the final image) resolves to one of:
  - `ruby:2.*`
  - `ruby:3.0*`
  - `ruby:3.1*`
- ARG-driven versions (`ARG RUBY_VERSION=...`) are evaluated against the default value when the default is literal.

**Guardrails**

- Skip if the Dockerfile's path indicates it is intentionally testing legacy compatibility (`spec/fixtures/...`, `test/legacy/...`,
  `compatibility/...`).
- Make the rule data-driven: implement EOL dates as a small embedded table (similar to the `tally/gpu/deprecated-cuda-image` data-table direction
  outlined in [32-gpu-container-rules.md](32-gpu-container-rules.md)). The data table is the source of truth, not hard-coded version comparisons.
- Severity should be configurable: `warning` for retired-but-still-receiving-security-patches, `error` for fully retired branches.

**Fix story**

- `FixSuggestion`: rewrite `FROM ruby:<eol>` to `FROM ruby:<latest-supported>` matching the same variant suffix (`-slim`, `-alpine`, `-bookworm`).
  Not `FixSafe` because major version bumps may require gem updates.

#### `tally/ruby/state-paths-not-writable-as-non-root`

**Problem**

A Rails app that runs as a non-root `USER` (commonly `rails:rails`, `app`, or `ruby`) needs write access to `tmp/`, `log/`, `storage/`, and `db/`
inside `WORKDIR`. The Rails generator template handles this in its final-stage `COPY --chown=rails:rails`. Forks of that template that switch to a
plain `COPY` (or that introduce a non-root `USER` independent of the generator pattern) end up with root-owned `tmp/cache` directories that crash the
app on first request when ActionView, ActiveStorage, or `bin/rails db:prepare` tries to write.

**Why this is grounded**

- The Rails generator template runs:
  `COPY --chown=rails:rails --from=build "${BUNDLE_PATH}" "${BUNDLE_PATH}"` and
  `COPY --chown=rails:rails --from=build /rails /rails`.
- `basecamp/writebook` adds the explicit `chown -R rails:rails db log storage tmp` for the same reason.
- This is a Rails-specific hazard because the directory list (`tmp`, `log`, `storage`, `db`) is Rails convention, not generic Docker.

**Trigger shape**

- A stage explicitly sets `USER` to a non-root identity (uid != 0, name != `root`).
- The same stage `COPY`s application content (typically `COPY . .`, `COPY /rails /rails`, or `COPY --from=build /rails /rails`).
- Neither the `COPY` carries `--chown=<that user>` nor a subsequent `chown -R <user>` covers the standard Rails state directories.
- The base image is plausibly a Rails app (Ruby base + the stage references `bundle`/`rails` somewhere).

**Guardrails**

- Skip stages where the stage is clearly a CLI gem image (no `WORKDIR /rails`, no `bin/rails`, no `bundle exec rails`, no `RAILS_ENV`).
- Skip when there is a runtime-side `chown` (e.g. `RUN chown -R rails:rails db log storage tmp`) before `USER` is set.
- The rule should not require `db` ownership when `db` is not present as a copied directory (Rails API mode).

**Fix story**

- `FixSuggestion`: change the offending `COPY` to add `--chown=<runtime user>:<runtime user>`. If there are multiple `COPY` instructions, prefer
  rewriting them all rather than inserting an after-the-fact `chown -R` (which is more layer-bloat than necessary).

### 4.3 Batch 3: niche, lower-noise

#### `tally/ruby/secrets-in-arg-or-env`

**Problem**

Rails secrets (`SECRET_KEY_BASE`, `RAILS_MASTER_KEY`) declared via `ARG` or `ENV` end up in image history. `docker history --no-trunc <image>` and any
registry that proxies image manifests will leak them. This is a Rails-specific variant of generic secret-in-Dockerfile rules: the names are
well-known, the impact is concrete, and the recommended fix (BuildKit `--mount=type=secret`) is well-documented.

**Why this is grounded**

- Rails security guide: secrets must come from `config/credentials.yml.enc` (decrypted with `RAILS_MASTER_KEY`) or environment variables provided at
  runtime — not at build time.
- The `tally/ruby/asset-precompile-without-dummy-key` rule above is the structural fix to make this avoidable.
- In the app subset, this is rare but consistently dangerous when present:
  - 1 file with `ARG SECRET_KEY_BASE`
  - 1 file with `ARG RAILS_MASTER_KEY`
  - 2 files with `ENV RAILS_MASTER_KEY=...`
  - 3 files with `ENV SECRET_KEY_BASE=...` (literal value, not the dummy form)

**Trigger shape**

- An `ARG` or `ENV` instruction defines `SECRET_KEY_BASE`, `RAILS_MASTER_KEY`, `SECRET_TOKEN`, `DEVISE_SECRET_KEY`, `DEVISE_PEPPER`, or
  `RAILS_KEY` (Rails core + Devise canonical names).
- The value is non-empty and is not literally the placeholder `1`/`dummy`/`SECRET_KEY_BASE_DUMMY`.

**Guardrails**

- Treat `ENV SECRET_KEY_BASE=1` (the dummy/placeholder form Rails accepts) as compliant — it is an explicit signal of "this is the placeholder
  contract", not a real secret.
- Treat `ARG SECRET_KEY_BASE` with no default value as a low-confidence trigger (still an antipattern because `ARG` becomes part of build cache key
  data, but lower severity).
- Recommend `RUN --mount=type=secret,id=...` as the fix.

**Fix story**

- `FixUnsafe`: cannot delete the secret directly; instead emit a fix-suggestion that points the user at BuildKit secret mounts and the
  `SECRET_KEY_BASE_DUMMY=1` build-time pattern.

#### `tally/ruby/yjit-not-enabled-on-supported-runtime`

**Problem**

YJIT is Ruby's bundled JIT compiler. On Ruby 3.3+, on most Rails workloads, it is approximately a free 15–30 % CPU win. It is opt-in. The opt-in is
literally `RUBY_YJIT_ENABLE=1` (env var) or `--yjit` at boot. Almost no production Dockerfile turns it on.

**Why this is grounded**

- Ruby 3.3 release notes describe YJIT as production-ready and call out the env switch.
- Mastodon enables it explicitly:
  `ENV ... RUBY_YJIT_ENABLE="${RUBY_YJIT_ENABLE}"` (default `1`).
- In the app subset, **3 of 196** Dockerfiles enable YJIT. That is essentially zero adoption.

**Trigger shape**

- The base image resolves to a Ruby version 3.3+ (so YJIT is available and considered stable).
- The Dockerfile shape is a long-running web/worker runtime (final stage exposes a port, runs `bin/rails`/`bundle exec puma`/`bin/thrust`/`sidekiq`).
- The Dockerfile does not set `RUBY_YJIT_ENABLE=1`, does not pass `--yjit` to the server entrypoint, and does not set `RUBYOPT="--yjit"`.

**Guardrails**

- Skip CLI-only gem images.
- Skip non-final stages.
- Skip Ruby 3.0/3.1 — YJIT existed but was experimental and had Rails-specific regressions; the rule should be silent there.
- Severity is `info`/`suggestion`. This is performance advice, not correctness.

**Fix story**

- `FixSuggestion`: add `ENV RUBY_YJIT_ENABLE="1"` to the final-stage `ENV` block.

#### `tally/ruby/deprecated-bundler-install-flags`

**Problem**

Bundler 2.x deprecated `bundle install --without`, `bundle install --deployment`, and `bundle install --path` in favor of `BUNDLE_*` env vars and
`bundle config set`. These flags still work but emit a deprecation notice on every CI build and are slated for removal in Bundler 3.

**Why this is grounded**

- [Bundler 2 upgrade guide](https://bundler.io/guides/bundler_2_upgrade.html) explicitly lists these flags.
- In the app subset, the corpus is small but consistent: **3 files use `--without`** and **1 uses `--deployment`**. The rule's value is in catching
  the next file that copy-pastes from a 2018 blog post.

**Trigger shape**

- A `RUN` invokes `bundle install` with `--without`, `--deployment`, or `--path`.

**Guardrails**

- Treat `bundle install --frozen` as a separate signal (still supported; advisory only).

**Fix story**

- `FixSafe` for `--without <groups>` → set `ENV BUNDLE_WITHOUT="<groups>"` and remove the flag.
- `FixSafe` for `--deployment` → set `ENV BUNDLE_DEPLOYMENT="1"` and remove the flag (this overlaps cleanly with `missing-bundle-deployment`).
- `FixSuggestion` for `--path <dir>` → set `ENV BUNDLE_PATH="<dir>"` and remove the flag.

### 4.4 Batch 4: educational, promote modern features

These rules have low corpus base rates by construction — the patterns they recommend have ~0 % adoption — so the value is in surfacing the modern
feature at the moment a user reaches for the older one. All four are advisory (`info`/`suggestion`) by default and should never be promoted to
`error`.

#### `tally/ruby/prefer-gemfile-bind-mounts`

**Problem**

The dominant pattern in the corpus is `COPY Gemfile Gemfile.lock ./` followed by `RUN bundle install`. That bakes the manifest files into an image
layer that is later overwritten by `COPY . .` and then rewritten again by every `assets:precompile` and `bootsnap precompile` invocation. With
BuildKit, the manifest files can be bind-mounted directly into the `RUN` step, never appearing as layer content at all. Combined with
`RUN --mount=type=cache` for the Bundler cache, this is the modern shape of a Ruby dependency stage.

This is the Ruby analog of `tally/php/prefer-composer-manifest-bind-mounts`.

**Why this is grounded**

- Docker's official Ruby/Rails containerization guide already uses `RUN --mount=type=cache` for the JS toolchain side (yarn cache); the Bundler
  side is a one-line port of the same idea plus `RUN --mount=type=bind`.
- BuildKit's `RUN --mount=type=bind` is well-documented and supported on every modern builder.
- Tally already parses `RUN --mount` options (`internal/runmount`).
- Corpus signal is unambiguous: **94 / 196** files `COPY Gemfile`/`COPY Gemfile.lock`, **0 / 196** bind-mount them.

**Trigger shape**

- A `COPY` instruction copies `Gemfile`, `Gemfile.lock`, or both as standalone files (not as part of `COPY . .`).
- The `COPY` is followed in the same stage by a `RUN` that invokes `bundle install` (or `bundle config set` then `bundle install`).
- Confidence tiers (mirroring `tally/php/prefer-composer-manifest-bind-mounts`):
  - high confidence: `COPY Gemfile Gemfile.lock` (or `COPY Gemfile* ./`) is the only thing the manifest layer carries.
  - medium confidence: `COPY Gemfile Gemfile.lock .ruby-version vendor` and similar — the Bundler bind-mount story still applies, but the user may
    want `vendor/` copied for vendored gems.

**Guardrails**

- Skip Windows stages (no `RUN --mount=type=bind`).
- Skip stages without a `# syntax=docker/dockerfile:1` (or higher) pragma — bind mounts are syntax-gated.
- Treat existing `RUN --mount=type=bind,source=Gemfile,target=Gemfile` as compliant.
- Keep `COPY Gemfile Gemfile.lock` as a valid fallback in the rule's wording — not every project ships with BuildKit enabled.

**Fix story**

- `FixSuggestion`: rewrite the `COPY` + `RUN bundle install` pair into a single `RUN` that bind-mounts both manifests and a cache mount for
  `${BUNDLE_PATH}/cache`:

  ```dockerfile
  RUN --mount=type=bind,source=Gemfile,target=Gemfile \
      --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
      --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
      bundle install
  ```

- Not `FixSafe` because the rewrite removes the manifest from the image layer entirely; if anything later in the Dockerfile re-references the
  copied files, that will need investigation.

**Representative misses**

- `chatwoot/chatwoot`
- `mastodon/mastodon`
- `forem/forem`
- `decidim/decidim`
- `errbit/errbit`
- `gitlabhq/gitlabhq`

#### `tally/ruby/prefer-network-none-install`

**Problem**

Bundler can split the dependency lifecycle into two halves: `bundle cache` (download `.gem` files into a cache directory, network required) and
`bundle install --local` (install from the cache only, network not required). With BuildKit, the install half can be wrapped in
`RUN --network=none`, which guarantees that nothing in the install path — including post-install C-extension build scripts — is making outbound
network calls. The result is a strictly reproducible install step and a real defense-in-depth boundary against malicious gems exfiltrating data at
build time.

This is a niche feature and should ship advisory-only, but it is exactly the kind of capability lint should surface to users who would otherwise
never know it exists.

**Why this is grounded**

- Bundler's own docs document `bundle cache` and `bundle install --local` as the offline-install pair. `--local` skips the rubygems.org check
  entirely.
- BuildKit's `RUN --network=none` is documented and stable. It is supported by every modern builder.
- Recent ecosystem incidents (the `xz` upstream supply-chain compromise, periodic compromised gem releases) make sandboxed install steps a
  defensible recommendation.
- Corpus signal: **2 / 196** use `bundle cache`/`bundle package`, **1 / 196** uses `bundle install --local`, **0 / 196** combine either with
  `RUN --network=none`. The pattern is essentially absent.

**Trigger shape**

- Advisory rule: only fires when an obvious build stage runs `bundle install` and is otherwise modern (BuildKit syntax pragma is present, multi-stage
  layout, manifest copy or bind mount). The point is not to nag every Dockerfile — the point is to teach users who already opted into BuildKit.

**Guardrails**

- Severity: `info`/`suggestion`. Never higher.
- Suppress when the Dockerfile lacks a `# syntax=docker/dockerfile:1` pragma — `--network=none` is BuildKit-only.
- Suppress on Windows stages.
- Suppress when `bundle install` already runs under `--mount=type=bind` for the manifest **and** under `--mount=type=cache` for the gem cache; that
  pattern is already a strong improvement and the next step (split into two `RUN`s) is a much larger refactor.

**Fix story**

- `FixSuggestion` only. The fix narrative is a two-step pattern:

  ```text
  RUN --mount=type=bind,source=Gemfile,target=Gemfile \
      --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
      --mount=type=cache,target=/bundle-cache,sharing=locked \
      bundle config set --local cache_path /bundle-cache && \
      bundle cache --no-install --all-platforms

  RUN --network=none \
      --mount=type=bind,source=Gemfile,target=Gemfile \
      --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
      --mount=type=cache,target=/bundle-cache,sharing=locked \
      bundle config set --local cache_path /bundle-cache && \
      bundle install --local
  ```

- The rule should never auto-rewrite this — the suggestion is intentionally educational.

**Representative misses**

- Every multi-stage Rails Dockerfile in the corpus that uses BuildKit syntax (≈ 20 / 196).

#### `tally/ruby/healthcheck-rails-up-endpoint`

**Problem**

Rails 7.1 added `Rails::HealthController` mounted at `/up` by default in `config/routes.rb`. It returns `200` if the app booted cleanly, `500`
otherwise. It is exactly the right shape for a Docker `HEALTHCHECK`. Almost no Rails Dockerfile uses it, and the few that do reach for `curl` —
which forces an extra package install on `ruby:*-slim` and `ruby:*-alpine` bases (where `curl` is not present by default).

The rule has three concerns, in priority order:

1. **Use the Ruby stdlib `Net::HTTP` for the healthcheck command.** Ruby is already in the image. `net/http` is in the stdlib. Adding `curl` (or
   `wget`) just for `HEALTHCHECK` is a ~3 MiB install for one shell line.
2. **Target `/up` (the Rails 7.1+ default).** Custom `/healthz`/`/health` endpoints work, but `/up` is the convention and has no per-app
   implementation cost.
3. **Use the JSON exec form** so the healthcheck command runs as PID *exec* rather than under `/bin/sh -c`. Faster, no shell injection surface, and
   correctly propagates signals if the healthcheck itself is ever killed.

This rule has two flavors:

1. **Missing `HEALTHCHECK` on a Rails 7.1+ runtime stage** — recommend adding the canonical Ruby-native one.
2. **`HEALTHCHECK` present but uses `curl`/`wget`** — suggest replacing with the Ruby stdlib equivalent. Especially when the same Dockerfile
   `apt-get install`s `curl` only because the healthcheck needs it (the corpus shows this exact pattern).

**Why this is grounded**

- Rails 7.1 release added `Rails::HealthController`; the route is generated by `rails new` since 7.1.
- The Rails generator's `Dockerfile.tt` does **not** add `HEALTHCHECK`, which is exactly the gap this rule fills.
- The official Ruby stdlib `Net::HTTP` ships with the runtime; both `ruby:*-slim` and `ruby:*-alpine` ship it. Source:
  [Net::HTTP stdlib reference](https://docs.ruby-lang.org/en/3.4/Net/HTTP.html).
- This rule mirrors the design direction the Node.js namespace is taking with native `fetch` for healthchecks. The principle is the same: when the
  language runtime is already in the image and ships an HTTP client in stdlib, prefer that over adding `curl`.
- Corpus signal:
  - **6 / 196** use `HEALTHCHECK` at all.
  - **2 / 196** target `/up`.
  - Both `/up` healthchecks (`ryanwi/rails7-on-docker`, `etewiah/property_web_builder`) use `curl` against a `ruby:*-slim` base, and both then need
    `apt-get install ... curl ...` upstream of that line. Neither needs to.
  - **0 / 196** uses a Ruby-native healthcheck. This is a pure educational gap.

**Trigger shape**

- Final-stage runtime where the entrypoint or `CMD` runs Rails (`bin/rails server`, `bundle exec rails server`, `bin/thrust`, `bin/boot`, or
  `bundle exec puma` with `config.ru` present).
- Variant 1 fires when the final stage has no `HEALTHCHECK`.
- Variant 2 fires when `HEALTHCHECK` is present and the command starts with `curl` or `wget`. Increase the severity of variant 2 when the same
  Dockerfile installs `curl` (or `wget`) via `apt-get`/`apk` *and* curl is not used outside the healthcheck — that is the strongest case for the
  Ruby-native rewrite.

**Guardrails**

- Skip non-final stages and CLI tooling images.
- Skip API-only Rails apps when the user has explicitly mounted the health endpoint elsewhere — but this is hard to detect from the Dockerfile
  alone. Default to fire-with-low-severity and let users disable per-file.
- Suppress when `HEALTHCHECK` is `NONE` (the explicit "I have an external orchestrator" signal).
- Suppress variant 2 when `curl`/`wget` is genuinely needed elsewhere in the same image (e.g. the entrypoint script `curl`s into the app). The
  Dockerfile-only signal for this is "more than one `curl` use site outside `HEALTHCHECK`".
- Treat `/up`, `/healthz`, `/health` all as compliant for the path concern; only suggest `/up` as the recommended path because it is the Rails
  default.
- Skip JRuby final stages — `Net::HTTP` semantics are the same, but the boot-time cost of spinning JRuby for every healthcheck poll is non-trivial,
  so the rule's recommendation may be a poor fit.

**Fix story**

Preferred fix (Ruby-native, no extra packages, JSON exec form, JSON-array argv):

```text
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["ruby", "-rnet/http", "-e", "exit Net::HTTP.get_response(URI('http://127.0.0.1:3000/up')).is_a?(Net::HTTPSuccess) ? 0 : 1"]
```

Why this exact shape:

- `ruby -rnet/http -e` keeps the entire HTTP client in the image's existing Ruby interpreter; no `curl`, no `wget`, no extra layer.
- `Net::HTTP.get_response(URI('...'))` returns a `Net::HTTPResponse` whose subclass implements `Net::HTTPSuccess` for any 2xx — so the check
  succeeds for `/up`'s normal `200 OK` and correctly fails on `500` (which is what `Rails::HealthController` returns when the boot fails).
- Using `127.0.0.1` rather than `localhost` avoids one DNS lookup per probe, and is robust against IPv6-only `localhost` weirdness inside containers.
- The JSON exec form (`["ruby", ...]`) means Docker runs the command directly via `execve` rather than spawning `/bin/sh -c`. This matters more for
  `HEALTHCHECK` than `CMD` because healthchecks run on every interval — the per-invocation shell startup adds up.

Fallback fix when `curl` already exists in the image for unrelated reasons (e.g. the entrypoint uses `curl`):

```text
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["curl", "-fsS", "http://127.0.0.1:3000/up"]
```

The exec form is still preferred over `CMD curl ...` to avoid the `/bin/sh -c` wrapper.

Severity:

- Variant 1 (missing healthcheck) → `FixSuggestion`.
- Variant 2 (uses `curl`/`wget`) → `FixSuggestion`. Promote to higher confidence when the same Dockerfile only installs `curl` for the healthcheck;
  the rewrite then also lets the user drop `curl` from the `apt-get install` line, which the rule should mention but **not** auto-edit.

**Representative misses (variant 1: no HEALTHCHECK)**

- `mastodon/mastodon`
- `chatwoot/chatwoot`
- `forem/forem`
- `huginn/huginn`
- `errbit/errbit`
- `basecamp/once-campfire`
- `basecamp/writebook`
- `maybe-finance/maybe`

**Representative misses (variant 2: HEALTHCHECK with curl on slim base)**

- `ryanwi/rails7-on-docker` — installs `curl` in the same `apt-get install` as `libjemalloc2` and `libvips`; `curl` is not used anywhere else in the
  Dockerfile.
- `etewiah/property_web_builder` — same shape; `curl` is in the apt list specifically for this `HEALTHCHECK`.
- `weathermen/soundstorm` — Alpine; installs `curl` via `apk add` for the healthcheck only.

#### `tally/ruby/prefer-secret-mounts-for-build-credentials`

**Problem**

When a Ruby image needs build-time credentials — `BUNDLE_GITHUB__COM` for github-hosted private gems, `BUNDLE_<HOST>__<TLD>` for any private gem
server, `RAILS_MASTER_KEY` for build-time `rails db:schema:load`, npm/yarn `_authToken` for private node packages used during asset compile — the
overwhelming temptation is to pass them via `ARG` or `ENV`. Both bake the credential into image history and into the build cache. BuildKit's
`RUN --mount=type=secret` is the documented production pattern: secrets exist for the duration of one `RUN`, never appear in image content, and are
not part of the cache key.

This is a Ruby-shaped variant of the generic "secrets in build" advice. It is Ruby-specific because Bundler's auth env-var convention
(`BUNDLE_<HOST_WITH_DOTS_AS_DOUBLE_UNDERSCORES>=<token>`) is unique to the ecosystem and is the most common shape of a build-time secret in the
corpus.

**Why this is grounded**

- Bundler documents `BUNDLE_<HOST>__<TLD>` as the credential env-var for private gem servers (e.g. `BUNDLE_GITHUB__COM`,
  `BUNDLE_GEMS__MYCOMPANY__COM`, `BUNDLE_RUBYGEMS__PKG__GITHUB__COM`).
- BuildKit's `RUN --mount=type=secret,id=<id>,env=<VAR>` syntax (env-driven variant) is the documented best fit for these env vars: the secret is
  exposed only inside the one `RUN`, and the secret value never reaches the image cache key.
- The corpus already contains the canonical good example. `basecamp/fizzy/saas`:

  ```text
  RUN --mount=type=secret,id=GITHUB_TOKEN ... \
      BUNDLE_GITHUB__COM="$(cat /run/secrets/GITHUB_TOKEN):x-oauth-basic" bundle install
  ```

- Corpus signal: **1 / 196** uses `RUN --mount=type=secret` at all. Multiple files in the corpus pass `RAILS_MASTER_KEY` or `BUNDLE_GITHUB__COM` via
  `ARG`/`ENV` instead.

**Trigger shape**

The rule fires when **any** of these patterns is present:

1. `ARG` or `ENV` for `BUNDLE_<HOST>__<TLD>` — Bundler's host credential env-var pattern (host name with dots replaced by `__`).
2. `ARG` or `ENV` for `GEM_HOST_API_KEY` (rubygems push key).
3. `ARG` or `ENV` for `BUNDLE_GITHUB__COM`, `BUNDLE_BITBUCKET__ORG`, `BUNDLE_GITLAB__COM` specifically.
4. `ARG` or `ENV` for `NPM_TOKEN`, `YARN_AUTH_TOKEN`, `_AUTH_TOKEN` — Yarn/npm auth used during `bin/rails assets:precompile`.
5. Build-time `RAILS_MASTER_KEY` passed via `ARG` or `ENV` (overlaps with `secrets-in-arg-or-env`; this rule is the suggested fix path for that
   problem).

**Guardrails**

- This rule is the **constructive companion** to `tally/ruby/secrets-in-arg-or-env`. The first rule says "stop"; this one says "do this instead".
- Suppress when the same RUN already consumes the value via `RUN --mount=type=secret`.
- Severity: `info`/`suggestion` for the BuildKit promotion; the underlying credential leak is already covered by `secrets-in-arg-or-env`.

**Fix story**

- `FixSuggestion`:
  - For `BUNDLE_GITHUB__COM`-style env vars:

    ```text
    RUN --mount=type=secret,id=github_token \
        BUNDLE_GITHUB__COM="$(cat /run/secrets/github_token):x-oauth-basic" \
        bundle install
    ```

  - For `RAILS_MASTER_KEY`:

    ```text
    RUN --mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY \
        bundle exec rails db:schema:load
    ```

  - The fix should also suggest passing the secret with `docker buildx build --secret id=...,src=...`.
- Not `FixSafe` because the user's secret-injection mechanism (CI variable, vault, file path) is repo-specific.

**Representative positive control**

- `basecamp/fizzy/saas` is the only example in the corpus and is genuinely textbook: a `--mount=type=secret,id=GITHUB_TOKEN` + Bundler
  `BUNDLE_GITHUB__COM="$(cat /run/secrets/GITHUB_TOKEN):x-oauth-basic"` combination that fetches private gems, never persists the token, and never
  affects build cache key data.

### 4.5 Context-Aware Refinements

Every rule above is designed to work in pure Dockerfile-only mode (no build context observable). When tally is invoked through an entrypoint that
exposes a build context — `tally lint --context <dir>`, `bake.hcl`, or a `compose.yaml` whose service builds from a local context — the
`LintInput.InvocationContext` resolves to a directory and the rule can read project files via `internal/context.BuildContext`. The result is fewer
false positives, better fix suggestions, and ground-truth claims instead of heuristics.

This section maps each proposed rule to the specific build-context evidence that sharpens it. These are refinements, not replacements: every rule
must still work without context. The infrastructure is already in place (`InvocationContext.ContextRef().Value`, `BuildContext.ReadFile`,
`BuildContext.FileExists`, `BuildContext.PathExists`); these refinements should be implemented as the rules ship rather than as a separate phase.

#### Build-context files the namespace cares about

| File | What it tells us |
|---|---|
| `Gemfile` | Ruby version constraint (`ruby "3.4.0"`); private gem sources (`source "https://gems.example.com"`, `git: "git@github.com:..."`); presence of `:development`/`:test` groups; gems that have native extensions; whether `bootsnap`, `thruster`, or `puma` are project dependencies. |
| `Gemfile.lock` | Pre-resolved gem set; `BUNDLED WITH` (exact bundler version the lockfile was produced under); `RUBY VERSION` (the Ruby the lockfile resolved against); list of platforms baked into the lockfile (`PLATFORMS` block). |
| `.ruby-version` | Exact Ruby patch version for the project. Resolves Dockerfile `${RUBY_VERSION}` placeholders to a concrete value. |
| `.tool-versions` | asdf-style alternative to `.ruby-version`. |
| `config/credentials.yml.enc` | Confirms that the Rails app uses encrypted credentials and therefore actually needs `RAILS_MASTER_KEY` at runtime (and possibly at build, though `SECRET_KEY_BASE_DUMMY=1` removes the build need). |
| `config/credentials/<env>.yml.enc` | Multi-env credentials (Rails 6+); same signal, scoped. |
| `config/routes.rb` | Whether `/up` is actually mounted (the Rails 7.1+ default; users can remove or remount it). |
| `config/puma.rb` | Effective listening port and worker count (sharpens healthcheck URL and `WEB_CONCURRENCY` reasoning). |
| `config/database.yml` | DB adapter (`postgresql`/`mysql2`/`sqlite3`/`trilogy`) → maps to required apt packages (`libpq5`/`libmysqlclient`/`libsqlite3-0`). |
| `config/application.rb` / `config/environments/production.rb` | `config.require_master_key`, `config.force_ssl`, `config.eager_load`, `config.assets.compile`, asset host configuration. |
| `bin/rails`, `bin/docker-entrypoint`, `Rakefile`, `config.ru` | Confirms a Rails (vs. plain Sinatra/Hanami/CLI gem) project. |
| `vendor/cache/*.gem` | The project already ships a checked-in gem cache — `bundle install --local` is viable today. |
| `tmp/`, `log/`, `storage/`, `db/` | Which Rails state directories exist in this project (API-only apps may lack `storage/`). |

#### Per-rule context refinements

`tally/ruby/jemalloc-installed-but-not-preloaded`

- Build-context inputs: none meaningful.
- Refinement: this rule is purely a Dockerfile-level fact. Skip context-aware logic.

`tally/ruby/asset-precompile-without-dummy-key`

- Inputs: `config/credentials.yml.enc`, `config/credentials/<env>.yml.enc`, `Gemfile`, `Gemfile.lock`.
- Refinement (high confidence): if `config/credentials.yml.enc` does not exist *and* no `config/credentials/<env>.yml.enc` exists for the build env,
  the app does not use credentials at all and the rule can demote to `info` (the user does not need the dummy key in this build, but it is still
  good hygiene).
- Refinement (severity): if `Gemfile` shows Rails older than 7.1, the `SECRET_KEY_BASE_DUMMY` constant does not work — emit a different fix suggestion
  (set `SECRET_KEY_BASE` to a random ephemeral value or pass the master key via `RUN --mount=type=secret`).

`tally/ruby/bootsnap-precompile-without-j1`

- Input: `Gemfile.lock`.
- Refinement (suppress when irrelevant): if `Gemfile.lock` does not list `bootsnap` as a dependency, suppress the rule entirely. Some Dockerfiles
  copy generic templates that include `bootsnap precompile` even though the project has removed `bootsnap` from its Gemfile; the corpus shows this
  pattern and the rule should not fire.
- This is a pure false-positive reduction.

`tally/ruby/missing-bundle-deployment`

- Input: `Gemfile.lock`.
- Refinement (severity escalation): if `Gemfile.lock` does not exist in the context, the rule should *upgrade* — running `bundle install` in
  production without a lockfile is more dangerous, not less, because Bundler will resolve from `Gemfile` afresh on every build.
- Refinement (fix wording): if the Dockerfile already runs `bundle config set --local frozen 'true'`, mention that explicitly in the fix wording so
  the user understands `BUNDLE_DEPLOYMENT=1` is the strict superset.

`tally/ruby/missing-bundle-without-development`

- Input: `Gemfile`.
- Refinement (suppress when irrelevant): if `Gemfile` has no `group :development do` block (some library-style or ops-tool gems do not), the rule
  should skip — there is nothing to exclude.
- Refinement (fix accuracy): inspect `Gemfile` for both `group :development` and `group :test` and recommend `BUNDLE_WITHOUT="development:test"`
  when both groups exist; recommend `BUNDLE_WITHOUT="development"` otherwise. The corpus shows both shapes; the right one depends on the project.

`tally/ruby/redundant-bundler-install`

- Input: `Gemfile.lock` (specifically the `BUNDLED WITH` block).
- Refinement (high-confidence ground truth): the `BUNDLED WITH` line of `Gemfile.lock` records the exact Bundler version the lockfile was produced
  with. If that version is ≤ what the base `ruby:*` image ships, the `gem install bundler` is provably redundant (not just heuristically). If it is
  greater, the install is *legitimate* and the rule should suppress with a clear "lockfile requires Bundler X, base image ships Y" note.
- This refinement turns the rule from advisory to almost-`FixSafe` when the lockfile is observable.

`tally/ruby/leftover-bundler-cache`

- Input: `Gemfile.lock` (size only).
- Refinement (severity): when `Gemfile.lock` is observable and large (rough threshold: > 200 KB, which corresponds to ≈ 80+ gems), bump severity —
  the bloat impact scales with gem count.

`tally/ruby/prefer-bundler-cache-mount`

- Input: `Gemfile.lock`.
- Refinement (severity): when `Gemfile.lock` lists native-extension gems (look for `extensions:` blocks: `nokogiri`, `pg`, `mysql2`, `sqlite3`,
  `grpc`, `ffi`, `oj`, `bcrypt`, `nio4r`, `puma`, `rugged`, `protobuf`, `sassc`), bump severity. The rebuild-from-scratch cost of these gems is what
  cache mounts most clearly fix.

`tally/ruby/eol-ruby-version`

- Inputs: `.ruby-version`, `Gemfile` (the `ruby "..."` directive), `Gemfile.lock` (`RUBY VERSION` block).
- Refinement (high-confidence ground truth): when the Dockerfile uses `${RUBY_VERSION}` or `ARG RUBY_VERSION=...`, resolve the value against the
  observable `.ruby-version` file. The corpus shows several Dockerfiles that look fine (`ruby:${RUBY_VERSION}-slim`) but resolve to
  `ruby:2.7-slim` once the project's `.ruby-version` is consulted.
- Refinement (suppress conflicts): if `.ruby-version` and the Dockerfile's literal `FROM ruby:X` disagree, emit a separate violation —
  `ARG`/`.ruby-version` mismatch is itself a bug the rule can surface for free.

`tally/ruby/state-paths-not-writable-as-non-root`

- Inputs: `tmp/`, `log/`, `storage/`, `db/` (presence in the build context).
- Refinement (false-positive reduction): only require `chown` for directories that actually exist in the build context. API-only Rails apps
  legitimately omit `storage/`; gem-only repos legitimately omit all of them.
- Refinement (`.dockerignore` awareness): use `BuildContext.IsIgnored` to skip directories that `.dockerignore` excludes from the build context
  — those directories are created at runtime as empty (and in fact need the `chown` more than ever, but the recommendation has to be runtime-side
  rather than `--chown=`).

`tally/ruby/secrets-in-arg-or-env`

- Input: `config/credentials.yml.enc`.
- Refinement (suppress): if no credentials file exists, the rule still fires for `SECRET_KEY_BASE`/`RAILS_MASTER_KEY` declarations — the secret leak
  is a leak whether or not the project uses Rails credentials. Just note in the fix wording that the project does not appear to use credentials at
  all, which is itself worth investigating separately.

`tally/ruby/yjit-not-enabled-on-supported-runtime`

- Inputs: `.ruby-version`, `Gemfile`'s `ruby "..."` directive, `Gemfile.lock`'s `RUBY VERSION` block.
- Refinement (high-confidence gating): instead of relying on the Dockerfile's `FROM ruby:X` (which may use ARG indirection), determine YJIT support
  from the project's authoritative Ruby version. YJIT became production-ready in Ruby 3.2 and stable in 3.3.

`tally/ruby/deprecated-bundler-install-flags`

- Input: `Gemfile.lock` (the `BUNDLED WITH` block).
- Refinement (severity escalation): when `BUNDLED WITH` is Bundler 2.6+, the deprecated flags emit louder warnings on every CI run; bump severity.

`tally/ruby/prefer-gemfile-bind-mounts`

- Input: `Gemfile`, `Gemfile.lock` (presence and exact paths).
- Refinement (correctness gate): only emit the bind-mount fix when both `Gemfile` and `Gemfile.lock` are observable at the build-context root (or
  at the path the Dockerfile is `COPY`ing from). If either is missing or is in a non-default location, the bind-mount path needs to match — emit a
  fix that uses the actual observed path rather than guessing.
- Refinement (suppress when not applicable): if the Dockerfile is `COPY`ing `Gemfile` and `Gemfile.lock` because the build context is a tarball or
  a remote URL (`InvocationContext.ContextRef().Kind != "dir"`), the bind mount may not be available — suppress accordingly.

`tally/ruby/prefer-network-none-install`

- Inputs: `Gemfile.lock` (gem count), `vendor/cache/*.gem`.
- Refinement (severity escalation): when `vendor/cache/` already contains `.gem` files in the build context, the project has already opted into the
  cached-gem world — the rule's recommendation is essentially free at that point. Promote from "info" to "suggestion-with-confidence".
- Refinement (fix accuracy): when `vendor/cache/` is observable, the suggested fix should use the existing cache path rather than
  `--mount=type=cache`, because the user already has the artifacts on disk.

`tally/ruby/healthcheck-rails-up-endpoint`

- Inputs: `config/routes.rb`, `config/puma.rb`, `bin/rails`, `Gemfile`.
- Refinement (correctness gate): only suggest `/up` when `config/routes.rb` actually mounts the Rails health controller (i.e. the file contains
  `Rails.application.routes` and one of `health#show`, `rails/health`, or `up`, or the routes file has not removed the generator's default mount).
  If the user has explicitly removed `/up`, suggest `HEALTHCHECK NONE` or a different endpoint instead.
- Refinement (port accuracy): read `config/puma.rb` to extract the actual `port` directive; the corpus shows projects on `:8000`, `:3000`, `:80`,
  and `:3036`. The fix suggestion should use the observed port, not a hard-coded `3000`.
- Refinement (Rails-version gate): only suggest the `/up` endpoint when `Gemfile` shows `gem "rails", "~> 7.1"` or higher (or `Gemfile.lock` shows
  Rails 7.1+). For Rails 6/7.0, suggest a custom controller path or `HEALTHCHECK NONE`.
- Refinement (Bundler-only repo): suppress entirely when the project is plainly a gem (not a Rails app) — no `bin/rails`, no `config.ru`, no
  `Rakefile` declaring Rails tasks.

`tally/ruby/prefer-secret-mounts-for-build-credentials`

- Input: `Gemfile`.
- Refinement (high-confidence ground truth for `BUNDLE_GITHUB__COM`): scan `Gemfile` for `git:` or `github:` gem entries. If none exist and the
  Dockerfile sets `BUNDLE_GITHUB__COM`, that is itself suspicious — emit a separate diagnostic.
- Refinement (private gem source detection): scan `Gemfile` for `source "https://..."` lines that point anywhere except `https://rubygems.org`. Each
  such source name maps deterministically to a `BUNDLE_<HOST>__<TLD>` env var name; the rule can suggest the exact env var name in the fix rather
  than a generic placeholder.
- Refinement (suppress for purely-public projects): if `Gemfile` has no private sources and no git-sourced gems, suppress the
  `BUNDLE_*__*` half of the rule. The `RAILS_MASTER_KEY`/`GEM_HOST_API_KEY` halves still fire because they are not gem-source-linked.

#### Implementation note

These refinements add no new tally infrastructure — they are simple consumers of the existing
`LintInput.InvocationContext.ContextRef()` and `internal/context.BuildContext.ReadFile`/`FileExists`/`PathExists` surface. To stay aligned with the
direction in [07-context-aware-foundation.md](07-context-aware-foundation.md):

- The rule should always return a useful result without context (Dockerfile-only mode is the default).
- The rule should treat read errors (file exists in `.dockerignore`, file is not regular, parent dir is missing) as "no signal" rather than as bugs.
- File reads should be cached in `internal/facts/` whenever multiple Ruby rules will consult the same file. `Gemfile.lock` is the obvious shared
  artifact: at minimum, add `facts.RubyFacts` (a small typed view: `BundledWith`, `RubyVersion`, `HasNativeExtensions`, `HasGitGems`,
  `Sources`, `GemPresent(name)`) so that each individual rule does not re-read and re-parse `Gemfile.lock`.
- New `RubyFacts` should follow the existing `internal/facts/` pattern: derived facts only, no IO inside rule logic, populated by the lint pipeline
  before any rule runs.

#### Building the `Gemfile.lock` parser

There is no off-the-shelf parser that fits tally's needs. The Go ecosystem has two parsers in active use, and there is no tree-sitter grammar.

**Existing Go libraries** (both Apache 2.0):

- [`github.com/aquasecurity/trivy/pkg/dependency/parser/ruby/bundler`](https://github.com/aquasecurity/trivy/tree/main/pkg/dependency/parser/ruby/bundler)
  — Trivy's Bundler parser. Returns `[]ftypes.Package` and `[]ftypes.Dependency` with direct/indirect relationships. Strips platform suffixes (e.g.
  `nokogiri (1.13.6-x86_64-linux)` → `1.13.6`). Walks the `DEPENDENCIES` block to mark direct deps. **Does not expose `BUNDLED WITH`,
  `RUBY VERSION`, `PLATFORMS`, or `remote:` source URLs.**
- [`github.com/anchore/syft/syft/pkg/cataloger/ruby`](https://github.com/anchore/syft/tree/main/syft/pkg/cataloger/ruby) — Syft's Ruby cataloger.
  Walks `GEM`, `GIT`, `PATH`, and `PLUGIN SOURCE` sections, extracts packages by detecting 4-leading-space lines. Even smaller surface area than
  Trivy; does not expose `BUNDLED WITH`, `RUBY VERSION`, native-extension hints, or source URLs either.

Both libraries are package-extractors built for SBOM and CVE workflows. They give us "the gem set" but not the metadata fields the rules above
need (`BUNDLED WITH` for `redundant-bundler-install`, `RUBY VERSION` for `eol-ruby-version`, `remote:` source URLs for
`prefer-secret-mounts-for-build-credentials`, native-extension presence for `prefer-bundler-cache-mount` severity escalation, etc.).

**Tree-sitter:** No grammar for `Gemfile.lock` exists. The closest artifact is
[`hmarr/gemfile-lock-tmlanguage`](https://github.com/hmarr/gemfile-lock-tmlanguage), a TextMate grammar for syntax highlighting only — it has no AST
output and is not usable as a parser. The
[`tree-sitter/tree-sitter-ruby`](https://github.com/tree-sitter/tree-sitter-ruby) grammar parses Ruby source code (so it would work on `Gemfile`,
not `Gemfile.lock`).

**Recommendation:** write a small purpose-built parser, scoped to the fields `RubyFacts` exposes.

Why this is justified:

- The format is well-specified and simple. Bundler's own
  [`Bundler::LockfileParser`](https://github.com/rubygems/rubygems/blob/master/bundler/lib/bundler/lockfile_parser.rb) is a single ~250-line file
  that walks line-by-line by leading-whitespace count (2/4/6 spaces) and section keyword (`GEM`, `GIT`, `PATH`, `PLUGIN SOURCE`, `DEPENDENCIES`,
  `PLATFORMS`, `RUBY VERSION`, `BUNDLED WITH`, `CHECKSUMS`). A Go port of just the fields tally needs is roughly ~150 lines.
- Pulling in Trivy or Syft as a dependency for a parser we would still have to extend (for `BUNDLED WITH`, `RUBY VERSION`, source URLs) buys very
  little over writing the parser ourselves. Trivy in particular has a heavy transitive dependency footprint that is undesirable in a single-purpose
  linter.
- The existing tally pattern is to keep `internal/facts/` parsers small and focused. `internal/facts/ruby/lockfile.go` would follow the same shape
  as the existing `internal/runmount/`, `internal/heredoc/`, and `internal/registry/` parsers.

Suggested package layout:

```text
internal/facts/ruby/
    facts.go            // RubyFacts type + populate-from-context entrypoint
    facts_test.go
    lockfile.go         // Gemfile.lock parser (~150 lines)
    lockfile_test.go
    gemfile.go          // Gemfile parser for `ruby "..."`, `source`, `git:`, `github:` lines
    gemfile_test.go
    ruby_version.go     // .ruby-version + .tool-versions reader
    testdata/
        gemfile.lock.basic
        gemfile.lock.with-git-source
        gemfile.lock.with-private-source
        gemfile.lock.with-native-extensions
        gemfile.lock.no-bundled-with-block      # legacy Bundler 1.x lockfiles
```

Suggested `RubyFacts` shape:

```go
package ruby

// LockfileFacts is the typed projection of Gemfile.lock that Ruby rules consume.
// All fields are zero-valued when Gemfile.lock is not observable.
type LockfileFacts struct {
    BundledWith   string            // value of BUNDLED WITH
    RubyVersion   string            // value of RUBY VERSION (may be empty)
    Platforms     []string          // PLATFORMS block
    Sources       []string          // remote: URLs from GEM/GIT/PATH blocks
    DirectDeps    map[string]string // gem name -> version constraint, from DEPENDENCIES
    Specs         map[string]string // gem name -> exact version, from specs blocks
    HasGitGems    bool              // any GIT block present
    HasPathGems   bool              // any PATH block present
    NativeExtGems []string          // gems with native extensions (heuristic: known list)
}

// GemfileFacts is the typed projection of Gemfile that complements LockfileFacts.
type GemfileFacts struct {
    RubyConstraint string   // ruby "..." directive, if present
    Sources        []string // source "..." lines (rubygems URLs)
    GitGems        []string // gem entries with git: or github: options
    HasDevGroup    bool     // group :development do
    HasTestGroup   bool     // group :test do
}

// RubyFacts bundles all observable Ruby project state for the build context.
type RubyFacts struct {
    Lockfile     *LockfileFacts
    Gemfile      *GemfileFacts
    RubyVersion  string  // resolved from .ruby-version, .tool-versions, or Lockfile.RubyVersion
}
```

Suggested boundary conditions:

- Treat absent files as `nil` fields, not as errors. Rules check for `nil` and degrade to Dockerfile-only mode.
- Treat malformed lockfiles as `nil` fields too — there is no value in returning a partially-populated struct that some rules might trust and
  others might not.
- The "native-extension gems" detection should start with a curated list of well-known extension gems
  (`nokogiri`, `pg`, `mysql2`, `sqlite3`, `grpc`, `ffi`, `oj`, `bcrypt`, `nio4r`, `puma`, `rugged`, `protobuf`, `sassc`, `eventmachine`,
  `unf_ext`, `racc`, `bigdecimal`). A truly authoritative answer requires the `.gemspec`'s `extensions:` array, which is not in `Gemfile.lock`;
  the curated-list approximation is acceptable because the rule that consumes it is severity-only, not correctness-critical.

---

## 5. Recommended Initial Implementation Order

Ship these first:

1. `tally/ruby/jemalloc-installed-but-not-preloaded`
2. `tally/ruby/asset-precompile-without-dummy-key`
3. `tally/ruby/bootsnap-precompile-without-j1`
4. `tally/ruby/missing-bundle-deployment`
5. `tally/ruby/missing-bundle-without-development`
6. `tally/ruby/redundant-bundler-install`

Ship these next:

1. `tally/ruby/leftover-bundler-cache`
2. `tally/ruby/prefer-bundler-cache-mount`
3. `tally/ruby/eol-ruby-version`
4. `tally/ruby/state-paths-not-writable-as-non-root`

Ship these next:

1. `tally/ruby/secrets-in-arg-or-env`
2. `tally/ruby/yjit-not-enabled-on-supported-runtime`
3. `tally/ruby/deprecated-bundler-install-flags`

Ship the educational batch last, advisory-only:

1. `tally/ruby/prefer-gemfile-bind-mounts`
2. `tally/ruby/healthcheck-rails-up-endpoint`
3. `tally/ruby/prefer-secret-mounts-for-build-credentials`
4. `tally/ruby/prefer-network-none-install`

Why this order:

- The first batch has the strongest mix of:
  - high corpus signal (every one of these has ≥ 50 % miss rate among the relevant population),
  - low ambiguity (each fix is a one- or two-line patch the user can verify),
  - direct alignment with the Rails generator template (so users who run `rails new` already get the correct result).
- The second batch is structurally larger or version-aware and benefits from a data-driven implementation (EOL table for Ruby versions).
- The third batch is correct but has lower corpus base rate (`secrets-in-arg-or-env`, `deprecated-bundler-install-flags`) or is performance-only
  (`yjit-not-enabled-on-supported-runtime`).
- The fourth batch is intentionally educational. These rules promote modern features that already exist in the ecosystem but have ~0 % corpus
  adoption. They are advisory by design and should never be promoted to `error` severity. Their value is teaching users about a feature at the
  moment of relevance, not enforcing compliance.

---

## 6. Explicit Non-Goals for This Draft

These ideas came up during research but should stay out of the first Ruby batch:

- "Pin a specific Ruby patch version" rules
  - the corpus is split across `ruby:3.4`, `ruby:3.4.x`, `ruby:3.4-slim`, `ruby:3.4-bookworm`, etc.; turning that into a lint rule that adds value
    over generic `tally/prefer-pinned-base-image` is hard.
- "Use Thruster" or "use a specific Rails server" rules
  - Thruster is Basecamp's choice, not a universal recommendation; Puma directly is fine.
- Sidekiq/Puma collocation rules
  - real signal exists, but detection requires multi-process awareness that is not Dockerfile-only.
- Yarn vs Bun vs npm preference rules
  - the Rails generator templates support all three; this is not a Ruby-namespace concern.
- `RAILS_LOG_TO_STDOUT` rules
  - in Rails 5/6 this was load-bearing; Rails 7.1+ logs to STDOUT by default in production when the `dockerfile_log_to_stdout?` template branch
    fires. A linter rule cannot reliably tell which Rails version is in use without `Gemfile.lock` access. Defer until tally has a fact source for
    Rails version.
- Generic non-root `USER` rules
  - covered by [34-user-instructions.md](34-user-instructions.md) and the `tally/stateful-root-runtime` family.
- `MALLOC_ARENA_MAX=2` advice
  - real corpus signal (8 / 196), but it is a glibc-allocator workaround that is mostly subsumed by adopting jemalloc; making it its own rule risks
    contradicting the jemalloc rule above. Track as a future item only if jemalloc is explicitly opted out.
- Asset precompile without `node_modules` cleanup
  - real signal in the corpus; better implemented as part of generic image-bloat lint or as a future rule
    `tally/ruby/node-modules-leaks-into-runtime` once the namespace has at least one foothold rule.

---

## 7. Bottom Line

The Ruby/Rails container ecosystem has a rare combination of properties for lint design:

- a single canonical reference (`Dockerfile.tt` from the Rails generator) that real apps measurably copy from,
- a small set of well-documented production knobs (`BUNDLE_DEPLOYMENT`, `BUNDLE_WITHOUT`, `LD_PRELOAD`, `SECRET_KEY_BASE_DUMMY`,
  `RUBY_YJIT_ENABLE`, `bootsnap precompile -j 1`),
- and a very long tail of "almost but not quite the canonical pattern" Dockerfiles that drift in the same six or seven specific ways.

That is exactly the environment where Tally adds disproportionate value: every rule above corresponds to a one- or two-line patch the user can
verify against the generator template, and almost every rule has at least 50 % miss rate against the real corpus.
