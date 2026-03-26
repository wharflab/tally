# 33. STOPSIGNAL Rules (top-level `tally/*`)

**Status:** Proposed

**Design Focus:** Turn `STOPSIGNAL` from obscure daemon folklore into a small set of high-confidence
top-level `tally/*` rules grounded in real-world Dockerfile usage, official daemon shutdown
guidance, and PID 1 signal-delivery realities.

---

## Goal

Make `STOPSIGNAL` meaningfully less cryptic in `tally` by:

- catching common `STOPSIGNAL` mistakes,
- recognizing a small set of high-confidence daemon-specific graceful-stop mappings, and
- avoiding noisy or speculative advice when the runtime process is not clear.

This proposal is Linux-focused. Windows-specific `STOPSIGNAL` behavior is already covered by the
proposed `tally/windows/no-stopsignal` rule in
[`27-windows-container-rules.md`](27-windows-container-rules.md).

## Non-goals

- Do **not** make `STOPSIGNAL` mandatory for all containers.
- Do **not** guess daemon-specific signals through opaque shell wrappers, init systems, or custom
  entrypoint scripts unless we can prove signal flow.
- Do **not** introduce a dedicated `tally/stopsignal/*` namespace.
- Do **not** add blanket rules for daemons where default `SIGTERM` is already graceful enough
  (`redis-server`, `gunicorn`, `uvicorn`, `mariadbd`, likely `mysqld`, etc.).

## Namespace decision

Keep these rules in the top-level `tally/*` namespace.

A dedicated namespace only makes sense when users frequently want to disable or enable the whole
family because it is unrelated to their workloads, as with:

- `tally/windows/*` for teams that never build Windows containers,
- `tally/powershell/*` for teams that never use PowerShell,
- `tally/gpu/*` for teams that never build GPU images.

`STOPSIGNAL` rules are different: they are generic Dockerfile runtime-correctness and
best-practice checks that apply broadly across ordinary Linux container images. They should stay in
the main `tally/*` namespace and use clear rule names rather than a dedicated sub-namespace.

## Research inputs

### GitHub corpus

Research was done with GitHub code search plus GitHub file downloads:

- used `github-mcp-server-search_code` with `STOPSIGNAL filename:Dockerfile` and
  `STOPSIGNAL filename:Containerfile`,
- downloaded **102 unique files via MCP** with `github-mcp-server-get_file_contents`,
- kept a **balanced active corpus of 100 files across 65 repos**,
- composition of the balanced corpus: **77 `Containerfile` / 23 `Dockerfile`**.

The raw search results were skewed:

- **306** raw hits examined,
- **287** were likely real container build files,
- one repository (`gotmax23/Containerfiles`) alone contributed **28** hits,
- the balanced corpus therefore capped itself at **2 files per repo**.

### Corpus caveat

This is not a random sample of all Dockerfiles on GitHub. It is a sample of Dockerfiles and
Containerfiles that already contain `STOPSIGNAL`, which means:

- systemd/init-style images are overrepresented,
- enterprise `Containerfile` usage is overrepresented,
- the corpus is useful for **what people actually do with `STOPSIGNAL`**, not for how common
  `STOPSIGNAL` is overall.

### Official sources

The corpus was cross-checked against upstream daemon docs and official image/container guidance:

- Dockerfile reference: <https://docs.docker.com/reference/dockerfile/#stopsignal>
- nginx signal control: <https://nginx.org/en/docs/control.html>
- Apache httpd stopping semantics: <https://httpd.apache.org/docs/2.4/stopping.html>
- PHP-FPM manpage source: <https://raw.githubusercontent.com/php/php-src/master/sapi/fpm/php-fpm.8.in>
- PostgreSQL shutdown modes: <https://www.postgresql.org/docs/current/server-shutdown.html>
- Redis signal handling: <https://redis.io/docs/latest/operate/oss_and_stack/reference/signals/>

Official image/container references used to confirm container-specific practice:

- PostgreSQL official image template:
  <https://raw.githubusercontent.com/docker-library/postgres/master/Dockerfile-debian.template>
