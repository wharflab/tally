# dockerfilelint vs Tally: rule coverage and gap analysis

_Last reviewed: April 26, 2026._

## Scope and method

This document compares:

1. `replicatedhq/dockerfilelint` rules that are currently implemented.
2. `dockerfilelint` rules/checks explicitly marked as planned (`[ ]`) in upstream docs.
3. Current Tally rule surface (`buildkit/*`, `hadolint/*`, `tally/*`, and `shellcheck/*`).

Primary upstream sources:

- dockerfilelint README (supported rules + planned checks):
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/README.md>
- dockerfilelint implementation:
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/checks.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/apt.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/apk.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/reference.js>
- dockerfilelint tests:
  - <https://github.com/replicatedhq/dockerfilelint/tree/main/test>

Tally source-of-truth used here:

- `_docs/rules/**` (published rule inventory)

---

## Executive summary

- **Most dockerfilelint “implemented” checks are already covered in Tally** via either BuildKit parser checks, Hadolint-backed rules, or existing Tally rules.
- **Highest-value gaps for Tally** (relative to dockerfilelint):
  1. **`apt-get_recommends`** equivalent (`--no-install-recommends`) — straightforward and still useful.
  2. **`apt-get update` must be paired with `apt-get install` in the same RUN** — somewhat opinionated but often desirable in CI policy profiles.
  3. **LABEL `key=value` structural validation parity** — mostly parser/BuildKit already validates syntax; additional policy might still add value for consistency.
- Many unchecked dockerfilelint “planned” ideas are either outdated (e.g., one `ENV` line for cache layers), conflicting with modern multi-stage best practices, or already better handled by existing Tally/BuildKit/Hadolint rules.

---

## Part A — dockerfilelint implemented rules vs Tally

Legend:

- **Covered**: equivalent or stricter check already exists in Tally.
- **Partial**: some overlap exists, but behavior diverges.
- **Missing**: no clear equivalent found in current Tally docs.
- **N/A / not recommended**: dockerfilelint behavior is legacy/opinionated in a way Tally likely should not mirror.

