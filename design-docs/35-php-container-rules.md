# 35. PHP Container Rules (`tally/php/*`)

**Status:** Draft

**Design Focus:** Propose a dedicated `tally/php/*` namespace for PHP-in-container rules that match how the PHP community actually ships
applications in 2026: Composer-heavy builds, official `php` images, frequent Apache/FPM runtime images, occasional Node asset builds, and a long tail
of copy-pasted Dockerfile patterns.

---

## 1. Decision

Introduce a dedicated `tally/php/*` namespace with an initial batch of **9 PHP-oriented rules**.

The namespace should focus on:

1. Composer install patterns that are clearly production-hostile in Dockerfiles.
2. BuildKit-aware dependency-stage patterns that avoid copying throwaway Composer manifests into image layers.
3. Runtime image mistakes repeatedly seen in PHP app images: Xdebug in the final image, missing OPcache, root runtime, and leftover extension build
   dependencies.
4. Rules that teach a better PHP container pattern without duplicating generic Docker hygiene.
5. Rules with a credible fix story:
   - `FixSafe` when the change is a narrow flag or instruction update
   - `FixSuggestion` when the right fix is structural but obvious
   - `FixUnsafe` when stage splitting or repo-specific refactors are required

The namespace should **not** absorb generic Docker advice such as broad `apt-get` cleanup, generic multi-stage guidance, or generic non-root rules
for every ecosystem. The rules here should stay anchored to PHP-specific signals: Composer, `docker-php-ext-*`, OPcache, Xdebug, and official PHP base
images.

---

## 2. Ground Truth

### 2.1 Corpus methodology

Discovery started from public GitHub repositories using GitHub code search and repository/file lookup via MCP. Representative Dockerfiles were pulled
directly through GitHub MCP during exploration, then the discovered public repositories were materialized locally to build a larger corpus for
pattern analysis.

The raw corpus contained **541 Dockerfile/Containerfile variants across 41 repositories**.

That raw set is useful, but it is dominated by a few image-template repositories:

| Repository | Dockerfile-ish files |
|---|---:|
| `webdevops/Dockerfile` | 149 |
| `khs1994-docker/lnmp` | 134 |
| `laradock/laradock` | 84 |
| `shinsenter/php` | 53 |
| `sclorg/s2i-php-container` | 22 |

To avoid one template family drowning out the rest, this draft uses two narrower lenses:

1. **Balanced corpus:** **209 files**
   - Large template repos were capped.
   - Good for "what the PHP container ecosystem keeps copy-pasting".
2. **Application-heavy subset:** **45 files from 27 repos**
   - Excludes most base-image/template factories.
   - Good for "what shipped PHP services and apps actually do".

Representative application repos in the subset include:

- `pterodactyl/panel`
- `firefly-iii/firefly-iii`
- `Kovah/LinkAce`
- `FOSSBilling/FOSSBilling`
- `thorsten/phpMyFAQ`
- `jorge07/symfony-7-es-cqrs-boilerplate`
- `CodelyTV/php-ddd-example`
- `bnw/firefly-iii-fints-importer`
- `iceburgcrm/iceburgcrm`
- `codeforeurope/docker-attendize`

### 2.2 External guidance that aligns with the corpus

The best "official" guidance was unusually consistent with the corpus findings:

- Docker's PHP guide uses a dedicated `composer:lts` dependency stage, mounts or copies `composer.json` and `composer.lock`, runs
  `composer install --no-dev --no-interaction`, switches the final stage to `php.ini-production`, and runs the runtime as `www-data`.
  Source: [Docker PHP guide: develop](https://docs.docker.com/guides/php/develop/)
- Composer documents `--no-dev`, `--no-interaction`, and `--optimize-autoloader`, and says `--optimize-autoloader` is especially recommended for
  production. It also explains that `composer.lock` makes installs deterministic.
  Sources:
  [Composer CLI docs](https://getcomposer.org/doc/03-cli.md#install-i),
  [Composer autoloader optimization](https://getcomposer.org/doc/articles/autoloader-optimization.md)
- Symfony deployment docs recommend `composer install --no-dev --optimize-autoloader` in production.
  Source: [Symfony deployment docs](https://symfony.com/doc/current/deployment.html)
- Laravel Sail documents Xdebug as a development-environment concern rather than a production runtime default.
  Source: [Laravel Sail](https://laravel.com/docs/12.x/sail)
- The PHP manual states that OPcache improves performance by storing precompiled bytecode in shared memory.
  Source: [PHP OPcache manual](https://www.php.net/manual/en/book.opcache.php)
- Community-maintained production PHP image docs also push versioned images, non-root runtime, and production image hardening. Source:
  [serversideup/php production packaging](https://serversideup.net/open-source/docker-php/docs/deployment-and-production/packaging-your-app-for-deployment)

### 2.3 Representative examples worth preserving

Good patterns worth teaching:

- `jorge07/symfony-7-es-cqrs-boilerplate:etc/artifact/Dockerfile`
  - Copies Composer manifests first.
  - Runs `composer install --no-dev --no-interaction --optimize-autoloader`.
  - Uses multi-stage separation.
- `Kovah/LinkAce:resources/docker/dockerfiles/release-multiplatform.Dockerfile`
  - Uses a separate asset build stage.
  - Copies Composer from `composer:latest`.
  - Runs final runtime as `www-data`.
- `thorsten/phpMyFAQ:Dockerfile`
  - Uses separate stages.
  - Copies Composer from a Composer image.
  - Copies `composer.json` and `composer.lock` first.

Common copy-paste problems worth catching:

- `pterodactyl/panel:Dockerfile`
  - `COPY . ./` before `composer install`
  - curl-installs Composer
  - no explicit unprivileged runtime user
- `bnw/firefly-iii-fints-importer:Dockerfile`
  - broad copy before `composer install`
  - curl-installs Composer
  - final-stage extension build deps likely remain
- `FOSSBilling/FOSSBilling:Dockerfile`
  - runtime-stage extension build/install work
  - no explicit unprivileged runtime user
- `CodelyTV/php-ddd-example:Dockerfile`
  - Xdebug in a non-dev image path

---

## 3. Corpus Findings

### 3.1 High-signal patterns

The table below uses the balanced corpus as the main baseline, with the app subset called out because it matters more than template farms.

| Finding | Balanced corpus | App subset |
|---|---:|---:|
| Multi-stage Dockerfiles | 31 / 209 | 13 / 45 |
| Dockerfiles running `composer install` | 20 / 209 | 13 / 45 |
| `composer install` with `--no-dev` | 14 / 20 | 9 / 13 |
| `composer install` with `--no-interaction` | 6 / 20 | 6 / 13 |
| `composer install` with `--optimize-autoloader` | 6 / 20 | 6 / 13 |
| Composer pulled from `composer` image | 22 / 209 | 13 / 45 |
| Composer installed via curl/bootstrap script | 19 / 209 | 7 / 45 |
| Composer cache anti-pattern (`COPY . .` before install) | 5 / 209 | 5 / 45 |
| Composer manifest-first pattern | 8 / 209 | 4 / 45 |
| `docker-php-ext-install` present | 41 / 209 | 23 / 45 |
| `pecl install` present | 24 / 209 | 11 / 45 |
| Leftover build-deps signal after extension build | 17 / 209 | 11 / 45 |
| Xdebug installed | 29 / 209 | 7 / 45 |
| Xdebug in the final image path | 26 / 209 | 7 / 45 |
| OPcache signal present | 46 / 209 | 8 / 45 |
| Any explicit `USER` | 40 / 209 | 5 / 45 |

### 3.2 What this means

The PHP container ecosystem is not confused about everything.

It already broadly understands:

- Composer belongs in the build story.
- Official `php` images plus `docker-php-ext-*` helpers are the dominant baseline.
- Multi-stage builds are normal for serious apps, even if not universal.

But it still copy-pastes the same mistakes:

1. Production Composer installs are often missing one or more of `--no-dev`, `--no-interaction`, and `--optimize-autoloader`.
2. Too many Dockerfiles still do `COPY . .` before `composer install`, instead of bind-mounting Composer manifests into the dependency stage or at
   least copying them separately.
3. Curl-installing Composer remains common even though a dedicated Composer image is widely used and officially documented.
4. PHP extension build dependencies are often left in the final image.
5. Xdebug frequently leaks into the final image instead of staying in dev-only stages.
6. OPcache is underused in runtime images despite clear production benefit.
7. PHP app containers frequently still run as root by omission.

---

## 4. Proposed Rules

This draft proposes **9 rules**. The first five are the highest-value implementation batch.

### 4.1 Batch 1: should ship first

#### `tally/php/composer-no-dev-in-production`

**Problem**

Production/runtime stages running `composer install` without `--no-dev` ship unnecessary dev dependencies, extra packages, and a larger attack
surface.

**Why this is grounded**

- Docker's PHP guide uses `composer install --no-dev --no-interaction` for production dependencies.
- Symfony and Composer's own docs both point to the same production pattern.
- In the app subset, only **9 of 13** `composer install` usages included `--no-dev`.

**Trigger shape**

- A runtime-like stage runs `composer install`.
- The stage does not look like `dev`, `development`, `test`, `testing`, or `ci`.
- No `--no-dev` flag and no equivalent env/config signal is present.

**Guardrails**

- Skip explicit development/test stages.
- Skip stages that only prepare a dev image target.
- Skip `composer update`, because that is a separate rule discussion.

**Fix story**

- Usually `FixSafe`: append `--no-dev`.

**Representative misses**

- `emaijala/MLInvoice`
- `codeforeurope/docker-attendize`
- `pterodactyl/panel` does this correctly; use it as a positive control for `--no-dev`.

#### `tally/php/composer-optimize-autoloader`

**Problem**

Production Composer installs without autoloader optimization leave request-time performance on the table.

**Why this is grounded**

- Composer says `--optimize-autoloader` is especially recommended for production.
- Symfony deployment docs and Composer's own CLI docs both explicitly support it.
- In the app subset, only **6 of 13** `composer install` usages had an optimization flag.

**Trigger shape**

- A production/runtime-like stage runs `composer install`.
- None of these signals appear:
  - `--optimize-autoloader`
  - `-o`
  - `--classmap-authoritative`
  - `composer dump-autoload -o` later in the same stage
  - project config that clearly enables optimized autoloading during build

**Guardrails**

- Skip explicit dev/test stages.
- Treat `--classmap-authoritative` as satisfying the rule.

**Fix story**

- Usually `FixSafe`: add `--optimize-autoloader`.

**Representative misses**

- `bnw/firefly-iii-fints-importer`
- `pelican-dev/panel`
- `codeforeurope/docker-attendize`

#### `tally/php/prefer-composer-manifest-bind-mounts`

**Problem**

Many PHP Dockerfiles copy `composer.json` and `composer.lock` into a dependency stage only to run `composer install`. In modern BuildKit-based
Dockerfiles, that data can be bind-mounted into the `RUN` instruction instead of being baked into an intermediate layer at all.

**Why this is grounded**

- Docker's PHP guide mounts `composer.json` and `composer.lock` directly into the Composer stage with `RUN --mount=type=bind`.
- Tally already parses `RUN --mount` options via `internal/runmount`, so this is technically feasible with the current analyzer.
- Tally's generic `tally/prefer-package-cache-mounts` rule already understands `composer install` and the Composer cache directory, so this PHP rule
  should focus on manifest transport and dependency-stage structure, not duplicate cache-mount advice.
- In the app subset, the bad broad-copy-before-install pattern appeared in **5 of 45** files; only **4 of 45** used an obvious manifest-first
  pattern, and even fewer used the newer bind-mount approach.

**Trigger shape**

- A non-Windows stage runs `composer install`.
- The stage does not already bind-mount `composer.json` and `composer.lock` into the `RUN`.
- Confidence tiers:
  - high confidence: broad `COPY . .` or `ADD . .` occurs before `composer install`
  - medium confidence: stage copies only Composer manifests first, but still bakes them into a layer instead of using bind mounts

**Guardrails**

- Skip Windows stages, because `RUN --mount` is not a viable recommendation there.
- Treat BuildKit bind mounts of Composer manifests as compliant.
- Keep targeted `COPY composer.json composer.lock ./` as a valid fallback pattern even if the rule recommends mounts.
- If Tally wants a lower-noise first release, scope the initial implementation to the high-confidence broad-copy case and mention bind mounts in the
  fix text.

**Fix story**

- `FixSuggestion`, not `FixSafe`: switching from `COPY` to bind mounts is structural and may require removing now-unnecessary copied files from the
  stage.

**Representative misses**

- `pterodactyl/panel`
- `bnw/firefly-iii-fints-importer`
- `iceburgcrm/iceburgcrm`

#### `tally/php/remove-build-deps-after-extension-build`

**Problem**

Using `docker-php-ext-install` or `pecl install` in the final stage often leaves behind package-manager and compiler dependencies that are only
needed to build extensions.

**Why this is grounded**

- This is one of the most repeated "real app" mistakes: **11 of 45** app Dockerfiles showed a leftover build-deps signal.
- The Docker guide's dev/prod stage split points in the same direction: build what you need, keep the final runtime lean.
- BuildKit cache mounts are a complementary optimization here, but they solve a different problem: faster rebuilds for package downloads, not final
  image bloat.

**Trigger shape**

- A final/runtime-like stage uses one or more of:
  - `docker-php-ext-install`
  - `docker-php-ext-enable`
  - `pecl install`
  - extension build tooling such as `$PHPIZE_DEPS`
- The same stage also installs obvious build deps via `apt`, `apk`, or similar.
- No cleanup/removal signal is present, and the work is not isolated to an earlier builder stage.

**Guardrails**

- Skip non-final builder stages.
- Skip stages whose only purpose is extension compilation for later copy-out.
- Do not fire on single-package runtime dependencies that are clearly meant to stay.

**Interaction with cache/tmpfs mounts**

- Cache mounts are relevant around extension builds when the same `RUN` also does `apt`, `apt-get`, or `apk` work to install build dependencies.
- That optimization belongs primarily to generic `tally/prefer-package-cache-mounts`, not this PHP-specific rule.
- A future enhancement could teach the generic cache-mount rule more PHP awareness, for example when `docker-php-ext-install` or `pecl install`
  clearly appears alongside package-manager commands.
- `tmpfs` is a weaker lint target. It can provide ephemeral scratch space, but unlike cache mounts it does not improve cross-build reuse, and there is
  no equally stable, ecosystem-wide target path that Tally can recommend with high confidence.

**Fix story**

- Usually `FixSuggestion`: remove the build deps or split extension compilation into a builder stage.

**Representative misses**

- `FOSSBilling/FOSSBilling`
- `bnw/firefly-iii-fints-importer`
- `codeforeurope/docker-attendize`
- `CodelyTV/php-ddd-example`

#### `tally/php/no-xdebug-in-final-image`

**Problem**

Xdebug belongs in development workflows, not in production runtime images.

**Why this is grounded**

- Docker's PHP guide splits development and final stages and uses production configuration in the final stage.
- Laravel Sail treats Xdebug as a development-environment feature.
- In the balanced corpus, **26 of 29** Xdebug-using Dockerfiles left it in the final image path.

**Trigger shape**

- The final/runtime stage installs, enables, or configures Xdebug.
- The stage is not explicitly named or targeted as development/test/debug.

**Guardrails**

- Skip `development`, `dev`, `debug`, and `test` stages.
- Skip Dockerfiles where the Xdebug stage is clearly non-final and never selected for production.

**Fix story**

- Usually `FixSuggestion`: move Xdebug into a dedicated dev stage or build target.

**Representative misses**

- `CodelyTV/php-ddd-example`
- `alexjustesen/speedtest-tracker`
- `yii-starter-kit/yii2-starter-kit`

### 4.2 Batch 2: valuable follow-up rules

#### `tally/php/composer-no-interaction-in-build`

**Problem**

Docker builds should not rely on interactive Composer prompts.

**Tracking issue**

- Multi-fix LSP support needed for the best IDE UX:
  [wharflab/tally#384](https://github.com/wharflab/tally/issues/384)

**Why this is grounded**

- Composer documents `--no-interaction` as a global option and exposes `COMPOSER_NO_INTERACTION`.
- Docker's PHP guide uses `composer install --no-interaction`.
- In the app subset, only **6 of 13** `composer install` invocations used `--no-interaction`.

**Trigger shape**

- A Dockerfile `RUN` executes `composer install` or other interactive Composer commands.
- No `--no-interaction`, `-n`, or `COMPOSER_NO_INTERACTION` signal is present.

**Guardrails**

- Limit the first version to `composer install`.
- Skip lines that already set `COMPOSER_NO_INTERACTION=1`.
- Prefer stage-local `ENV COMPOSER_NO_INTERACTION=1` as the suggested fix when the stage runs one or more Composer commands, because Composer
  documents that env var as equivalent to passing `--no-interaction` to every command.
- Keep direct `--no-interaction` insertion as a fallback fix strategy for cases where adding a stage-level `ENV` would be awkward or would broaden
  scope too much.

**Fix story**

- Preferred fix: `FixSuggestion` that inserts `ENV COMPOSER_NO_INTERACTION=1` into the stage before the first `RUN` instruction in that stage.
- Why this is preferable:
  - it covers multiple Composer invocations in the same stage,
  - it also covers nested Composer executions triggered from scripts or wrappers that Tally cannot see directly from command parsing,
  - it avoids brittle edits inside complex shell pipelines.
- Why the insertion point is early:
  - if nested Composer execution is part of the justification, inserting the `ENV` only before the first visible Composer command is too narrow,
  - placing it before the first `RUN` makes the stage-level intent explicit and avoids under-fixing hidden Composer entrypoints.
- Fallback fix: inject `--no-interaction` into the specific `composer install` command when a narrow command-line patch is clearly easier than a
  structural stage edit.

**Representative misses**

- `pterodactyl/panel`
- `bnw/firefly-iii-fints-importer`
- `iceburgcrm/iceburgcrm`

#### `tally/php/prefer-composer-stage`

**Problem**

Many PHP Dockerfiles still curl-install Composer inside the application image even though the ecosystem already has a well-known Composer image and
copy-from-stage pattern.

**Why this is grounded**

- Docker's guide uses `FROM composer:lts`.
- The corpus already shows strong adoption of the better pattern: **22 of 209** files copy Composer from a Composer image, while **19 of 209** still
  install it via curl/bootstrap script.
- In the app subset, **7 of 45** still curl-install Composer.

**Trigger shape**

- The Dockerfile downloads Composer via `curl`, `wget`, or `php -r "copy(...getcomposer...)"`.

**Guardrails**

- This should be advisory, not a hard correctness rule.
- Allow either:
  - `FROM composer:*` as a dependency stage, or
  - `COPY --from=composer:* /usr/bin/composer ...`

**Fix story**

- Usually `FixSuggestion`: switch to a Composer stage or copy the binary from `composer`.

**Representative misses**

- `pterodactyl/panel`
- `bnw/firefly-iii-fints-importer`
- `codeforeurope/docker-attendize`

#### `tally/php/enable-opcache-in-production`

**Problem**

Runtime PHP web images frequently omit OPcache even though it is one of the most obvious production wins.

**Why this is grounded**

- The PHP manual says OPcache improves performance by caching precompiled script bytecode in shared memory.
- Composer's autoloader docs also note that optimized class maps benefit from OPcache.
- In the app subset, only **8 of 45** Dockerfiles showed an OPcache signal.

**Trigger shape**

- A final/runtime-like stage appears to be a long-running PHP web runtime:
  - official `php:*apache*`
  - official `php:*fpm*`
  - obvious nginx+php-fpm or Apache runtime stage
- No OPcache install/enable/config signal is present.

**Guardrails**

- Skip CLI-only or one-shot utility images.
- Prefer official-`php`-image gating for the first implementation.
- Treat explicit OPcache config or enablement as compliant.

**Fix story**

- Usually `FixSuggestion`: install or enable OPcache in the production runtime path.

**Representative misses**

- `FOSSBilling/FOSSBilling`
- `pterodactyl/panel`
- `shawon100/RUET-OJ`

#### `tally/php/prefer-non-root-runtime`

**Problem**

Too many PHP app images still run as root by omission, especially when based on official `php` images.

**Why this is grounded**

- Docker's PHP guide ends the final stage with `USER www-data`.
- ServersideUp's production docs explicitly recommend running as a non-root user.
- In the app subset, only **5 of 45** Dockerfiles set any explicit `USER`; only **4** set `USER www-data`.

**Interoperability with USER design work**

- This proposal must be read together with
  [34-user-instructions.md](34-user-instructions.md).
- That document explicitly rejects a generic "every final stage must contain `USER`" rule as too noisy.
- The PHP rule here is therefore intentionally narrower:
  - scoped to official `php`-image runtimes and obvious derivatives,
  - advisory rather than the whole security story,
  - and justified by strong ecosystem-specific defaults (`www-data`, Apache/FPM app images, and Docker's own PHP guide).
- It should also inherit the USER document's semantic model:
  - `USER` affects later `RUN` and runtime identity,
  - `USER` does not fix `COPY` / `ADD` ownership,
  - runtime hardening and runtime user overrides are outside Dockerfile-only observability.
- Future overlap is expected with the more general USER-rule direction proposed in that document, especially:
  - `tally/stateful-root-runtime`
  - `tally/user-created-but-never-used`
  - `tally/copy-after-user-without-chown`
  - `tally/workdir-created-under-wrong-user`
- If those generic USER rules ship, this PHP rule should remain a low-noise, PHP-specific front door rather than become a competing generic policy.
- In particular, it should suppress when Tally can already prove the base image defaults to non-root, matching the `BaseDefaultsToNonRoot` direction
  proposed in `34-user-instructions.md`.

**Trigger shape**

- A final/runtime-like stage based on an official PHP image path has no `USER`, or explicitly sets `USER root`.

**Guardrails**

- Limit the first version to official `php` images and obvious derivatives.
- Skip builder stages and images that are clearly tooling containers rather than app runtimes.
- Accept any non-root user, not only `www-data`.

**Fix story**

- Usually `FixSuggestion`: set `USER www-data` or the repo's existing unprivileged runtime user.

**Representative misses**

- `pterodactyl/panel`
- `FOSSBilling/FOSSBilling`
- `bnw/firefly-iii-fints-importer`

---

## 5. Recommended Initial Implementation Order

Ship these first:

1. `tally/php/composer-no-dev-in-production`
2. `tally/php/composer-optimize-autoloader`
3. `tally/php/prefer-composer-manifest-bind-mounts`
4. `tally/php/remove-build-deps-after-extension-build`
5. `tally/php/no-xdebug-in-final-image`

Ship these next:

1. `tally/php/composer-no-interaction-in-build`
2. `tally/php/prefer-composer-stage`
3. `tally/php/enable-opcache-in-production`
4. `tally/php/prefer-non-root-runtime`

Why this order:

- The first batch has the best mix of corpus signal, low ambiguity, and easy explanation to users.
- The second batch is still useful, but each rule wants slightly tighter gating to avoid noise.

---

## 6. Explicit Non-Goals for This Draft

These ideas came up during research but should stay out of the first PHP batch:

- Apache-only rewrite rules
  - too framework-specific unless tied to stronger repo/facts signals
- "use `php.ini-production`" as a standalone rule
  - good advice, but initial Dockerfile-only detection is too noisy
- Node-in-final-image rules under `tally/php/*`
  - real signal exists, but the rule is not PHP-specific enough and belongs in a broader namespace if implemented
- generic `apt-get`/`apk` hygiene
  - not unique to PHP

---

## 7. Appendix: Composer Distribution via `codewithkyrian/platform-package-installer`

This is outside the scope of `tally/php/*` rule implementation, but it is highly relevant for PHP ecosystem adoption.

If Tally should become installable via `composer require --dev`, the most credible "native PHP" path is to publish a Composer package that uses
[`codewithkyrian/platform-package-installer`](https://packagist.org/packages/codewithkyrian/platform-package-installer) to select the correct
platform archive at install time.

Why this approach fits Tally:

- it matches Tally's existing release matrix and GitHub release asset flow,
- it avoids a first-run downloader UX,
- it feels closer to ordinary Composer package installation than a custom bootstrap command,
- and it gives Packagist users a predictable `vendor/bin/tally` entrypoint.

Important tradeoffs:

- it is still a Composer plugin, so root projects must allow `codewithkyrian/platform-package-installer` via `config.allow-plugins`,
- the plugin currently requires PHP 8.1+, which becomes an installation-time requirement even though Tally itself is a Go binary,
- and the generated platform URLs must be correct, otherwise Composer falls back to the source archive, which is not enough on its own for a binary
  distribution flow.

Recommended package shape:

1. Add a dedicated Composer packaging root, for example `packaging/composer/`, containing:
   - `composer.json`
   - `platforms.yml`
   - `README.md`
   - `LICENSE`
   - `NOTICE`
   - `bin/tally`
   - `bin/tally.exe`
2. Make the Composer package use `"type": "platform-package"` and require `codewithkyrian/platform-package-installer`.
3. Declare `bin` entries so Composer exposes `vendor/bin/tally`.
4. Treat the checked-in package root as a template. Each released platform archive should contain a package root plus the correct executable, not just
   the raw Tally release tarball.

The last point matters: Tally's current release archives only contain the compiled binary plus `LICENSE` and `NOTICE`. That is enough for direct
binary downloads, but it is not enough for Composer package installation because Composer expects package metadata and bin declarations inside the
dist archive.

Recommended `platforms.yml` targets:

```yaml
linux-x86_64:
linux-arm64:
darwin-x86_64:
darwin-arm64:
windows-x86_64:
windows-arm64:
```

Recommended release-asset strategy:

- Keep the existing public release assets unchanged.
- Publish additional Composer-specific assets alongside them, for example:
  - `tally-composer-linux-x86_64.tar.gz`
  - `tally-composer-linux-arm64.tar.gz`
  - `tally-composer-darwin-x86_64.tar.gz`
  - `tally-composer-darwin-arm64.tar.gz`
  - `tally-composer-windows-x86_64.zip`
  - `tally-composer-windows-arm64.zip`
- Generate `extra.platform-urls` in the Composer package with a custom template rather than trying to reuse Tally's existing end-user binary archive
  names.

That should look like:

```bash
composer platform:generate-urls \
  --dist-type=https://github.com/wharflab/tally/releases/download/{version}/tally-composer-{platform}.{ext}
```

Recommended release workflow changes:

1. Add a Composer packaging step after the platform binaries and checksums already exist in `dist/`.
2. For each supported target, stage a package root from `packaging/composer/`.
3. Copy the matching built executable into `bin/tally` or `bin/tally.exe`.
4. Archive that package root as the Composer-specific release asset for the platform.
5. Upload those assets together with the existing release artifacts.
6. Validate that every URL generated into `extra.platform-urls` exists before publish completes.

There is a useful implementation precedent here from the existing release pipeline:

- npm already publishes platform-specific packages from `packaging/npm/`
- RubyGems already stages package contents from `packaging/rubygems/`
- Homebrew already consumes the GitHub release checksums generated by the workflow

Composer packaging should follow the same model: treat Composer as another packaging target built from the release artifacts, not as a change to the
core Go build.

Recommended install documentation for users:

```bash
composer config allow-plugins.codewithkyrian/platform-package-installer true
composer require --dev wharflab/tally
vendor/bin/tally lint Dockerfile
```

Operational notes:

- Because this repository is a Go monorepo rather than a Composer-first repository, publishing to Packagist may be cleaner from a dedicated split repo
  or release branch whose root is the Composer package.
- If Tally wants the lowest-friction install path for CI and Docker builds, a non-plugin wrapper package remains simpler operationally.
- If Tally wants the most "PHP-native" install UX, `platform-package-installer` is currently the strongest candidate.

## 8. Bottom Line

The PHP container ecosystem is large, conservative, and extremely pattern-driven. That makes it a strong target for lint rules:

- the good patterns are stable,
- the bad patterns are repeated constantly,
- and the ecosystem's official guidance is unusually aligned across Docker, Composer, Symfony, Laravel, and the PHP manual.

That is exactly the environment where Tally can add value.