- PHP official image template:
  <https://raw.githubusercontent.com/docker-library/php/master/Dockerfile-linux.template>
- Apache httpd official image:
  <https://raw.githubusercontent.com/docker-library/httpd/master/2.4/Dockerfile>
- nginx official image:
  <https://raw.githubusercontent.com/nginxinc/docker-nginx/master/stable/debian/Dockerfile>
- HAProxy official image template:
  <https://raw.githubusercontent.com/docker-library/haproxy/master/Dockerfile.template>

Additional negative-evidence sources used to decide what **not** to lint:

- Redis official image template:
  <https://raw.githubusercontent.com/redis/docker-library-redis/master/Dockerfile.template>
- Gunicorn signals doc:
  <https://raw.githubusercontent.com/benoitc/gunicorn/master/docs/content/signals.md>
- Uvicorn deployment docs:
  <https://www.uvicorn.org/deployment/>

## What the corpus says

### Observed signal values

In the balanced corpus, there were **98 active `STOPSIGNAL` directives** after filtering comment and
template noise.

| Signal token | Count | Notes |
|---|---:|---|
| `SIGRTMIN+3` | 30 | Dominated by systemd/init images |
| `SIGTERM` | 24 | Generic catch-all; also used by some nginx/php-fpm images |
| `SIGINT` | 22 | Strong PostgreSQL cluster |
| `SIGQUIT` | 14 | Strong nginx/php-fpm cluster |
| `SIGKILL` | 3 | Clear smell |
| `RTMIN+3` | 2 | Same idea as `SIGRTMIN+3`, but non-canonical spelling |
| `"SIGINT"` | 2 | Quoted / suspicious formatting |
| `SIGRTMIN` | 1 | Likely wrong for systemd/init intent |

If systemd/init images are excluded, the leaders become:

- `SIGTERM`: **24**
- `SIGINT`: **20**
- `SIGQUIT`: **14**

### Runtime launcher shape

Where the final launcher was visible in the file:

- **91/96** were exec-form,
- **5/96** were shell-form,
- **24/98** used wrapper entrypoints or scripts.

That matters because `STOPSIGNAL` only helps when the signal reaches the real service process.
Shell-form `CMD`/`ENTRYPOINT`, `sh -c`, `bash -c`, and opaque wrapper scripts make the
daemon/signal relationship much less trustworthy.

### Observed daemon clusters

| Daemon / PID 1 pattern | Files | Dominant signal(s) | Confidence |
|---|---:|---|---|
| `systemd` / `/sbin/init` / `/usr/sbin/init` | 32 | `SIGRTMIN+3` / `RTMIN+3` | High |
| `nginx` / `openresty` | 7 | `SIGQUIT` (5), `SIGTERM` (2) | High |
| `php-fpm` | 4 | `SIGQUIT` (3), `SIGTERM` (1) | High |
| `postgres` | 7 | `SIGINT` (7) | High |
| node apps | 6 | `SIGTERM`, `SIGINT`, `SIGKILL` | Low for daemon-specific linting |
| python web apps | 3 | `SIGQUIT`, `SIGINT` | Low for daemon-specific linting |

### Practical takeaways

1. `STOPSIGNAL` is not one thing; there are a few real clusters, not a universal best value.
2. The strongest Linux/container-specific mappings are:
   - systemd/init -> `SIGRTMIN+3`
   - nginx/openresty -> `SIGQUIT`
   - php-fpm -> `SIGQUIT`
   - postgres -> `SIGINT`
   - httpd/apache -> `SIGWINCH` (strong from upstream docs and official image, even though not a
     strong cluster in this balanced corpus)
3. The strongest generic smells are:
   - `SIGKILL`,
   - quoted / non-canonical signal tokens,
   - shell-form or opaque wrappers hiding the real PID 1,
   - systemd/init images using something other than `SIGRTMIN+3`.

## What upstream guidance says