| dockerfilelint rule | dockerfilelint intent | Tally status | Notes | Potential Tally action |
|---|---|---|---|---|
| `required_params` | all instructions must have args | **Covered** | BuildKit parser catches malformed instructions; `tally/unknown-instruction` also covers invalid instruction structure cases. | none |
| `uppercase_commands` | instruction casing uppercase | **Covered** | Equivalent to `buildkit/ConsistentInstructionCasing`. | none |
| `from_first` | first meaningful instruction must be `FROM`/`ARG` and FROM cannot appear later | **N/A / not recommended** | Modern Dockerfiles intentionally use multiple `FROM` for multi-stage builds. dockerfilelint README appears self-contradictory with its multistage support. | do not port as-is |
| `invalid_line` | invalid line syntax | **Covered** | Parser-level error space in Tally/BuildKit. | none |
| `sudo_usage` | prohibit `sudo` in RUN | **Covered** | Equivalent to `hadolint/DL3004` in Tally docs. | none |
| `apt-get_missing_param` | enforce `-y/--yes` for apt-get install/remove/upgrade | **Covered** | Equivalent to `hadolint/DL3014` (use `-y`). | none |
| `apt-get_recommends` | enforce `--no-install-recommends` for apt installs | **Missing** | No `hadolint/DL3015` equivalent is currently listed in Tally docs. | **candidate rule** |
| `apt-get-upgrade` | ban `apt-get upgrade` | **Covered** | Equivalent to `hadolint/DL3005` behavior (do not upgrade in image builds). | none |
| `apt-get-dist-upgrade` | ban `apt-get dist-upgrade` | **Covered** | Included in no-upgrade family policy coverage. | none |
| `apt-get-update_require_install` | require apt update + install in same RUN chain | **Missing** | Tally has package sorting/cache-mount guidance, but no explicit pairing policy documented. | **candidate rule** |
| `apkadd-missing_nocache_or_updaterm` | enforce `apk add --no-cache` or update+rm cache | **Covered** | Equivalent to `hadolint/DL3019` style behavior. | none |
| `apkadd-missing-virtual` | suggest `apk add --virtual` when adding then deleting build deps | **Partial** | Tally has package hygiene rules but no exact `--virtual` heuristic documented. | optional candidate |
| `invalid_port` | EXPOSE port format validation | **Covered** | Equivalent to `buildkit/ExposeInvalidFormat`. | none |
| `invalid_command` | instruction must be valid Dockerfile command | **Covered** | Equivalent to `tally/unknown-instruction`. | none |
| `expose_host_port` | disallow `EXPOSE host:container` | **Covered** | Equivalent to `buildkit/ExposeInvalidFormat` behavior. | none |
| `label_invalid` | `LABEL` should be `key=value` pairs | **Partial** | Parsing catches invalid syntax; explicit policy rule not documented. | optional candidate |
| `missing_tag` | FROM image should include a tag | **Covered** | Equivalent to `hadolint/DL3006`. | none |
| `latest_tag` | disallow `:latest` | **Covered** | Equivalent to `hadolint/DL3007`. | none |
| `extra_args` | catch extra args for single-arg commands | **Covered** | BuildKit parser and command-specific rules catch invalid forms. | none |
| `missing_args` | catch missing args for specific commands | **Covered** | Parser/command validation coverage in BuildKit checks. | none |
| `add_src_invalid` | `ADD` source must stay in build context | **Covered** | Equivalent to BuildKit context checks / copy-add source validation family. | none |
| `add_dest_invalid` | multi-source/wildcard ADD requires directory destination | **Covered** | Equivalent to `hadolint/DL3021` and related COPY/ADD destination checks in Tally docs. | none |
| `invalid_workdir` | unescaped spaces in WORKDIR path | **Partial** | Tally has `buildkit/WorkdirRelativePath` and `hadolint/DL3000` but not this exact quoting-focused style rule. | low-priority candidate |
| `invalid_format` | generic invalid arg format (ENV/SHELL/HEALTHCHECK options) | **Covered** | Parser + BuildKit instruction-specific validation catches these categories. | none |
| `apt-get_missing_rm` | require removing apt lists in same layer | **Covered** | Equivalent to `hadolint/DL3009` style check. | none |
| `deprecated_in_1.13` | MAINTAINER deprecated | **Covered** | Equivalent to `buildkit/MaintainerDeprecated`. | none |
| `healthcheck_options_missing_args` | HEALTHCHECK options must include values | **Covered** | BuildKit HEALTHCHECK parser validation catches malformed option/value pairs. | none |

### Potentially actionable gaps (implemented upstream, not clearly in Tally)

#### 1) `apt-get_recommends` parity

- **Why it may be worth adding:** practical image-size reduction; deterministic dependency surface.
- **dockerfilelint implementation:**
  - Rule wiring: <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
  - apt helper: <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/apt.js>
- **dockerfilelint tests:**
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/apt.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/index.js>

#### 2) `apt-get-update_require_install` parity

- **Why it may be worth adding:** avoids stale index in separate layers; aligns with common Dockerfile best-practice guidance.
- **dockerfilelint implementation:**
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
- **dockerfilelint tests:**
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/index.js>

#### 3) `apkadd-missing-virtual` parity (optional)

- **Why it may be worth adding:** encourages cleanup grouping for temporary build dependencies in Alpine flows.
- **dockerfilelint implementation:**
  - Rule wiring: <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
  - apk helper: <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/apk.js>
- **dockerfilelint tests:**
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/apk.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/index.js>

#### 4) `label_invalid` parity (optional)

- **Why it may be worth adding:** improves explicit feedback quality for key/value format violations even if parser already rejects many malformed cases.
- **dockerfilelint implementation:**
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/checks.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
- **dockerfilelint tests:**
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/checks.js>
  - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/index.js>

