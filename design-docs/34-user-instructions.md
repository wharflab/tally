# USER / useradd / privilege transitions in Dockerfiles

> **Date**: 2026-03-27
> **Purpose**: evaluate real-world `USER` / `useradd` usage, explain what `USER`
> actually does, and propose high-signal `tally/*` rules that go beyond the very
> shallow `hadolint/DL3002` model.

---

## 1. Executive summary

`USER` is still important, but `DL3002` ("last user should not be root") is far
too weak to be the center of Tally's guidance.

What the research shows:

- `USER` **does solve a real problem**: it changes the default identity for
  later `RUN` instructions and for runtime `ENTRYPOINT` / `CMD`, so it changes
  who can write files, open privileged paths, or mutate package-manager state by
  default.
- `USER` is **not the whole security story**: rootless containers, user
  namespaces, capabilities, seccomp/AppArmor, read-only rootfs, and minimal
  runtime images often matter as much or more.
- Modern images **often do not need `useradd` at all**. Distroless,
  Chainguard/Wolfi-style, and `scratch` patterns commonly use a fixed numeric
  user (`65532`, `1001`, etc.) or inherit a pre-created non-root user from the
  base image.
- The worst copy/paste mistakes are usually **not** about obscure `useradd`
  flags. They are:
  - creating a user and never switching to it,
  - assuming `USER` affects `COPY` / `ADD` ownership,
  - using named users in passwd-less images,
  - expanding the "root window" far beyond the instructions that actually need
    it,
  - creating application directories with `WORKDIR` as root and then cleaning up
    with `chown` later.

Conclusion: keep `DL3002` for compatibility, but make Tally's USER guidance
topic-specific, semantics-aware, and centered on **actual author mistakes**.

---

## 2. What `USER` actually changes

The official Dockerfile reference says:

- [`USER`](https://docs.docker.com/reference/dockerfile/#user) sets the user
  name / UID and optional group / GID for the remainder of the current stage.
- The specified user is used for later `RUN` instructions and at runtime for the
  relevant `ENTRYPOINT` / `CMD`.
- If a group is specified (`USER app:app`), Docker uses **only** that group
  membership; supplementary groups are ignored.
- [`WORKDIR`](https://docs.docker.com/reference/dockerfile/#workdir) affects
  later `RUN`, `CMD`, `ENTRYPOINT`, `COPY`, and `ADD`, and creates the directory
  if it does not already exist.
- [`COPY --chown`](https://docs.docker.com/reference/dockerfile/#copy---chown)
  and `ADD --chown` control copied-file ownership; **without `--chown`, copied
  files are created as `0:0`**.
- Named `--chown` values are resolved through `/etc/passwd` and `/etc/group`;
  numeric IDs do not require lookup.

Practical implications:

- `USER` does **not** make later `COPY` or `ADD` land with application-user
  ownership. Authors must still use `--chown`, `chown`, `install -d -o`, or a
  base image that already has the right ownership model.
- `USER` is therefore primarily about:
  - who later `RUN` steps execute as,
  - who later runtime commands execute as,
  - and, via `WORKDIR`, **who creates application directories when they do not
    exist yet**.

This matches prior BuildKit-source analysis already done in this project:

- `USER` affects `RUN`.
- `USER` affects `WORKDIR` directory creation.
- `USER` does **not** affect `COPY` / `ADD` ownership.

That last point is the single most important semantics fact for future Tally
rules.

---

## 3. Why `DL3002` feels unsatisfying

`hadolint/DL3002` asks a single question: "is the last `USER` root?"

That misses most of the interesting space:

- a stage can create and prepare a non-root user, then forget to switch;
- a stage can run as non-root in the middle of the build, then switch back to
  root for later stages on purpose;
- a final image can safely use a numeric non-root UID without any `useradd`;
- a final image can still be broken even when `USER` is non-root, because files
  copied later are root-owned;
- a runtime can still be safer with rootless / userns isolation even if the
  image default is root.

`DL3002` is therefore a **coarse fallback warning**, not a good design model for
Tally's USER strategy.

By contrast, `DL3046` is much better: it targets a specific, concrete,
low-noise, evidence-backed `useradd` pitfall (`-l` / `--no-log-init` for high
UIDs). Tally's new USER rules should look more like `DL3046` than `DL3002`.

---

## 4. Real-world patterns from GitHub

The examples below were collected via GitHub MCP code search and file download.
The sample intentionally covers:

- `USER` with no `useradd`,
- `useradd` / `adduser` with no `USER`,
- named vs numeric users,
- Alpine vs Debian/Ubuntu patterns,
- `scratch` / distroless / shell-less runtimes,
- multiple `USER` instructions in one stage.

### 4.1 Strong / intentional patterns

| Pattern | Example | Why it matters |
|--------|---------|----------------|
| Debian runtime creates named service account, prepares ownership, then switches | [`TeamHypersomnia/Hypersomnia`](https://github.com/TeamHypersomnia/Hypersomnia/blob/a455395a8c36bc13e36e8eae85e1dbd0fc3e7d79/Dockerfile) | `groupadd -r`, `useradd -r`, explicit `chown`, then `COPY --chown=...` and `USER hypersomniac`. This is the "classic named user" pattern done correctly. |
| Alpine runtime creates simple app user/group and switches | [`gitlabform/gitlabform`](https://github.com/gitlabform/gitlabform/blob/11177f8887a42e84f4301851542b802ab42b9fa0/Dockerfile) | `addgroup -S`, `adduser -S`, then `USER appuser`, then `WORKDIR /config`. Good example of `WORKDIR` after `USER` so the runtime directory is created under the app identity. |
| Alpine image uses minimal service-account flags, avoids extra home creation | [`pump-io/pump.io`](https://github.com/pump-io/pump.io/blob/ba6b392c0720404a9e689d334e9436ce71b814c3/Dockerfile) | `adduser -S -D -H -G ... -h ... -u ...`. This is a good reminder that many `useradd` / `adduser` switches are distro- and app-specific; Tally should not over-generalize them. |
| Ubuntu runtime uses explicit numeric IDs with named account | [`OpenOrbis/OpenOrbis-PS4-Toolchain`](https://github.com/OpenOrbis/OpenOrbis-PS4-Toolchain/blob/0a1aaf9dd4a92695538bdeb09fb056d06dd11725/Dockerfile) | `groupadd -g 1000` + `useradd -r -u 1000 -g orbis`, then `USER orbis`. Stable IDs matter for host volume ownership and CI portability. |
| Distroless runtime uses numeric non-root identity, no `useradd` in final stage | [`kedacore/keda`](https://github.com/kedacore/keda/blob/2f8e3b1d017afa55031d98134c2d511be5cebf7e/Dockerfile) | Final stage is `gcr.io/distroless/static:nonroot`, then `USER 65532:65532`. This is a modern "no useradd in runtime" pattern. |
| Distroless runtime prefers numeric user for Kubernetes compatibility | [`kubernetes-sigs/cluster-api`](https://github.com/kubernetes-sigs/cluster-api/blob/21b420f08daa937c6c92a399b221027c8f59b732/Dockerfile) | The Dockerfile explicitly comments that Kubernetes expects a numeric user when applying pod security policies. This is an important real-world reason not to hard-code "named USER is better". |
| Distroless runtime uses named `COPY --chown` plus numeric `USER` | [`cloudnative-pg/cloudnative-pg`](https://github.com/cloudnative-pg/cloudnative-pg/blob/324709cabaa73a4961f5a59116ef9d676148056a/Dockerfile) | Good example that non-root images still need explicit ownership decisions. |
| `scratch` final stage copies `/etc/passwd`, then uses numeric `USER` | [`flux-iac/tofu-controller`](https://github.com/flux-iac/tofu-controller/blob/fa21532c44bffbff0e43cd63031e35ca201fc719/Dockerfile) | A robust scratch pattern: create user in builder, copy `/etc/passwd`, then run as `65532:65532`. |
| `scratch` final stage uses numeric `USER` without any passwd database | [`screego/server`](https://github.com/screego/server/blob/5285d3e07993c513825f92d8ca2059dc56a9c4ac/Dockerfile) | Important counterexample: a `scratch` image does **not** always need `useradd` or `/etc/passwd`. Numeric IDs are often enough. |
| Temporary build-stage `USER` switch is intentional | [`stitionai/devika`](https://github.com/stitionai/devika/blob/80bb343cbe4a4e5f5a0ba08d2524920139baceb6/devika.dockerfile) | The stage starts as root, installs packages, then switches to `USER nonroot` and changes `WORKDIR`. Not every mid-file `USER` is messy or redundant. |
| Large multi-stage build uses non-root for a build subsection, then resets to root for later derived stages | [`neondatabase/neon`](https://github.com/neondatabase/neon/blob/6a35a3e9f149798df1b1761ee64099d8d75fbe90/compute/compute-node.Dockerfile) | This is exactly why a blanket "move `USER` to first use and leave it there" rule would be too simplistic. |

### 4.2 Copy/paste pitfalls and suspicious patterns

| Pattern | Example | Why it matters |
|--------|---------|----------------|
| User is created, files are chowned, but runtime never switches away from root | [`PhantomBot/PhantomBot`](https://github.com/PhantomBot/PhantomBot/blob/f40abd93e835078fbab3d41d756ff0d29971d5de/Dockerfile) | Extremely high-signal pitfall. This is a much better lint target than plain DL3002. |
| User is created but never used | [`yandex/pgmigrate`](https://github.com/yandex/pgmigrate/blob/f90eaec24b758747999ff907e9a212e4485bc3d6/Dockerfile) | May be intentional, but often indicates cargo-culted setup or an incomplete hardening attempt. |
| Non-root switch exists only as a commented-out idea | [`loft-sh/vcluster`](https://github.com/loft-sh/vcluster/blob/a5339c42a3f225f56d5f2954fc1df1400465fd29/Dockerfile) | Good example of an acknowledged-but-unfinished non-root transition. Tally should help here with precise guidance, not a generic scold. |
| Runtime directories are created as root and only later repaired with `chown` | Seen in multiple Debian/Alpine examples, including `Hypersomnia` and `Devika` | Sometimes necessary, but often a sign that `USER` / `WORKDIR` ordering could be clearer and safer. |

Important exception:

- official images such as Postgres / Redis / RabbitMQ often create a service
  account but intentionally do **not** set `USER` in the Dockerfile because
  their entrypoint starts as root, performs ownership/init work, and then drops
  privileges with `gosu`, `su-exec`, or similar helpers.
- This means Tally should **not** ship a naive "`useradd` without `USER`"
  warning. Any "created user never used" rule should explicitly suppress
  entrypoint privilege-drop patterns.

### 4.3 What `useradd` / `adduser` flags actually mattered in practice

Across the sampled files, the flags that mattered repeatedly were:

- `-r` / `-S`: create a system/service account rather than a full login user.
- `-u` / `-g`: assign stable numeric identities for ownership portability.
- `-H` / `-M` or "no `-m`": avoid creating a home directory when one is not
  required.
- `-s /sbin/nologin` (or similar): occasionally used to make the account
  obviously non-login.
- `-l` / `--no-log-init`: already covered well by `DL3046`.

What **did not** look like good standalone lint targets:

- "always create a home directory"
- "never create a home directory"
- "always set a shell"
- "never set a shell"

Those choices clearly depend on whether the stage is a builder, whether the app
needs `$HOME`, whether the image is shell-less, and whether toolchains like
Cargo/pip/npm run inside the build.

---

## 5. Official ecosystem guidance

### 5.1 Docker

- Docker's official reference for [`USER`](https://docs.docker.com/reference/dockerfile/#user),
  [`WORKDIR`](https://docs.docker.com/reference/dockerfile/#workdir), and
  [`COPY --chown`](https://docs.docker.com/reference/dockerfile/#copy---chown)
  is the most important semantics source for Tally rules.
- Docker's build best-practices guide recommends small/minimal runtime images
  and multi-stage builds:
  <https://docs.docker.com/build/building/best-practices/>
- Docker's blog also has a dedicated post:
  ["Understanding the Docker USER Instruction"](https://www.docker.com/blog/understanding-the-docker-user-instruction/)
  (June 26, 2024), which explicitly frames the topic as one of best practices
  and common pitfalls.
- Docker's rootless documentation is also relevant because it explains the next
  layer beyond image-level `USER`:
  <https://docs.docker.com/engine/security/rootless/>

### 5.2 Distroless

- Distroless images emphasize that runtime images should contain only the app and
  its runtime dependencies, not shells or package managers:
  <https://github.com/GoogleContainerTools/distroless/blob/main/README.md>
- Distroless publishes `:nonroot` image variants.
- Distroless' examples also show that non-root users can be provided by the
  image build system itself rather than by `useradd` in a Dockerfile:
  <https://github.com/GoogleContainerTools/distroless/tree/main/examples/nonroot>

### 5.3 Chainguard / Wolfi-style guidance

- Chainguard's image best practices are unusually concrete here:
  <https://github.com/chainguard-images/images/blob/main/BEST_PRACTICES.md>
- Key recommendations:
  - prefer a standard username when one exists,
  - otherwise use `nonroot`,
  - default to UID/GID `65532`,
  - prefer specifying `run-as` using the **numeric UID**,
  - if the image sometimes needs root, still include a `nonroot` user and
    document how to switch.

This lines up closely with what appeared in the GitHub sample set.

### 5.4 Podman / Red Hat: what comes after `USER`

- Red Hat's rootless Podman articles are the best official explanation of why
  image-level `USER` is not the whole story:
  - <https://www.redhat.com/en/blog/rootless-podman-user-namespace-modes>
  - <https://www.redhat.com/sysadmin/rootless-containers-podman>
- Red Hat / OpenShift image guidance adds another important nuance: platforms
  may run containers under an arbitrary numeric UID, so image authors often need
  group-0 writable paths and numeric-ID-friendly ownership strategies rather
  than hard dependence on a named account:
  - <https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/images/creating-images>
  - <https://www.redhat.com/en/blog/a-guide-to-openshift-and-uids>
- The key lesson for Tally docs: container "root" can map to an unprivileged
  host UID under rootless / userns modes, and a platform may override the image's
  default runtime UID entirely.

This does **not** make `USER` irrelevant; it means Tally should explain that:

- `USER` sets the image's default least-privilege identity,
- while runtime isolation (`rootless`, user namespaces, capabilities,
  read-only rootfs, seccomp, Kubernetes `securityContext`) is the next layer.

---

## 6. What `USER` is actually solving

A direct formulation of the motivating question is:

> "If this already runs in a container, why change users at all?"

Because `USER` has **two different jobs**, and they are easy to mix together:

1. a **build-time job**: from this point onward in the current stage, later `RUN`
   instructions execute as that user, and `WORKDIR` creation also follows that
   identity;
2. a **runtime job**: unless the operator overrides it, the container's main
   process starts as that UID/GID.

Those two jobs solve different problems.

### 6.1 Build-time: what `USER` promises, and what it really delivers

At build time, `USER` is mostly a **correctness and image-shaping** instruction,
not a strong security boundary.

What it promises:

- later `RUN` steps execute as the chosen account;
- later missing `WORKDIR` directories are created under that account;
- the Dockerfile can model a narrower "root window" and make privileged steps
  explicit.

What it normally delivers in practice:

- it surfaces hidden root assumptions early;
- it makes files created by later `RUN` steps belong to the runtime identity the
  author actually intends;
- it reduces the need for late recursive `chown` repairs;
- it documents where the stage truly needs root and where it does not.

What it does **not** deliver at build time:

- it does **not** make `COPY` / `ADD` ownership follow that user; Docker's
  semantics still require `COPY --chown` / `ADD --chown` for that;
- it does **not** make the build itself a trustworthy security sandbox;
- it does **not** by itself protect against malicious build scripts in any strong
  sense.

So the build-time face of `USER` is mainly:

> "From this line onward, behave as the app user would behave, so the image is
> built with the right ownership, assumptions, and privilege boundaries."

### 6.2 Runtime: what `USER` promises, and what it does **not** promise

At runtime, `USER` sets the **default process identity inside the container**.

That is narrower than many generic Linux explanations imply.

`USER` does **not** promise:

- access to other machines;
- extra database permissions;
- persistence into future container instances;
- rewriting the immutable image for all future runs;
- protection from the initial exploit.

So if the question is:

> "Does `USER nonroot` stop a compromised app from reaching a database it already
> has credentials and network access for?"

Usually **no**.

If the question is:

> "Does `USER nonroot` change what the compromised process can do inside the
> container boundary and to resources mounted into that container?"

Usually **yes**.

That is the exact problem space `USER` addresses.

### 6.3 When runtime `USER` materially matters

Runtime `USER` matters most when the container has something valuable or mutable
that root can reach and the app user cannot.

The important cases are:

1. **Writable persistent mounts**
   Docker docs are explicit that volumes and bind mounts persist outside the
   container lifecycle. In those cases, root inside the container can often
   `chown`, `chmod`, replace, or corrupt persistent files in ways a non-root
   process cannot.

2. **Mounted sockets, devices, or permissioned files**
   If the container sees a root-owned Unix socket, device, cert/key file, or
   other permissioned resource, root may be able to use it while the app user
   cannot. This is one of the clearest ways "root inside the container" can
   unlock access to something beyond ordinary app permissions.

3. **Root-only or tightly permissioned files inside the container**
   If the image or mounted data intentionally keeps some files unreadable or
   unwritable to the app user, root bypasses that separation.

4. **Default Linux capabilities**
   Kubernetes security guidance treats `runAsNonRoot`, dropped capabilities,
   `allowPrivilegeEscalation`, seccomp, and read-only root filesystems as
   separate hardening controls. That is an important clue: container runtime
   identity still changes the privilege picture even inside a container.

5. **Mutable root filesystem during the life of the container**
   Even if the change does not survive into *future* container instances, root
   can still patch binaries, trust stores, startup scripts, or config for the
   lifetime of the compromised container. That can matter a lot for long-lived
   containers, sidecars, workers, or anything serving traffic for hours or days.

6. **Operational shells and accidental commands**
   `docker exec`, `kubectl exec`, debug shells, admin jobs, and startup helpers
   all inherit the container's current privilege model. Non-root limits damage
   from routine operational mistakes too.

### 6.4 When runtime `USER` gives only limited value

That concern is correct in an important special case.

If a container is:

- using a read-only root filesystem,
- has no writable bind mounts or persistent volumes,
- has no mounted sockets/devices that are root-accessible,
- has no root-only secrets or files that the app user cannot already read,
- has capabilities aggressively dropped,
- has `allowPrivilegeEscalation=false`,
- is run rootless or in a strong user-namespace/sandbox model,
- and the orchestrator may override the image UID anyway,

then the incremental security value of Dockerfile `USER` can be **modest**.

In that scenario, `USER` is still useful as:

- a correct default,
- a policy signal,
- a compatibility hint,
- and a way to catch hidden root assumptions,

but it is not the star of the security model.

That nuance is important. Tally should not oversell `USER`.

### 6.5 The clearest exact statement

The most precise formulation is:

> Build time: `USER` solves "run the rest of this stage as the intended account,
> so later `RUN`/`WORKDIR` behavior matches the app's privilege model."
>
> Runtime: `USER` solves "do not start the container's main process as UID 0 by
> default when a narrower UID/GID would work."
>
> What it normally delivers: smaller blast radius over writable files, mounted
> resources, and default capabilities **inside the container boundary**.
>
> What it does not normally deliver: new external permissions, persistence across
> future container instances, or protection against the initial exploit.

That is the framing Tally should use.

### 6.6 Why this still matters

Container isolation is not a single on/off property. There is still a privilege
model *inside* the container and over the resources the container can touch.

So Tally should teach `USER` as:

- **important, but narrow**,
- **stronger for correctness than for build security**,
- **stronger when mounts/capabilities/root-only resources exist**,
- and **not a substitute** for rootless mode, user namespaces, dropped
  capabilities, read-only rootfs, seccomp, AppArmor, and runtime policy.

---

## 7. Proposed Tally rules

The rules below are ranked by expected value and expected noise.

### 7.0 Observability boundary: what a Dockerfile linter can and cannot know

This is the key design constraint.

A Dockerfile-only linter **can** see:

- `USER`, `RUN`, `WORKDIR`, `COPY --chown`, `ADD --chown`,
- `VOLUME`,
- obvious state directories created in the image,
- entrypoint scripts and privilege-drop helpers declared in the image,
- chmod/chown patterns,
- whether the image is `scratch`, distroless-like, Alpine, Debian, etc.

A Dockerfile-only linter **cannot reliably see**:

- whether the container will be run rootless,
- whether user namespaces are enabled,
- whether the root filesystem will be mounted read-only,
- whether capabilities will be dropped at runtime,
- whether `allowPrivilegeEscalation=false` is set,
- whether Kubernetes/Compose will override `USER`,
- whether bind mounts or extra volumes will be added at runtime.

That means:

- `VOLUME` is **positive evidence** that state is expected,
- but the *absence* of `VOLUME` is **not** evidence that no mounts will exist,
- and the absence of runtime hardening in the Dockerfile is **not** evidence that
  it will be absent in deployment.

So Tally should avoid rules like:

- "no `VOLUME`, so `USER` brings no value"
- "no capability-drop settings in the Dockerfile, therefore image is insecure"

because those claims are not observable from a Dockerfile alone.

#### Useful supporting fact: known default non-root base

A supporting fact is useful here, but the semantic should be:

- **`BaseDefaultsToNonRoot`**

not:

- **`BaseHasNoRoot`**

Those are different claims.

What can be detected deterministically:

1. **Local-stage inheritance**
   If `FROM runtime-base` points at an earlier stage in the same Dockerfile, Tally
   can deterministically derive the earlier stage's final effective `USER`.

2. **Known external image/tag heuristics**
   For exact known references such as Distroless `:nonroot` / `:debug-nonroot`
   tags, Tally can deterministically infer that the base image is intended to
   start as a non-root default user.

3. **Registry-verified image config** (optional / stronger)
   If registry-backed resolution is enabled, the strongest fact is the image
   config's default `User` value. That still proves "defaults to non-root", not
   "root is impossible".

Why this fact is useful:

- it can suppress naive "add `USER`" advice when the base image already defaults
  to non-root;
- it can help distinguish "no explicit `USER` here" from "runtime will actually
  start as root";
- it can support a low-severity "redundant re-assertion of the same non-root
  default" style hint in some cases.

Why the stronger "`no root exists`" fact is usually the wrong target:

- Dockerfile and image config metadata usually indicate the **default runtime
  user**, not whether UID 0 is truly unusable in every execution environment;
- even when a base publishes a `nonroot` variant, a later explicit `USER 0`,
  runtime override, or other environment-specific behavior is a separate question;
- in practice, the important lint question is almost always "does this base
  already default to non-root?" rather than "is root metaphysically absent?"

A useful fact shape would be:

- `StageFacts.BaseDefaultUser`
- `StageFacts.BaseDefaultsToNonRoot`
- `StageFacts.BaseDefaultUserSource` (`stage`, `static-known-image`, `registry`)

That would be a strong foundation for smarter USER rules.

### 7.1 High-priority Dockerfile-only rules

| Rank | Proposed rule | What it catches | Fixability | Why it is worth it |
|-----:|---------------|-----------------|------------|--------------------|
| 1 | `tally/stateful-root-runtime` | Final stage runs as root **and** the Dockerfile positively signals mutable/runtime state (`VOLUME`, database/data dirs, cache dirs, `/data`, `/var/lib/...`, `/var/log/...`, etc.). | Suggestion | This is the strongest Dockerfile-only version of "root actually matters here": root now intersects with state that may outlive the container or be mounted in from outside. Better foundation than generic DL3002. |
| 2 | `tally/user-created-but-never-used` | A stage creates a dedicated user/group and prepares ownership for it, but the final effective `USER` of that stage stays root/implicit-root and no privilege-drop entrypoint pattern (`gosu`, `su-exec`, `suexec`) is detected. | Suggestion / partially auto-fixable | This is the clearest "hardening attempt left unfinished" signal (`PhantomBot`, `pgmigrate`) while respecting official-image exceptions. |
| 3 | `tally/copy-after-user-without-chown` | Stage switches to non-root, then `COPY` / `ADD` into app paths without `--chown`, assuming `USER` will carry ownership. | Usually auto-fixable when user/group is known | Directly anchored to Docker semantics. This is a common source of ownership bugs in Dockerfiles that already declare `USER`. |
| 4 | `tally/world-writable-state-path-workaround` | Dockerfile uses `chmod 777`, `a+rwx`, or similarly broad world-writable permissions on app/data/runtime paths. | Suggestion | High-signal smell of ownership confusion. It addresses the same user confusion from a better angle than "always add USER". Important to distinguish from valid OpenShift-style `chgrp 0 && chmod g=u`. |
| 5 | `tally/named-identity-in-passwdless-stage` | Final `scratch` / passwd-less stage uses named `USER` or named `--chown` without copying `/etc/passwd` / `/etc/group`. | Suggestion | High-signal in scratch/minimal images. Encourages numeric IDs or explicit passwd copying. |
| 6 | `tally/user-explicit-group-drops-supplementary-groups` | Dockerfile creates a user with extra groups, then later uses `USER name:group`, which drops supplementary groups. | Suggestion | Rare but subtle. Directly grounded in Docker docs and easy to misunderstand. |
| 7 | `tally/workdir-created-under-wrong-user` | App user exists, but `WORKDIR` for the app path is created while still root, then later repaired with `chown` or relied on by a non-root runtime. | Suggestion; narrow auto-fix in simple reorder cases | This speaks directly to the build-time face of `USER` and to BuildKit semantics around `WORKDIR`. |

### 7.2 Medium-priority Dockerfile-only rules worth considering

| Rank | Proposed rule | What it catches | Fixability | Notes |
|-----:|---------------|-----------------|------------|-------|
| 8 | `tally/final-stage-root-after-nonroot-scope` | Final stage temporarily switched to non-root, then reset to root and never switched back, even though the trailing instructions no longer require root. | Suggestion | Good for overly broad root windows; should be carefully scoped to final stages only. |
| 9 | `tally/service-user-has-login-shell-or-home-without-evidence` | Final runtime stage creates a service user with a login shell and/or home directory, but nothing in the image appears to need either. | Advisory only | Useful for shell-less/minimal images, but should stay low severity because HOME really is needed in some builds. |
| 10 | `tally/passwd-copied-but-runtime-still-root` | A scratch/minimal final stage copies `/etc/passwd` or otherwise prepares user DB state, but never sets `USER`. | Suggestion | A focused variant of `user-created-but-never-used` for scratch flows. Might be folded into that rule instead of shipped separately. |

### 7.3 Future cross-surface rules (only if Tally can also see deployment config)

These are the rules that match the user's mental model best, but they are **not**
sound from Dockerfile-only input.

Related tracking issue:

- [`#327 Research: design BuildInvocation model and explore Docker Bake integration`](https://github.com/wharflab/tally/issues/327)
  should also be read as the current umbrella for this broader expanded-context
  direction. In particular, future USER-related catches should include the
  deployment/runtime side that Dockerfile-only lint cannot observe today.

| Proposed rule | Requires | Why it is useful |
|---------------|----------|------------------|
| `tally/nonroot-without-runtime-hardening` | Dockerfile + Kubernetes/Compose/Podman config | If image uses non-root but deployment omits `readOnlyRootFilesystem`, capability drops, `allowPrivilegeEscalation=false`, etc., Tally can say "non-root is present, but the hardening story is incomplete". |
| `tally/runtime-overrides-image-user` | Dockerfile + deployment manifests | Warn when deployment runs as root even though the image is designed for non-root, or vice versa. |
| `tally/stateful-image-without-volume-permission-strategy` | Dockerfile + deployment manifests | If manifests mount persistent volumes but image has no coherent UID/GID/fsGroup/group-0 strategy, warn before users hit permission pain in production. |
| `tally/root-runtime-with-dropped-hardening-signals-missing` | Dockerfile + deployment manifests | This is the real contextual successor to DL3002: root runtime plus no read-only rootfs, no capability dropping, no privilege-escalation controls, etc. |

This is probably the best long-term direction if Tally eventually becomes capable
of linting Dockerfiles together with Kubernetes / Compose files.

### 7.4 Notes on fixes

The safest fix classes appear to be:

- add `--chown=...` to `COPY` / `ADD`,
- insert `USER <known-user>` or `USER <known-uid>:<gid>` when the image already
  prepared that identity,
- replace named identities with numeric ones in `scratch` / passwd-less stages,
- remove an unnecessary explicit group from `USER name:group` when the Dockerfile
  itself already established supplementary groups,
- replace `chmod 777`-style state-dir workarounds with narrower ownership and
  group-permission strategies where the intent is clear.

The riskiest fix classes are:

- blindly reordering `USER` and `WORKDIR`,
- stripping home/shell creation from `useradd` without proving the app does not
  rely on `$HOME`,
- moving `USER root` / `USER nonroot` across build logic in multi-stage files.

---

## 8. Rules explicitly *not* recommended

To keep noise low, the following are not recommended:

- **A generic "every final stage must contain `USER`" rule**
  Too noisy. Distroless `:nonroot` images, runtime overrides, and arbitrary-UID
  platforms make this weaker than it sounds.

- **A generic "no `VOLUME` means `USER` has no value" rule**
  Wrong because Docker/Podman/Kubernetes can add bind mounts and volumes at
  runtime. Absence of `VOLUME` is not evidence of statelessness.

- **A generic "Dockerfile should also declare read-only rootfs / cap drops" rule**
  Those are runtime/deployment controls, not Dockerfile instructions.

- **A generic "`useradd` / `adduser` without `USER`" rule**
  Wrong for Postgres/Redis/RabbitMQ-style images that intentionally start as
  root and drop privileges in entrypoint logic with `gosu`/`su-exec`/`suexec`.

- **A generic "always create a non-root user with `useradd`" rule**
  Wrong for scratch, distroless, Chainguard/Wolfi, and many numeric-ID flows.

- **A generic "always use named users" rule**
  The evidence points the other way for many runtime images; numeric IDs are
  often more portable.

- **A generic "always create a home directory" rule**
  Final runtimes often do not want one.

- **A generic "never create a home directory" rule**
  Builder stages and some runtimes genuinely need `$HOME`.

- **An unconditional "move `USER` to its first usage point" auto-fix**
  Too risky. `Neon`-style build stages prove that `USER` sometimes scopes a
  subsection intentionally.

---

## 9. Recommended direction for Tally

1. Keep `hadolint/DL3002` for compatibility, but treat it as a coarse
   compatibility warning rather than as the main USER rule.

2. Keep `hadolint/DL3046`; it is concrete, low-noise, and still useful.

3. In the first wave, prefer rules where Dockerfile-only evidence is actually
   strong:
   - `stateful-root-runtime`
   - `user-created-but-never-used`
   - `copy-after-user-without-chown`
   - `world-writable-state-path-workaround`
   - `named-identity-in-passwdless-stage`
   - `user-explicit-group-drops-supplementary-groups`
   - `workdir-created-under-wrong-user`

4. Make rule severity contextual:
   - generic final-root warnings should be weak,
   - stateful-root / mutable-root / permission-workaround warnings should be
     stronger,
   - "non-root is missing" should never be treated as the whole security story.

5. Document the surrounding model clearly:
   - `USER` affects `RUN` and runtime identity,
   - `WORKDIR` creation follows the current user,
   - `COPY` / `ADD` ownership is separate,
   - numeric IDs are first-class, especially for scratch/distroless/Kubernetes,
   - Dockerfile lint cannot see runtime rootless/cap-drop/read-only settings,
   - the real full story emerges only when deployment config is linted too.

This would make Tally much more credible on the topic than simply repeating
"last user should not be root."