| Process | Upstream graceful-stop guidance | Container conclusion |
|---|---|---|
| nginx | `QUIT` = graceful shutdown; `TERM`/`INT` = fast shutdown | direct nginx should prefer `SIGQUIT` |
| Apache httpd | `WINCH` = graceful-stop; `TERM` = immediate stop | direct httpd should prefer `SIGWINCH` |
| PHP-FPM | `SIGQUIT` = graceful stop; `SIGINT`,`SIGTERM` = immediate termination | direct php-fpm should prefer `SIGQUIT` |
| PostgreSQL | `SIGINT` = fast shutdown; `SIGTERM` = smart shutdown | containerized `postgres` should prefer `SIGINT` |
| Redis | `SIGTERM` already performs graceful shutdown | no custom rule |
| Gunicorn | `TERM` is graceful; `QUIT`/`INT` are quick | no custom rule |
| Uvicorn | graceful shutdown is built around default `SIGTERM` handling | no custom rule |

## Design principles for tally

### 1. Prefer a small number of high-confidence rules

`STOPSIGNAL` is runtime behavior, not formatting. Wrong advice is worse than silence. Rules should
only fire when `tally` can identify the actual service process with high confidence.

### 2. Do not duplicate `buildkit/JSONArgsRecommended`

BuildKit already warns on shell-form `CMD`/`ENTRYPOINT`. The `STOPSIGNAL` proposal should reuse that
fact and add the missing pieces:

- explicit shell wrappers in exec form (`["sh", "-c", ...]`, `["bash", "-c", ...]`),
- `STOPSIGNAL`-specific messaging,
- daemon-specific rules that suppress themselves when PID 1 is hidden.

### 3. Do not add a blanket “missing STOPSIGNAL” rule

That would be noisy and wrong for many images. `STOPSIGNAL` should be suggested only for a small
allowlist of daemons with strong upstream guidance.

### 4. Favor suggestions over magical autofixes

Auto-fix is only safe when:

- the signal mapping is strong,
- the final process is direct or transparently wrapped,
- there is no init/supervisor/shell layer changing signal delivery.

### 5. Separate “token hygiene” from “daemon semantics”

These are different problems:

- token hygiene: `SIGKILL`, `"SIGINT"`, `RTMIN+3`, etc.
- daemon semantics: nginx wants `SIGQUIT`, postgres wants `SIGINT`, etc.

Different rules keep the feedback precise.

## Shared implementation helper

Before adding daemon-specific rules, `tally` should add a small shared helper that computes:

1. **Normalized stop signal**
   - expand environment variables when they are statically known,
   - normalize spelling where possible,
   - preserve whether the original token was quoted or non-canonical.

2. **Effective runtime process fingerprint**
   - combine `ENTRYPOINT` and `CMD` with Docker semantics,
   - detect shell-form launchers,
   - detect explicit shell wrappers (`sh -c`, `bash -c`, `dash -c`, etc.),
   - optionally recognize transparent local shell wrappers when a copied/generated entrypoint script
     clearly ends in `exec "$@"`, `exec gosu ... "$@"`, `exec su-exec ... "$@"`, or direct
     `exec <daemon> ...`.

3. **Confidence level**
   - `Direct`
   - `TransparentWrapper`
   - `OpaqueWrapper`
   - `ShellForm`
   - `Unknown`

Every daemon-specific rule should require `Direct` or `TransparentWrapper`.

## Proposed rules

### 1. `tally/no-ungraceful-stopsignal`

**Severity:** warning  
**Category:** correctness

#### Trigger

`STOPSIGNAL` is one of:

- `SIGKILL`
- `SIGSTOP`
- numeric aliases that normalize to those signals

#### Why

These values defeat the point of `STOPSIGNAL`:

- `SIGKILL` cannot be trapped for cleanup,
- `SIGSTOP` does not terminate the process at all.

In practice, this means the runtime will either bypass graceful shutdown or eventually escalate to
`SIGKILL` anyway after the stop timeout.

#### Detection

- parse and normalize the `STOPSIGNAL` token,
- flag `SIGKILL`, `SIGSTOP`, and their numeric equivalents.

#### Fix

No blind auto-fix. Suggested replacement should be daemon-aware when possible:

- nginx/openresty -> `SIGQUIT`
- php-fpm -> `SIGQUIT`
- postgres -> `SIGINT`
- systemd/init -> `SIGRTMIN+3`
- otherwise likely `SIGTERM`