---

## Part B — dockerfilelint “intent to implement” checks (README unchecked list)

The README includes planned but not fully implemented checks. Below is the subset that still appears relevant to Tally and worth considering.

### Worth considering for Tally

1. **COPY parity checks (similar to ADD)**
   - Intent: validate COPY semantics similarly to ADD and detect cache-antagonistic COPY patterns.
   - Relevance to Tally: moderate; Tally already has COPY-focused rules (`prefer-copy-chmod`, `copy-after-user-without-chown`, BuildKit COPY validations), but explicit multi-source/cache policy could be useful as style guidance.
   - Upstream reference: <https://github.com/replicatedhq/dockerfilelint/blob/main/README.md#copy>

2. **STOPSIGNAL validation & uniqueness**
   - Intent: validate signal values and single declaration.
   - Relevance to Tally: moderate-high; Tally already has nuanced signal guidance (`prefer-canonical-stopsignal`, `no-ungraceful-stopsignal`, `windows/no-stopsignal`, service-specific recommendations). A uniqueness constraint could still be additive.
   - Upstream reference: <https://github.com/replicatedhq/dockerfilelint/blob/main/README.md#stopsignal>

3. **ENTRYPOINT validation support**
   - Intent: additional entrypoint structure checks.
   - Relevance to Tally: moderate; Tally already carries `hadolint/DL4003`, `DL4004`, and shell-focused process checks, so only targeted gaps may remain.
   - Upstream reference: <https://github.com/replicatedhq/dockerfilelint/blob/main/README.md#entrypoint>

4. **ONBUILD validation support**
   - Intent: ONBUILD checks.
   - Relevance to Tally: low-moderate; Tally already has `tally/invalid-onbuild-trigger`, but there may be broader ONBUILD policy opportunities.
   - Upstream reference: <https://github.com/replicatedhq/dockerfilelint/blob/main/README.md#onbuild>

### Likely low-value / outdated to port directly

- “All EXPOSE ports in a single line” (layer-count micro-optimization is much less relevant with modern build cache and readability trade-offs).
- “Only one ENV line” (same rationale; often harms readability and change locality).
- Single-CMD rule as phrased in dockerfilelint may conflict with stage-aware semantics if interpreted globally.

---

## Suggested Tally backlog seeds (prioritized)

1. **tally/apt-no-install-recommends** (or hadolint DL3015 parity)
   - Source behavior: `apt-get_recommends`.
   - Implementation/test links:
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/apt.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/apt.js>

2. **tally/apt-update-must-pair-install**
   - Source behavior: `apt-get-update_require_install`.
   - Implementation/test links:
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/index.js>

3. **tally/apk-prefer-virtual-build-deps** (optional, lower priority)
   - Source behavior: `apkadd-missing-virtual`.
   - Implementation/test links:
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/apk.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/apk.js>

4. **tally/label-key-value-format** (optional, mainly DX/clarity)
   - Source behavior: `label_invalid`.
   - Implementation/test links:
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/checks.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/lib/index.js>
     - <https://github.com/replicatedhq/dockerfilelint/blob/main/test/checks.js>

---

## Appendix — additional interesting facts

1. **dockerfilelint is still discoverable but comparatively mature/stable rather than rapidly evolving.**
   - Repository metadata currently shows latest push in **September 2023**.

2. **Rule configurability in dockerfilelint is limited compared with Tally.**
   - Upstream README documents primarily on/off rule toggles in `.dockerfilelintrc`.

3. **dockerfilelint rule architecture is mostly line- and token-oriented, while Tally leverages richer parser/facts layers.**
   - This explains why several dockerfilelint checks map to parser-level validations already present in Tally/BuildKit.

4. **Some dockerfilelint README intents encode older cache-layer optimization advice.**
   - Examples: “single ENV line”, “single EXPOSE line”. In modern teams, these are often style choices rather than universally accepted lint violations.

