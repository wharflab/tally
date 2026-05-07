# Proposed Rules Tracker

Rules described in design docs but **not yet implemented**.
Cross-referenced against the 52 rules currently in `internal/rules/tally/` and `RULES.md`.

> Last updated: 2026-05-07

---

## GPU Container Rules (`tally/gpu/*`)

Source: [32-gpu-container-rules.md](32-gpu-container-rules.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/gpu/require-torch-cuda-arch-list` | Info | Warn when CUDA extensions built without `TORCH_CUDA_ARCH_LIST` | |
| `tally/gpu/hardcoded-cuda-path` | Info | Detect hardcoded `/usr/local/cuda-X.Y` paths (use `/usr/local/cuda`) | |
| `tally/gpu/ld-library-path-overwrite` | Warning | Detect `LD_LIBRARY_PATH` overwrite without preserving existing value | |
| `tally/gpu/deprecated-cuda-image` | Warning | Flag EOL CUDA image versions | Needs resolver for EOL data |
| `tally/gpu/cuda-image-upgrade` | Info | Suggest patched CUDA image versions | Needs resolver for version data |
| `tally/gpu/model-download-in-build` | Info | Warn about downloading large model artifacts in RUN | |

**Already implemented (6):** `prefer-runtime-final-stage`, `no-redundant-cuda-install`, `no-container-runtime-in-image`, `no-buildtime-gpu-queries`,
`no-hardcoded-visible-devices`, `prefer-minimal-driver-capabilities`

---

## STOPSIGNAL Rules

Source: [33-stopsignal-rules.md](33-stopsignal-rules.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/no-shell-wrapper-for-stopsignal` | Warning | Detect shell/opaque wrapper hiding PID 1 when STOPSIGNAL is set | |
| `tally/prefer-nginx-sigquit` | Info | Recommend SIGQUIT for nginx/openresty images | |
| `tally/prefer-php-fpm-sigquit` | Info | Recommend SIGQUIT for php-fpm images | |
| `tally/prefer-postgres-sigint` | Info | Recommend SIGINT for postgres images | |
| `tally/prefer-httpd-sigwinch` | Info | Recommend SIGWINCH for Apache httpd images | |

**Already implemented (3):** `no-ungraceful-stopsignal`, `prefer-canonical-stopsignal`, `prefer-systemd-sigrtmin-plus-3`

---

## PHP Container Rules (`tally/php/*`)

Source: [35-php-container-rules.md](35-php-container-rules.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/php/composer-optimize-autoloader` | Warning | Detect production `composer install` without autoloader optimization | |
| `tally/php/prefer-composer-manifest-bind-mounts` | Info | Recommend BuildKit bind mounts for composer.json/lock over COPY | |
| `tally/php/remove-build-deps-after-extension-build` | Warning | Warn on leftover build deps after `docker-php-ext-install`/`pecl install` | |
| `tally/php/composer-no-interaction-in-build` | Info | Detect `composer install` without `--no-interaction` | |
| `tally/php/prefer-composer-stage` | Info | Suggest `composer:*` image instead of curl-installing composer | |
| `tally/php/prefer-non-root-runtime` | Info | Suggest running as non-root (www-data) in production | |

**Already implemented (3):** `composer-no-dev-in-production`, `no-xdebug-in-final-image`, `enable-opcache-in-production`

---

## Ruby Container Rules (`tally/ruby/*`)

Source: [43-ruby-on-docker.md](43-ruby-on-docker.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/ruby/jemalloc-installed-but-not-preloaded` | Warning | `libjemalloc` installed but `LD_PRELOAD`/`MALLOC_CONF` never set | |
| `tally/ruby/asset-precompile-without-dummy-key` | Warning | `rails assets:precompile` without `SECRET_KEY_BASE_DUMMY=1` | |
| `tally/ruby/bootsnap-precompile-without-j1` | Warning | `bootsnap precompile` without `-j 1` flag (QEMU multi-arch crash) | |
| `tally/ruby/missing-bundle-deployment` | Warning | Production `bundle install` without `BUNDLE_DEPLOYMENT=1` | |
| `tally/ruby/missing-bundle-without-development` | Warning | Production `bundle install` without `BUNDLE_WITHOUT="development"` | |
| `tally/ruby/redundant-bundler-install` | Info | `gem install bundler` redundant on official `ruby:*` base | |
| `tally/ruby/leftover-bundler-cache` | Info | `bundle install` not followed by canonical Bundler cache cleanup | |
| `tally/ruby/prefer-bundler-cache-mount` | Info | `bundle install` without BuildKit `RUN --mount=type=cache` | |
| `tally/ruby/eol-ruby-version` | Warning | EOL Ruby base image (2.x, 3.0, 3.1) | Needs resolver for EOL data |
| `tally/ruby/state-paths-not-writable-as-non-root` | Warning | Non-root `USER` but `tmp`/`log`/`storage`/`db` not chowned | |
| `tally/ruby/secrets-in-arg-or-env` | Warning | `SECRET_KEY_BASE`/`RAILS_MASTER_KEY` in `ARG`/`ENV` (history leak) | |
| `tally/ruby/yjit-not-enabled-on-supported-runtime` | Suggestion | Ruby 3.3+ web/worker image without `RUBY_YJIT_ENABLE=1` | |
| `tally/ruby/deprecated-bundler-install-flags` | Info | `bundle install --without`/`--deployment`/`--path` (deprecated 2.x flags) | |
| `tally/ruby/prefer-gemfile-bind-mounts` | Info | Recommend `RUN --mount=type=bind` for `Gemfile`/`Gemfile.lock` over `COPY` | Educational |
| `tally/ruby/healthcheck-rails-up-endpoint` | Suggestion | Promote `HEALTHCHECK CMD curl -fsS .../up` (Rails 7.1+ built-in) | Educational |
| `tally/ruby/prefer-secret-mounts-for-build-credentials` | Info | Recommend `RUN --mount=type=secret` for `BUNDLE_GITHUB__COM`/`RAILS_MASTER_KEY` | Educational |
| `tally/ruby/prefer-network-none-install` | Suggestion | Promote `bundle cache` + `RUN --network=none bundle install --local` split | Educational |

---

## Windows Container Rules (`tally/windows/*`)

Source: [27-windows-container-rules.md](27-windows-container-rules.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/windows/cleanup-in-same-layer` | Warning | Detect file cleanup in separate layer from download | |
| `tally/windows/prefer-nanoserver` | Info | Recommend nanoserver over servercore for minimal runtimes | |

**Already implemented (3):** `no-stopsignal`, `no-run-mounts`, `no-chown-flag`

---

## PowerShell Rules (`tally/powershell/*`)

Source: [27-windows-container-rules.md](27-windows-container-rules.md)

_All rules proposed in the design doc are implemented._

**Already implemented (3):** `prefer-shell-instruction`, `error-action-preference`, `progress-preference`

---

## USER & Privilege Rules

Source: [34-user-instructions.md](34-user-instructions.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/user-explicit-group-drops-supplementary-groups` | Suggestion | `USER name:group` drops supplementary groups accidentally | |
| `tally/workdir-created-under-wrong-user` | Suggestion | WORKDIR created as root then repaired with chown | |

**Already implemented (4):** `stateful-root-runtime`, `user-created-but-never-used`, `copy-after-user-without-chown`,
`world-writable-state-path-workaround`

---

## BuildKit Phase 2 Rules

Source: [10-buildkit-phase2-path-forward.md](10-buildkit-phase2-path-forward.md),
[buildkit-phase2-rules-research.md](buildkit-phase2-rules-research.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| WorkdirRelativePath | Warning | Relative WORKDIR without prior absolute WORKDIR | No `tally/` prefix assigned yet |
| SecretsUsedInArgOrEnv | Warning | Pattern match for secret tokens in ARG/ENV keys | No `tally/` prefix assigned yet |
| RedundantTargetPlatform | Info | Unnecessary `$TARGETPLATFORM` usage | No `tally/` prefix assigned yet |

---

## ShellCheck Native Porting

Source: [28-shellcheck-go-reimplementation-bridge-sc1040.md](28-shellcheck-go-reimplementation-bridge-sc1040.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `shellcheck/SC1040` | Warning | `<<-` end token can only be indented with tabs | Pilot for native Go reimplementation |

---

## Summary

| Category | Proposed | Implemented | Remaining |
|----------|----------|-------------|-----------|
| GPU | 14 | 6 | **8** |
| STOPSIGNAL | 8 | 3 | **5** |
| PHP | 9 | 1 | **8** |
| Ruby | 17 | 0 | **17** |
| Windows | 6 | 3 | **3** |
| PowerShell | 3 | 3 | **0** |
| USER / Privilege | 7 | 4 | **3** |
| BuildKit Phase 2 | 3 | 0 | **3** |
| ShellCheck Native | 1 | 0 | **1** |
| **Total** | **68** | **19** | **49** |