### 2. `tally/prefer-canonical-stopsignal`

**Severity:** info  
**Category:** style

#### Trigger

`STOPSIGNAL` is present but uses non-canonical spelling, for example:

- `"SIGINT"`
- `TERM`
- `QUIT`
- `RTMIN+3`

#### Why

One of the main user-facing problems with `STOPSIGNAL` is that it is already obscure. Non-canonical
spellings make it harder to read and harder to connect to upstream docs.

The corpus contained:

- **2** quoted `"SIGINT"` tokens,
- **2** `RTMIN+3` spellings alongside the more common `SIGRTMIN+3`.

#### Detection

Normalize to a canonical spelling:

- ordinary signals: `SIGTERM`, `SIGINT`, `SIGQUIT`, etc.
- RT signals: `SIGRTMIN+n`

#### Fix

Safe auto-fix when the token is recognized exactly:

- strip quotes,
- add `SIG` prefix,
- normalize `RTMIN+3` -> `SIGRTMIN+3`.

### 3. `tally/no-shell-wrapper-for-stopsignal`

**Severity:** warning  
**Category:** correctness

#### Trigger

A stage sets `STOPSIGNAL`, but its effective runtime process is hidden behind:

- shell-form `CMD` / `ENTRYPOINT`,
- exec-form `sh -c`, `bash -c`, `dash -c`, etc.,
- an opaque wrapper script that `tally` cannot prove is signal-transparent.

#### Why

`STOPSIGNAL` is delivered to PID 1. If PID 1 is a shell or opaque wrapper, the chosen signal may
never reach the actual daemon, or may reach it with different semantics.

This is a more specific runtime-focused complement to `buildkit/JSONArgsRecommended`.

#### Detection

- if `buildkit/JSONArgsRecommended` would already fire for shell-form launcher, this rule should
  either suppress itself or emit a narrower `STOPSIGNAL`-specific message,
- additionally catch exec-form shell wrappers,
- suppress daemon-specific stop-signal rules when this rule fires.

#### Fix

No auto-fix. Suggest:

- direct exec-form daemon launch, or
- a transparent wrapper that ends in `exec "$@"`.

### 4. `tally/prefer-systemd-sigrtmin-plus-3`

**Severity:** warning  
**Category:** correctness

#### Trigger

The effective runtime process is clearly systemd/init, for example:

- `/sbin/init`
- `/usr/sbin/init`
- `systemd`
- `systemd --system`

and `STOPSIGNAL` is missing or not one of:

- `SIGRTMIN+3`
- `RTMIN+3` (accepted but non-canonical)

#### Why

This was the single biggest cluster in the corpus:

- **32** systemd/init files,
- **27** used `SIGRTMIN+3`,
- **2** used `RTMIN+3`,
- outliers used `SIGINT` or `SIGRTMIN`.

This is a strong enough real-world convention to justify a dedicated rule, but only for images that
are clearly trying to run systemd/init as PID 1.

#### Detection

Require direct PID 1 identification. Do not fire through shells or opaque wrappers.

#### Fix

Safe auto-fix to:

```dockerfile
STOPSIGNAL SIGRTMIN+3
```

Also pair with `prefer-canonical-stopsignal` so `RTMIN+3` normalizes to `SIGRTMIN+3`.

### 5. `tally/prefer-nginx-sigquit`

**Severity:** info  
**Category:** best-practice

#### Trigger

The effective runtime process is clearly:

- `nginx`
- `openresty`

typically in foreground form (`daemon off;` or equivalent), and `STOPSIGNAL` is missing or not
`SIGQUIT`.

#### Why

Both the corpus and upstream docs agree:

- corpus cluster: **7** nginx/openresty files -> **5 `SIGQUIT`**, **2 `SIGTERM`**
- nginx docs: `QUIT` is graceful shutdown, `TERM` is fast shutdown

This is exactly the kind of subtle, daemon-specific knowledge users should not need to rediscover.

#### Detection

Only fire when PID 1 is direct or transparently wrapped. Suppress when the stage uses shell form,
`sh -c`, or an opaque startup script.

