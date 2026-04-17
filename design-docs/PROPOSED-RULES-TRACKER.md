# Proposed Rules Tracker

Rules described in design docs but **not yet implemented**.
Cross-referenced against the 52 rules currently in `internal/rules/tally/` and `RULES.md`.

> Last updated: 2026-04-03

---

## GPU Container Rules (`tally/gpu/*`)

Source: [32-gpu-container-rules.md](32-gpu-container-rules.md)

| Rule ID | Severity | Description | Notes |
|---------|----------|-------------|-------|
| `tally/gpu/prefer-uv-over-conda` | Info | Suggest uv over conda for GPU Python deps | |
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
| `tally/php/enable-opcache-in-production` | Info | Suggest enabling OPcache in production PHP runtimes | |
| `tally/php/prefer-non-root-runtime` | Info | Suggest running as non-root (www-data) in production | |

**Already implemented (1):** `composer-no-dev-in-production`

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
| Windows | 6 | 3 | **3** |
| PowerShell | 3 | 3 | **0** |
| USER / Privilege | 7 | 4 | **3** |
| BuildKit Phase 2 | 3 | 0 | **3** |
| ShellCheck Native | 1 | 0 | **1** |
| **Total** | **51** | **19** | **32** |