#### Fix

Safe suggestion, and safe auto-fix only when PID 1 confidence is high:

```dockerfile
STOPSIGNAL SIGQUIT
```

### 6. `tally/prefer-php-fpm-sigquit`

**Severity:** info  
**Category:** best-practice

#### Trigger

The effective runtime process is clearly `php-fpm` / `php-fpm -F`, and `STOPSIGNAL` is missing or
not `SIGQUIT`.

#### Why

The evidence is strong:

- corpus cluster: **4** php-fpm files -> **3 `SIGQUIT`**, **1 `SIGTERM`**
- upstream php-fpm manpage: `SIGQUIT` = graceful stop; `SIGINT`,`SIGTERM` = immediate termination
- official PHP FPM image sets `STOPSIGNAL SIGQUIT`

#### Detection

Only fire with direct or transparent wrapper confidence.

#### Fix

Safe suggestion, and safe auto-fix only when PID 1 is direct or transparently wrapped:

```dockerfile
STOPSIGNAL SIGQUIT
```

### 7. `tally/prefer-postgres-sigint`

**Severity:** info  
**Category:** best-practice

#### Trigger

The effective runtime process is clearly `postgres`, and `STOPSIGNAL` is missing or not `SIGINT`.

#### Why

This is the strongest app-daemon cluster in the corpus:

- corpus cluster: **7** postgres files -> **7 `SIGINT`**
- upstream PostgreSQL docs: `SIGINT` = fast shutdown; `SIGTERM` = smart shutdown
- official Postgres image sets `STOPSIGNAL SIGINT` and explains why

This rule is intentionally softer than nginx/php-fpm/systemd because PostgreSQL's `SIGTERM` is not
wrong; it is just less container-friendly than the official image's preferred choice.

#### Detection

Only fire with high PID 1 confidence. Suppress when the process goes through shell-form or opaque
wrappers.

#### Fix

Suggestion only by default:

```dockerfile
STOPSIGNAL SIGINT
```

The diagnostic should also mention runtime stop timeout, because PostgreSQL often needs more than
Docker's default 10 seconds.

### 8. `tally/prefer-httpd-sigwinch`

**Severity:** info  
**Category:** best-practice

#### Trigger

The effective runtime process is clearly:

- `httpd`
- `apache2-foreground`
- `httpd-foreground`

and `STOPSIGNAL` is missing or not `SIGWINCH`.

#### Why

Even though the balanced corpus did not surface a large direct httpd cluster, the upstream evidence
is strong:

- Apache docs: `WINCH` = graceful-stop; `TERM` = immediate stop
- official `httpd` image sets `STOPSIGNAL SIGWINCH`
- official PHP Apache variant also sets `STOPSIGNAL SIGWINCH`

#### Detection

Only fire when the launcher is direct or transparently wrapped.

#### Fix

Suggestion only by default:

```dockerfile
STOPSIGNAL SIGWINCH
```

## Rules we should **not** add in phase 1

### No blanket `missing-stopsignal`

Too noisy. Many daemons are perfectly fine with default `SIGTERM`.

### No daemon-specific rule for Redis

Redis already handles `SIGTERM` and `SIGINT` gracefully. The official image intentionally relies on
default behavior.

### No daemon-specific rule for Gunicorn or Uvicorn

Default `SIGTERM` is already the graceful path. Their other signals mostly control worker pools or
reload behavior rather than container stop.

### No daemon-specific rule for generic node/python apps

The corpus showed mixed `SIGTERM`, `SIGINT`, and even `SIGKILL`, but not a stable, trustworthy
mapping that `tally` should bless.

### No phase-1 auto-fix through init wrappers

`tini`, `dumb-init`, `s6`, `supervisord`, and similar wrappers are important, but they complicate
signal forwarding and sometimes rewrite signals. That is better handled as a later extension.

## Recommended implementation order

### Phase 1: generic correctness and hygiene

1. `tally/no-ungraceful-stopsignal`
2. `tally/prefer-canonical-stopsignal`
3. `tally/no-shell-wrapper-for-stopsignal`

These are broadly useful, low-risk, and immediately address common misunderstanding.

### Phase 2: strongest daemon-specific rules

4. `tally/prefer-systemd-sigrtmin-plus-3`
5. `tally/prefer-nginx-sigquit`
6. `tally/prefer-php-fpm-sigquit`
7. `tally/prefer-postgres-sigint`

### Phase 3: next daemon-specific rule

8. `tally/prefer-httpd-sigwinch`

The rule is good, but likely benefits from the same runtime-process helper used by the others.

## Future extensions

- `tally/prefer-haproxy-sigusr1`
  - strong upstream and official-image evidence,
  - not common enough in the balanced corpus to include in phase 1.
- transparent-wrapper inspection for copied entrypoint scripts
- optional cross-file check that a local entrypoint script ends in `exec "$@"`

## Representative corpus examples

These are the examples that most directly shaped the rule choices.

| Repo / path | Observed pattern | Takeaway |
|---|---|---|
| `performancecopilot/pcp/build/containers/pcp/Containerfile` | `/usr/sbin/init` + `SIGRTMIN+3` | canonical systemd/init pattern |
| `tmichett/ansible-practice/Containers/Containerfile` | `/sbin/init` + `SIGRTMIN+3` | canonical systemd/init pattern |
| `AlmaLinux/container-images/Containerfiles/8/Containerfile.init` | init image + `SIGRTMIN+3` | canonical systemd/init pattern |
| `freeipa/freeipa-local-tests/ipalab-config/minimal/Containerfile.minimal` | init image + `RTMIN+3` | correct intent, non-canonical token |
| `tulilirockz/Snapd-OCI/Containerfile` | `/sbin/init` + `SIGRTMIN` | likely wrong systemd/init signal |
| `Neomediatech/pve-docker/Dockerfile.8.2` | `/sbin/init` + `SIGINT` | suspicious init-image outlier |
| `manarth/devvm/.containers/nginx/Containerfile` | direct nginx + `SIGTERM` | likely fast stop where graceful stop was intended |
| `secondlife/nginx-proxy/Dockerfile` | nginx wrapper + `SIGQUIT` | strong nginx mapping |
| `cdrage/containerfiles/cat/Containerfile` | `nginx -g 'daemon off;'` + `SIGQUIT` | strong nginx mapping |
| `wojiushixiaobai/docker-openresty/Dockerfile` | openresty + `SIGQUIT` | nginx-family mapping extends to openresty |
| `akafeng/docker-php/Dockerfile-8.1` | `php-fpm` + `SIGQUIT` | strong php-fpm mapping |
| `SkautDevs/kissj/deploy/container_images/php/Containerfile-ubi` | `php-fpm -F` + `SIGQUIT` | strong php-fpm mapping |
| `manarth/devvm/.containers/phpfpm/Containerfile` | `php-fpm` + `SIGTERM` | likely fast/immediate stop where graceful stop was intended |
| `polymathrobotics/oci/postgres/15/noble/Containerfile` | `postgres` + `SIGINT` | strong postgres mapping |
| `greboid/dockerfiles/postgres-15/Containerfile` | `postgres` + `SIGINT` | strong postgres mapping |
| `yellow-corps/ibis/shopify/Containerfile` | node app + `SIGKILL` | clear generic smell, not a daemon-specific mapping |
| `ophilon/awesome-pods/flask/Containerfile` | quoted `"SIGINT"` | token hygiene problem |
| `redhat-manufacturing/device-edge-workshops/.../database-containerfile` | shell-form `ENTRYPOINT db-start.sh` + `SIGINT` | PID 1 hidden behind wrapper |
| `surreymagpie/nginx-php/Containerfile` | shell-form `/start.sh` + `SIGKILL` | strongest shell-wrapper + ungraceful-signal smell |

## Bottom line

The real lesson from the corpus is not “always set `STOPSIGNAL`”. It is:

- only a few daemon families need non-default stop signals,
- those mappings are stable enough to teach,
- many `STOPSIGNAL` mistakes are really **PID 1 / wrapper** mistakes,
- `tally` should help where the signal semantics are strong and stay quiet everywhere else.
