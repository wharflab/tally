# 40. LABEL Rules Research and Proposal

## Status

Partially implemented.

Research date: 2026-04-28.

Implementation status as of 2026-04-28:

- Implemented the shared Dockerfile `LABEL` facts layer in `internal/facts`.
- Implemented `tally/labels/no-duplicate-keys` as a diagnostic-only rule.
- Implemented `tally/labels/valid-key` with Docker-reserved namespace guardrails and allowlists.
- Registered the new `internal/rules/tally/labels` package and added docs, navigation, unit tests, integration fixtures, and snapshots for the two
  implemented rules.
- Deferred the originally proposed partial duplicate-key auto-fix until source-preserving label-pair edits are designed.

The recommendation is to add a dedicated `tally/labels/*` namespace for Dockerfile and Containerfile `LABEL` management rules. The namespace should
focus on maintenance value: preventing accidental overrides, making image metadata easier to review, keeping generated provenance labels out of the
Dockerfile when Buildx already emits them, and validating configured label contracts when teams opt in.

## Goal

Design label-related rules that are useful in modern Dockerfiles:

- prevent errors and ambiguous image metadata
- improve diffs by organizing labels in a stable human-readable order
- avoid rules that merely enforce personal formatting taste
- ground the proposal in official documentation, upstream linter behavior, and a corpus of real Dockerfiles and Containerfiles from GitHub

## Non-goals

- Do not make every image publish a full OCI annotation set by default.
- Do not require Kubernetes object labels inside image metadata.
- Do not treat all `org.opencontainers.image.*` labels as Buildx conflicts. Many projects use them intentionally.
- Do not port Hadolint label rules one-for-one if a smaller Tally-native schema engine can cover the same value with less noise.
- Do not auto-move labels across `FROM`, `ARG`, `ENV`, `RUN`, or comments unless the move is provably source-preserving.

## Executive Summary

The high-value v1 set is:

| Rule | Default | Severity | Auto-fix | Why it belongs in v1 |
|---|---|---|---|---|
| `tally/labels/no-duplicate-keys` | on | warning | partial | Docker applies the most recent label value, so duplicates are usually mistakes or stale metadata. |
| `tally/labels/no-buildx-git-overlap` | conditional | warning when active, info otherwise | no | Buildx can emit source, revision, and Dockerfile-path labels automatically; manually maintaining those keys causes stale metadata. |
| `tally/labels/no-ineffective-stage-metadata` | on | info | no | Labels in non-exported build stages are often invisible in the final image; this teaches the multi-stage image-config boundary. |
| `tally/labels/no-invocation-conflicts` | on when invocation labels exist | warning/info | no | Bake and Compose can set image labels outside the Dockerfile; duplicate keys split ownership and hide the effective value. |
| `tally/labels/prefer-grouped` | on | info | adjacent blocks only | Scattered labels were common in the corpus and make diffs harder to review. Modern Docker no longer needs separate labels for layer reasons. |
| `tally/labels/prefer-stable-order` | on | info | safe blocks only | Label order should be stable and logical, not accidental. This keeps metadata diffs small and easy to review. |
| `tally/labels/valid-key` | on | warning/style | no | BuildKit accepts many malformed-looking keys, so Tally should catch parser-independent key mistakes and reserved namespace misuse. |
| `tally/labels/prefer-reverse-dns-keys` | on | info/style | no | Docker recommends reverse-DNS namespaces for custom label keys. This fills the educational gap for playful or ambiguous unqualified keys. |

The configurable v1 or v1.1 set is:

| Rule | Default | Severity | Auto-fix | Why it should be configurable |
|---|---|---|---|---|
| `tally/labels/prefer-invocation-scope` | off or info with orchestrator entrypoints | info | no | Volatile labels such as revision, created, version, and target-specific source are usually more consistent in Bake or Compose build labels. |
| `tally/labels/no-image-keys-in-service` | on for Compose entrypoints | info/warning | no | OCI image labels in Compose service `labels:` label containers, not the built image. |
| `tally/labels/no-service-keys-in-image` | info for Compose entrypoints | info | no | Routing, monitoring, and update-policy labels are usually deployment metadata, not reusable image metadata. |
| `tally/labels/no-kubernetes-security-context` | on | info/warning | no | Labels that look like Kubernetes `securityContext` controls create false confidence; Kubernetes enforces these through Pod or Container specs, not image labels. |
| `tally/labels/schema` | off | warning | no | Covers required labels and value types. Useful for organizations, noisy as a default. |
| `tally/labels/require-oci-baseline` | off | info/warning | no | Useful for published images, but too policy-heavy for all Dockerfiles. |

Post-v1 candidates:

- `tally/labels/prefer-oci-over-legacy-schema`: migrate legacy `org.label-schema.*` keys to OCI keys.
- `tally/labels/no-deprecated-maintainer`: prefer `org.opencontainers.image.authors` over `LABEL maintainer=...`.
- Hadolint compatibility wrappers for DL3048 through DL3058, backed by the same `tally/labels/schema` implementation.

## Research Inputs

### GitHub Corpus

I used GitHub code search through the GitHub tooling to build a focused corpus of real files containing OCI labels.

Seed queries:

| Query | Result count |
|---|---:|
| `"LABEL org.opencontainers.image" filename:Dockerfile` | 30,080 |
| `"LABEL org.opencontainers.image" filename:Containerfile` | about 860 to 876 |
| `"BUILDX_GIT_LABELS"` | 74 |
| `"LABEL org.label-schema" filename:Dockerfile` | 3,928 |
| `"LABEL io.k8s" filename:Dockerfile` | 1,724 |

Additional orchestrator-focused probes:

| Query | Result count | Takeaway |
|---|---:|---|
| `"org.opencontainers.image" "labels" filename:docker-bake.hcl` | 936 | Bake files commonly own image labels directly. |
| `"org.opencontainers.image" "labels:" filename:compose.yaml` | 52 | Compose files also carry OCI labels, sometimes in build labels and sometimes in service labels. |
| `"build:" "labels:" "org.opencontainers.image" filename:docker-compose.yml` | 115 | Compose build labels are used for image metadata in real projects. |
| `"com.docker.compose" "labels:" filename:compose.yaml` | 154 | Users do try to set Compose-reserved labels. Compose documents this prefix as reserved for runtime-managed labels. |
| `"LABEL traefik." filename:Dockerfile` | 116 | Runtime routing labels sometimes get baked into images, even though they usually belong to container/service definitions. |
| `"LABEL com.centurylinklabs.watchtower" filename:Dockerfile` | 327 | Runtime update-policy labels are often baked into images; some cases are intentional, but Compose-aware placement advice is valuable. |
| `"LABEL org.opencontainers.image.created" filename:Dockerfile` | 1,592 | Build-time timestamps are frequently wired through Dockerfiles, often via `ARG`. |
| `"LABEL org.opencontainers.image.revision" filename:Dockerfile` | 1,808 | Git revisions are frequently wired through Dockerfiles, but Buildx/Bake/CI can usually supply them more consistently. |

The collected corpus used 130 Dockerfile or Containerfile-like files from 126 repositories. I fetched file blobs through the GitHub API and parsed
`LABEL` instructions with a simple continuation-aware scanner. This scanner was good enough for corpus analysis, but the implementation should use the
existing BuildKit parser and Tally facts layer.

Corpus totals:

| Metric | Count |
|---|---:|
| Files | 130 |
| Repositories | 126 |
| `LABEL` instructions | 383 |
| Label key/value pairs | 495 |
| Files with one `LABEL` instruction | 65 |
| Files with two `LABEL` instructions | 16 |
| Files with three or more `LABEL` instructions | 49 |
| Files with at least one multi-pair `LABEL` instruction | 32 |
| Files with at least one multi-line `LABEL` instruction | 33 |
| Files with duplicate label keys in the same file | 8 |
| Files with labels overlapping Buildx git label keys | 89 |
| Files with dynamic label values | 29 |
| Files with legacy `org.label-schema.*` keys | 2 |
| Files with `LABEL maintainer=...` | 18 |
| Files with `io.openshift.*` keys | 1 |

Most common keys in the collected corpus:

| Key | Count |
|---|---:|
| `org.opencontainers.image.source` | 93 |
| `org.opencontainers.image.description` | 63 |
| `org.opencontainers.image.authors` | 51 |
| `org.opencontainers.image.title` | 46 |
| `org.opencontainers.image.licenses` | 35 |
| `org.opencontainers.image.vendor` | 27 |
| `org.opencontainers.image.version` | 26 |
| `org.opencontainers.image.url` | 19 |
| `maintainer` | 18 |
| `org.opencontainers.image.documentation` | 16 |
| `org.opencontainers.image.created` | 12 |
| `org.opencontainers.image.revision` | 10 |
| `org.opencontainers.image.base.name` | 8 |

Corpus caveats:

- The corpus is intentionally biased toward files that already use OCI labels.
- GitHub search results include examples, tests, and Dockerfile-like files outside project roots.
- The corpus parser approximated Dockerfile syntax. Final rule implementations must use the BuildKit AST and source ranges.
- The high frequency of `org.opencontainers.image.source` does not mean it is always safe to flag. It is both a useful human-authored label and a
  label Buildx can generate.

Representative corpus examples:

| Pattern | Example |
|---|---|
| Scattered OCI labels | [`OHDSI/Atlas` Dockerfile](https://github.com/OHDSI/Atlas/blob/f65b9f712ef1e6ff58d293f548d1301da5145464/Dockerfile) |
| Many separate labels | [`tomdotorg/docker-weewx` Dockerfile-like file](https://github.com/tomdotorg/docker-weewx/blob/ff6548dcb3df9ce9d1a44c96d2082cf4d546d389/doug_dockerfile.txt) |
| Duplicate OCI keys | [`QAInsights/PerfAction` Dockerfile](https://github.com/QAInsights/PerfAction/blob/d16221318a6d261b1546b9dec9c0bb1739e3d480/Dockerfile) |
| Grouped multi-line OCI labels | [`censys/censys-cloud-connector` Dockerfile](https://github.com/censys/censys-cloud-connector/blob/75d973e0924a1b22abc06db9ee8f6e985ecf2321/Dockerfile) |
| Grouped contest example | [`png261/dockerfile-contest-2025` Dockerfile.txt](https://github.com/png261/dockerfile-contest-2025/blob/b407d2dfe792519cf69d92606a536d5512fb49c4/Dockerfile.txt) |
| Legacy `org.label-schema.*` | [`Neomediatech/php` Dockerfile.8](https://github.com/Neomediatech/php/blob/12a1f1ba7c7da2fda5d0e4910930b3ca2fe4e4cf/fpm/Dockerfile.8) |
| OpenShift style labels | [`ubi-micro-dev/ubi-micro-dev` Containerfile-node](https://github.com/ubi-micro-dev/ubi-micro-dev/blob/e70db7106dca64835f7505024f9c1f5a833aafc2/Containerfile-node) |
| Bake shared label target | [`vllm-project/vllm` docker-bake.hcl](https://github.com/vllm-project/vllm/blob/de3da0b97cd9db8b1d429312992a5759c89ef881/docker/docker-bake.hcl) |
| Bake label function with dynamic values | [`dani-garcia/vaultwarden` docker-bake.hcl](https://github.com/dani-garcia/vaultwarden/blob/7cf0c5d67eb81c8b4f2e86b5c8d030bb330faa28/docker/docker-bake.hcl) |
| Compose `build.labels` for image metadata | [`CoreWorxLab/CAAL` docker-compose.yaml](https://github.com/CoreWorxLab/CAAL/blob/3eb4e9d6eed20925d5d8fa3c1913e03baaa2dd54/docker-compose.yaml) |
| Compose service labels for runtime routing | [`nhost/nhost` docker-compose.yaml](https://github.com/nhost/nhost/blob/fcfd6095faa478ad39e574addf1b0a74221ac869/examples/docker-compose/docker-compose.yaml) |
| Traefik labels in Dockerfile | [`guillaumebriday/todolist-backend-laravel` Dockerfile.prod](https://github.com/guillaumebriday/todolist-backend-laravel/blob/71ee99b7d57b2da9f6a730200f697d71e89082a7/.cloud/docker/Dockerfile.prod) |
| Watchtower labels in Dockerfile | [`appsmithorg/appsmith` Dockerfile](https://github.com/appsmithorg/appsmith/blob/a92dee41fe82ad5fc4f4166df86ab423a1f93bd9/Dockerfile) |
| Volatile OCI labels wired through Dockerfile args | [`gordalina/cachetool` Dockerfile](https://github.com/gordalina/cachetool/blob/732a831d53a4a66365c77fb95255e81a70a20160/Dockerfile) |

### Official And Upstream Sources

Primary sources used:

- Dockerfile `LABEL` reference: <https://docs.docker.com/reference/dockerfile/#label>
- Docker object labels guide: <https://docs.docker.com/engine/manage-resources/labels/>
- Dockerfile best practices, `LABEL` section: <https://docs.docker.com/develop/develop-images/dockerfile_best-practices/#label>
- Docker build variables, including `BUILDX_GIT_LABELS`: <https://docs.docker.com/build/building/variables/>
- Docker Bake file reference, `target.labels`: <https://docs.docker.com/build/bake/reference/#targetlabels>
- Docker Compose build specification, `build.labels`: <https://docs.docker.com/reference/compose-file/build/#labels>
- Docker Compose services reference, service `labels` and `label_file`: <https://docs.docker.com/reference/compose-file/services/#labels>
- GitHub Container registry label support:
  <https://docs.github.com/en/enterprise-cloud@latest/packages/working-with-a-github-packages-registry/working-with-the-container-registry>
- GitHub package repository connection: <https://docs.github.com/en/packages/learn-github-packages/connecting-a-repository-to-a-package>
- Docker Buildx source for git label emission: <https://github.com/docker/buildx/blob/f1b60d2003901a68bf9c5748a275b82444d05db1/build/git.go>
- BuildKit Dockerfile label parsing: <https://github.com/moby/buildkit/blob/v0.29.0/frontend/dockerfile/parser/line_parsers.go>
- BuildKit typed `LABEL` instruction parsing: <https://github.com/moby/buildkit/blob/v0.29.0/frontend/dockerfile/instructions/parse.go>
- BuildKit label dispatch and expansion: <https://github.com/moby/buildkit/blob/v0.29.0/frontend/dockerfile/dockerfile2llb/convert.go>
- OCI image annotation keys: <https://github.com/opencontainers/image-spec/blob/main/annotations.md>
- OCI Go constants for annotation keys: <https://github.com/opencontainers/image-spec/blob/main/specs-go/v1/annotations.go>
- Kubernetes labels and selectors: <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>
- Kubernetes recommended labels: <https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/>
- Kubernetes images: <https://kubernetes.io/docs/concepts/containers/images/>
- Kubernetes security context: <https://kubernetes.io/docs/tasks/configure-pod-container/security-context/>
- Kubernetes Pod Security Standards: <https://kubernetes.io/docs/concepts/security/pod-security-standards/>
- Hadolint README label linting: <https://github.com/hadolint/hadolint/blob/master/README.md>
- Hadolint label rule source: <https://github.com/hadolint/hadolint/tree/master/src/Hadolint/Rule>

## Dockerfile Label Semantics

Dockerfile `LABEL` sets image metadata as key/value pairs. A single instruction can contain multiple pairs, and Dockerfile line continuation makes
multi-line grouped labels straightforward.

Important behavior for linting:

- Labels are inherited from base images.
- If a label already exists and a later `LABEL` sets the same key to a different value, the most recently applied value wins.
- Modern Docker does not require separate `LABEL` instructions to avoid creating extra layers. Docker's best-practices page explicitly notes that the
  older "combine labels to avoid extra layers" reason no longer applies.
- Values with spaces should be quoted.
- Label keys should follow Docker object-label guidance: reverse-DNS namespaces for custom labels, lowercase alphanumeric boundaries, and punctuation
  such as periods and hyphens in the key body.
- Label values are strings. Docker does not define typed value syntax for ordinary labels; typed validation belongs in an opt-in schema rule.

These semantics make duplicate label keys a real correctness risk and grouped labels a maintainability preference rather than a build-size
optimization.

### BuildKit Parser Reality

Follow-up parser testing against the BuildKit version currently used by Tally (`github.com/moby/buildkit v0.29.0`) showed that Docker's label-key
guidance is not enforced by the Dockerfile parser. BuildKit parses `LABEL` through the same name/value path used for `ENV`: tokenize shell-like words,
split each word on the first `=`, then reject only malformed key/value structure. The typed instruction parser mainly rejects blank keys.

Accepted by `tally lint` today:

| Input | Current behavior |
|---|---|
| `LABEL bad:key=value` | accepted |
| `LABEL bad/key=value` | accepted |
| `LABEL Bad.Key=value` | accepted |
| `LABEL bad,key=value` | accepted |
| `LABEL "bad key"=value` | accepted at parse time |
| `LABEL key=` | accepted; empty value |
| `LABEL key=a=b` | accepted; value is `a=b` |
| `LABEL key='literal value'` | accepted; single quotes keep the literal value semantics documented by Docker |
| `LABEL key="unterminated` | accepted by Tally's parse/lint pipeline, even though full BuildKit conversion would fail during shell expansion |

Rejected or warned today:

| Input | Current Tally surface |
|---|---|
| `LABEL` | fatal parse error: `LABEL requires at least one argument` |
| `LABEL =value` | fatal parse error: `LABEL names can not be blank` |
| `LABEL key` | fatal parse error: `LABEL must have two arguments` |
| `LABEL key=value trailing` | fatal parse error: `Syntax error - can't find = in "trailing". Must be of the form: name=value` |
| `LABEL key=value withspace` | fatal parse error for the unquoted trailing word |
| `LABEL key value with spaces` | accepted, then reported as `buildkit/LegacyKeyValueFormat` with a fix to `LABEL key="value with spaces"` |

Rule implications:

- `tally/labels/valid-key` cannot rely on BuildKit to reject key characters. It must validate keys itself.
- Dockerfile parser errors are already fatal before normal rule execution. Tally currently prints `Error: failed to lint <file>: ...` and exits with
  config/parse error status `2`, so label rules should not duplicate `LABEL`, `LABEL key`, `LABEL =value`, or trailing unquoted-word diagnostics.
- Shared label facts should record both raw parsed key/value text and BuildKit-style shell-expanded key/value text when that can be computed. Quoted
  keys such as `"com.example.vendor"` are normal Dockerfile syntax and should validate as `com.example.vendor`, not as a literal key containing
  quotes.
- Expansion errors such as an unterminated quote are currently not surfaced by Tally for labels because Tally does not run BuildKit's
  Dockerfile-to-LLB conversion. If this matters, add a separate syntax/facts hardening pass rather than overloading label-key validation.
- A NUL byte in a Dockerfile is not label-specific. Current Tally behavior is odd: syntax-directive detection can write
  `<input>:1:4: invalid character NUL` to stderr while still producing an empty successful report. Treat that as a parser/syntax robustness bug, not
  as a label rule.

## OCI Annotation Model

The OCI image spec defines common annotation keys that are also widely used as Docker image labels:

- `org.opencontainers.image.created`
- `org.opencontainers.image.authors`
- `org.opencontainers.image.url`
- `org.opencontainers.image.documentation`
- `org.opencontainers.image.source`
- `org.opencontainers.image.version`
- `org.opencontainers.image.revision`
- `org.opencontainers.image.vendor`
- `org.opencontainers.image.licenses`
- `org.opencontainers.image.ref.name`
- `org.opencontainers.image.title`
- `org.opencontainers.image.description`
- `org.opencontainers.image.base.digest`
- `org.opencontainers.image.base.name`

The corpus shows these are the dominant label family for modern Dockerfiles. The proposed ordering and schema rules should treat OCI keys as
first-class, but should not assume every image can know all values statically in the Dockerfile.

## Buildx Git Labels

Docker Buildx can add git provenance labels through `BUILDX_GIT_LABELS`.

From Docker's build-variable documentation:

- `BUILDX_GIT_LABELS=1` adds git provenance labels.
- `BUILDX_GIT_LABELS=full` adds a fuller set that includes the source URL.

From the Buildx source:

- `org.opencontainers.image.revision` is populated from the git commit.
- `org.opencontainers.image.source` is populated from the git remote URL when full labels are requested.
- `com.docker.image.source.entrypoint` is populated from the Dockerfile path. Buildx declares this key as `DockerfileLabel`.

Rule implication:

- Do not blanket-flag `org.opencontainers.image.source` in ordinary Dockerfiles. It appeared 93 times in the 495 corpus pairs and is useful human
  metadata.
- Do flag overlaps when there is evidence that Buildx git labels are enabled for this lint invocation, or when the rule is explicitly configured with
  `buildx-git-labels = "true"` or `buildx-git-labels = "full"`.
- Prefer warning for active conflicts and info for potential conflicts.

The rule should distinguish three modes:

| Mode | Conflicting generated keys |
|---|---|
| `off` | none |
| `true` or `1` | `org.opencontainers.image.revision`, `com.docker.image.source.entrypoint` |
| `full` | `org.opencontainers.image.revision`, `org.opencontainers.image.source`, `com.docker.image.source.entrypoint` |

## Docker Official Best Practices

Docker does publish official guidance relevant to labels, but it is intentionally narrow:

- The Dockerfile reference documents syntax, quoting, inheritance, and override behavior.
- Docker's object-label guide recommends reverse-DNS namespaces for custom keys, says un-namespaced labels are reserved for CLI use, and reserves
  Docker internal namespaces such as `com.docker.*`, `io.docker.*`, and `org.dockerproject.*`.
- Dockerfile best practices include a `LABEL` section, but the only durable modern guidance is to use labels for metadata and quote values with
  spaces. It also notes that the old advice about combining labels to avoid layers is obsolete.
- Docker's object-label guide describes the key format as guidance, not parser behavior. BuildKit does not actively reject many keys that violate the
  recommendation, so Tally can add real value here.

I did not find an official Docker recommendation for a particular logical order of OCI labels. The proposed grouping and ordering rule is therefore a
Tally maintainability rule grounded in corpus patterns and review ergonomics, not an upstream Docker mandate.

## Entrypoint-Aware Label Ownership

The recent Bake and Compose entrypoint support changes the right rule design. Tally can now reason about more than `LABEL` instructions in one
Dockerfile:

- Dockerfile `LABEL` sets image metadata on the stage image.
- Bake `target.labels` sets image labels for a planned build. Docker documents this as equivalent to the `docker build --label` flag.
- Compose `build.labels` sets metadata on the resulting image built for that service.
- Compose service `labels` and `label_file` set metadata on containers created for that service.

These are related, but they are not interchangeable. The same key can be correct in one surface and wrong in another.

Recommended ownership model:

| Label kind | Best home | Examples | Rationale |
|---|---|---|---|
| Stable image identity | Dockerfile `LABEL`, or a shared Bake label target when many Dockerfiles/targets share it | `title`, `description`, `authors`, `vendor`, `licenses`, `documentation`, static `url` | These describe the artifact regardless of where it is built or deployed. Keeping them near the packaged filesystem is reviewable and portable. |
| Build invocation metadata | Bake `target.labels`, Compose `build.labels`, CI `--label`, or Buildx generated labels | `created`, `revision`, `version`, `ref.name`, `source` when supplied by CI, `com.docker.image.source.entrypoint` | These values depend on the build run, target, tag, platform, or git checkout. Orchestrator files and CI are already the source of truth. |
| Runtime/service metadata | Compose service `labels`, Swarm service labels, Kubernetes manifests, systemd/Podman unit labels, or equivalent deployment config | `traefik.*`, `caddy*`, `prometheus.io/*`, `homepage.*`, `diun.*`, `com.centurylinklabs.watchtower.*`, `autoheal.*` | These labels configure how a container is routed, monitored, updated, backed up, or displayed in one deployment. Baking them into an image makes the image less reusable. |
| Distribution/product metadata | Dockerfile `LABEL` or Bake labels, depending on whether the image is one product or a target matrix | Docker extension labels, marketplace labels, MCP/tool catalog labels | These labels are part of how the image is published or discovered. They are image metadata, but they may vary by target. |
| Platform-managed labels | Do not set by hand | `com.docker.compose.project`, `com.docker.compose.service` | Compose reserves its prefix and documents runtime errors for user-specified service labels with that prefix. |

This classification gives Tally a stronger educational story than a generic "labels should be grouped" rule. The tool can explain not just whether a
label is valid, but whether it is being declared at the right layer of the build/deploy stack.

GHCR note: GitHub Container Registry documents `org.opencontainers.image.source=https://github.com/OWNER/REPO` as the label used to associate a
container package with a GitHub repository when publishing to GHCR. This is a legitimate reason to keep `org.opencontainers.image.source` in the
Dockerfile or build labels. Tally should validate and de-duplicate it, not discourage it broadly.

### Bake Patterns

Bake is often the cleanest owner for dynamic image labels. Two representative corpus patterns:

- `vllm-project/vllm` defines a `_labels` target and has real targets inherit it. This keeps shared metadata out of every Dockerfile and every target.
- `dani-garcia/vaultwarden` defines a label function that fills `created`, `source`, `revision`, and `version` from Bake variables.

These are good patterns. A Tally rule should not push all labels out of Dockerfiles, but it should recognize when a Bake target already owns labels
and when the same label is also maintained in the Dockerfile.

Bake-specific nuances:

- `target.labels` can use `null` to tell the builder to use the Dockerfile label value. That is an explicit fallback and should not be reported as a
  conflict.
- Bake targets can inherit labels from common targets. Repeated label maps across targets are a maintainability smell, but detecting that requires an
  orchestrator-level view of the whole Bake file, not just one Dockerfile invocation.
- Matrix builds make target-specific labels more useful. If `target.name`, tags, platforms, or args vary across matrix outputs, values like
  `version`, `ref.name`, and target-specific descriptions usually belong in Bake.

### Compose Patterns

Compose has two separate label planes:

- `build.labels` labels the built image.
- service `labels` and `label_file` label containers created for the service.

This is a common source of human mistakes:

- OCI image labels under service `labels:` do not annotate the built image.
- Traefik, Caddy, Watchtower, Homepage, Prometheus, or backup labels under Dockerfile `LABEL` or Compose `build.labels` may work in some Docker
  runtimes, but they are deployment policy and usually belong on the service.
- `com.docker.compose.*` labels are Compose-managed and should not be written by users.

The rule design should use the invocation context to choose the message:

```text
compose service label "org.opencontainers.image.source" labels containers, not the image built for service "api"; move it to build.labels or the Dockerfile if it is image metadata
```

```text
label "traefik.http.routers.api.rule" is deployment routing metadata; with a Compose entrypoint, keep it in the service labels for the service that runs this image
```

### What Should Stay In The Dockerfile

Keep labels in the Dockerfile when the label describes the reusable artifact and is stable across every intended build of that Dockerfile:

```dockerfile
LABEL org.opencontainers.image.title="tigervnc-devenv" \
      org.opencontainers.image.description="TigerVNC server environment" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.documentation="https://github.com/drengskapur/tigervnc"
```

Move labels to Bake or Compose build metadata when the value comes from the invocation:

```hcl
target "image" {
  labels = {
    "org.opencontainers.image.created" = BUILD_CREATED
    "org.opencontainers.image.revision" = GIT_SHA
    "org.opencontainers.image.version" = VERSION
  }
}
```

Move labels to Compose service metadata when the value configures one deployment:

```yaml
services:
  api:
    build:
      context: .
    labels:
      traefik.enable: "true"
      traefik.http.routers.api.rule: Host(`api.example.com`)
```

## Kubernetes Usage

Kubernetes labels are key/value pairs on Kubernetes API objects such as Pods, Deployments, Services, and Nodes. They drive selectors, grouping,
rollouts, and automation through `metadata.labels` and label selectors.

Kubernetes does not use Dockerfile or OCI image config labels for Pod selection or Deployment selectors. Image labels can still be useful to
registries, scanners, admission controllers, or humans, but they are not the native Kubernetes label mechanism.

Kubernetes also publishes recommended object labels such as:

- `app.kubernetes.io/name`
- `app.kubernetes.io/instance`
- `app.kubernetes.io/version`
- `app.kubernetes.io/component`
- `app.kubernetes.io/part-of`
- `app.kubernetes.io/managed-by`

Those keys belong primarily in Kubernetes manifests, Helm charts, and generated resources. A Dockerfile rule should not require them as image labels.

The separate `io.k8s.*` and `io.openshift.*` image-label families do exist in real Dockerfiles. The targeted GitHub search for `LABEL io.k8s` returned
1,724 hits. These are mostly ecosystem-specific image metadata conventions, especially around OpenShift or older image catalog practices. They are not
a reason for Tally to enforce Kubernetes object-label keys in Dockerfiles.

A sharper misuse is labels that mimic Kubernetes `securityContext` fields. The corpus follow-up found this exact pattern in the
[Nephoran Intent Operator Dockerfile](https://github.com/hctsai1006/nephoran-intent-operator/blob/d8fb8c1acb517359e3a571f31af76c2da7d635f3/Dockerfile#L180):

```dockerfile
# Security hardening annotations for container runtime
LABEL io.kubernetes.container.capabilities.drop="ALL" \
      io.kubernetes.container.readOnlyRootFilesystem="true" \
      io.kubernetes.container.runAsNonRoot="true" \
      io.kubernetes.container.runAsUser="65532" \
      io.kubernetes.container.allowPrivilegeEscalation="false" \
      io.kubernetes.container.seccompProfile="RuntimeDefault"
```

From a Kubernetes point of view, this is noise unless a custom admission controller or scanner is explicitly wired to read those labels. Kubernetes'
documented enforcement surface is the Pod or Container `securityContext`; Pod Security Standards also name `securityContext` paths for
`runAsNonRoot`, `runAsUser`, `allowPrivilegeEscalation`, `capabilities.drop`, and `seccompProfile`. A Tally rule should flag this pattern because it
creates the appearance of runtime hardening without changing Kubernetes behavior.

## Hadolint Label Rules

Tally has not yet implemented Hadolint DL3048 through DL3056 or DL3058. Hadolint's label-related rules are:

| Hadolint rule | Upstream meaning | Tally recommendation |
|---|---|---|
| DL3048 | Invalid label key | Implement as `tally/labels/valid-key`; optionally expose a compatibility rule later. |
| DL3049 | Required label is missing | Implement through configurable `tally/labels/schema`. |
| DL3050 | Superfluous label present | Implement as strict mode in `tally/labels/schema`, off unless configured. |
| DL3051 | Configured label is empty | Implement through `tally/labels/schema`; do not make a broad default rule. |
| DL3052 | Configured label is not a valid URL | Implement through `tally/labels/schema` type `url`. |
| DL3053 | Configured label is not RFC3339 | Implement through `tally/labels/schema` type `rfc3339`. |
| DL3054 | Configured label is not SPDX | Implement through `tally/labels/schema` type `spdx`. |
| DL3055 | Configured label is not a git hash | Implement through `tally/labels/schema` type `hash`. |
| DL3056 | Configured label is not SemVer | Implement through `tally/labels/schema` type `semver`. |
| DL3058 | Configured label is not an RFC5322 email | Implement through `tally/labels/schema` type `email`. |

Hadolint's schema mechanism is useful, but it is policy-driven. The best Tally design is a native schema rule plus optional Hadolint compatibility
aliases, not nine always-on label validators.

Two compatibility details matter:

- Dynamic Dockerfile values such as `${VERSION}` should usually be treated as `text` or skipped for typed validation unless the user opts in to strict
  validation. Hadolint's README also warns that variables in labels should use the `text` schema type.
- Hadolint treats Docker-reserved namespaces as invalid label keys. Tally should be more nuanced because real Docker labels such as
  `com.docker.extension.*` and Buildx's `com.docker.image.source.entrypoint` exist. Reserved namespace use should be reported with context and
  allowlists, not treated as a universal hard error.

## Existing Tally Interaction

Tally already has `tally/newline-per-chained-call`, which checks `LABEL` along with `RUN`, `ENV`, and similar instructions. It flags multiple
key/value pairs on one physical line and accepts multi-line chained labels.

The new label grouping rule should coordinate with that behavior:

- `tally/newline-per-chained-call` should continue to handle physical line splitting.
- `tally/labels/prefer-grouped` should handle instruction-level grouping and order.
- Auto-fixes from `prefer-grouped` should emit the grouped label in the multi-line format that `newline-per-chained-call` accepts.

Example target shape:

```dockerfile
LABEL org.opencontainers.image.title="tigervnc-devenv" \
      org.opencontainers.image.description="TigerVNC server environment" \
      org.opencontainers.image.source="https://github.com/drengskapur/tigervnc"
```

## Namespace And Package Shape

Add a dedicated package:

```text
internal/rules/tally/labels/
```

Rule codes:

Because these rules already live under `tally/labels`, rule names should not repeat `label` unless the word is part of an external standard name that
cannot be avoided. Prefer `key`, `metadata`, `scope`, or the specific integration domain instead.

```text
tally/labels/no-duplicate-keys
tally/labels/no-buildx-git-overlap
tally/labels/no-ineffective-stage-metadata
tally/labels/no-invocation-conflicts
tally/labels/prefer-invocation-scope
tally/labels/no-image-keys-in-service
tally/labels/no-service-keys-in-image
tally/labels/no-kubernetes-security-context
tally/labels/prefer-grouped
tally/labels/prefer-stable-order
tally/labels/valid-key
tally/labels/prefer-reverse-dns-keys
tally/labels/schema
tally/labels/require-oci-baseline
```

Registration:

- Add a blank import for `internal/rules/tally/labels` in `internal/rules/all/all.go`.
- Add rule docs under `_docs/rules/tally/labels/*.mdx`.
- Update schema generation/indexing so `[rules.tally.labels.<rule>]` is discoverable and documented.

Implemented for the first rollout increment:

- Added the `internal/rules/tally/labels` package.
- Added the blank import in `internal/rules/all/all.go`.
- Added docs for `tally/labels/no-duplicate-keys` and `tally/labels/valid-key` under `_docs/rules/tally/labels/`.
- Added the Labels section to docs navigation and the rules overview.
- No generated option schema was needed for the first two rules because they expose only the common `severity` setting.

Config shape examples:

```toml
[rules.tally.labels.prefer-grouped]
min-labels = 3

[rules.tally.labels.prefer-stable-order]
order = "oci-logical"
sort-unknown = false

[rules.tally.labels.no-buildx-git-overlap]
buildx-git-labels = "auto" # auto, off, true, full

[rules.tally.labels.prefer-reverse-dns-keys]
strict = false
example-prefix = "com.example"

[rules.tally.labels.schema]
strict = false
allow-dynamic = true

[rules.tally.labels.schema.labels]
"org.opencontainers.image.source" = "url"
"org.opencontainers.image.licenses" = "spdx"
"org.opencontainers.image.created" = "rfc3339"
"org.opencontainers.image.revision" = "hash"
```

## Shared Label Facts

The rules should share one parsed view of labels rather than each rule reparsing raw Dockerfile text.

Recommended facts:

```go
type FileLabelFacts struct {
    Instructions []LabelInstructionFact
    Stages       []StageLabelFacts
}

type StageLabelFacts struct {
    StageIndex        int
    Labels            []LabelPairFact
    EffectiveByKey    map[string]LabelPairFact
    DuplicateGroups   map[string][]LabelPairFact
    HasLabelInstruction bool
}

type LabelInstructionFact struct {
    StageIndex        int
    InstructionIndex  int
    Range             parser.Range
    Pairs             []LabelPairFact
    IsMultiline       bool
    IsContiguousGroup bool
}

type LabelPairFact struct {
    StageIndex       int
    InstructionIndex int
    PairIndex        int
    Key              string // effective BuildKit-style key when statically available
    Value            string // effective BuildKit-style value when statically available
    RawKey           string
    RawValue         string
    IsDynamic        bool
    KeyIsDynamic     bool
    ExpansionError   string
    Range            parser.Range
}

type InvocationLabelFacts struct {
    Labels []InvocationLabelFact
}

type InvocationLabelFact struct {
    Key          string
    Value        string
    Scope        string // "image-build" or "service-container"
    SourceKind   string // "bake", "compose", "dockerfile-cli", "ci"
    SourceFile   string
    SourceName   string // Bake target or Compose service
    SourcePath   string // e.g. target.labels, services.api.build.labels, services.api.labels
    IsNullFallback bool // Bake null label: use Dockerfile value
}
```

Implementation notes:

- Prefer BuildKit's typed `LabelCommand` for parsed key/value pairs.
- Normalize quoted keys and values with the same shell-lexing behavior BuildKit uses during expansion. Keep raw text separately for source-preserving
  diagnostics and fixes.
- Use source ranges only for diagnostics and fixes.
- Track stage boundaries. Labels in build-only stages should not be treated as final-image metadata unless the lint target is that stage.
- Track stage reachability through `FROM <stage>` separately from filesystem-only references such as `COPY --from=<stage>` and
  `RUN --mount=from=<stage>`. Labels are image config metadata and are inherited through `FROM`, but they are not copied through filesystem transfers.
- Extend the invocation model before implementing cross-source rules. The current `BuildInvocation.Labels` map flattens Bake labels, Compose
  `build.labels`, and Compose service labels. That is useful for simple consumers, but source-aware rules need scope, origin, and source path.
- Do not hardcode OCI annotation key strings in Tally code. Import the OCI image-spec Go constants from
  `github.com/opencontainers/image-spec/specs-go/v1` and use constants such as `v1.AnnotationSource`, `v1.AnnotationRevision`, `v1.AnnotationTitle`,
  and `v1.AnnotationDescription` as the source of truth.
- Preserve comments and ordering in fixes. If comments split label sections, the fixer should avoid crossing them in v1.
- Treat escaped line continuations and quoted values through the existing Dockerfile parser, not through ad hoc shell parsing.

## Rule Specification: `tally/labels/no-duplicate-keys`

Implementation status: implemented as a diagnostic-only rule. The partial auto-fix described below is still deferred.

Purpose: prevent accidental overwrites inside a stage.

Why: Docker applies the most recent label value. Duplicates are easy to miss when labels are split across many instructions or copied between images.
The corpus found duplicate keys in 8 of 130 files.

Report when:

- The same label key appears more than once in the same stage.
- The final value differs from an earlier value, or the earlier value is redundant.

Do not report when:

- The same key appears in different independent stages and both stages are legitimate outputs.
- The file intentionally redefines a base-image label once in the final stage. Tally usually cannot see base image labels statically.

Fix strategy:

- If duplicate labels are in the same contiguous `LABEL` block and the earlier duplicate is identical, remove the earlier pair.
- If duplicate values differ, do not auto-fix by default. Suggest keeping the later value or consolidating manually.
- If duplicates are non-contiguous, emit diagnostics only.

Message examples:

```text
label "org.opencontainers.image.source" is set more than once in this stage; Docker keeps the last value
```

## Rule Specification: `tally/labels/no-buildx-git-overlap`

Purpose: avoid stale hand-maintained labels when Buildx can emit git provenance.

Why: `BUILDX_GIT_LABELS` can populate `org.opencontainers.image.revision`, `org.opencontainers.image.source`, and
`com.docker.image.source.entrypoint`. Static Dockerfile values for these keys can drift from the actual build input.

Activation:

- `buildx-git-labels = "auto"` reads invocation context and, if available, `BUILDX_GIT_LABELS` from the lint environment.
- `buildx-git-labels = "off"` disables the rule.
- `buildx-git-labels = "true"` checks revision and Dockerfile-path labels.
- `buildx-git-labels = "full"` checks revision, source, and Dockerfile-path labels.

Report when:

- A Dockerfile sets one of the keys generated by the active Buildx mode.
- The value is static, or it is dynamic but still duplicates Buildx's responsibility.

Do not report when:

- Buildx git labels are not enabled and the rule is in auto mode.
- `org.opencontainers.image.source` is present without Buildx evidence. This is common and useful in real Dockerfiles.

Fix strategy:

- No automatic deletion in v1.
- Message should explain which Buildx mode creates the conflict.

Message example:

```text
Buildx with BUILDX_GIT_LABELS=full can emit "org.opencontainers.image.source"; remove the Dockerfile label or disable the generated label source
```

## Rule Specification: `tally/labels/no-ineffective-stage-metadata`

Purpose: make the relationship between multi-stage builds and image labels explicit.

Why: Labels are image config metadata. They apply to the stage image that contains the `LABEL` instruction and to later stages that inherit from that
stage with `FROM <stage>`. They do not move through `COPY --from=<stage>` or `RUN --mount=from=<stage>` because those operations transfer filesystem
content, not image configuration. In a typical multi-stage build, labels placed in a builder stage are invisible in the final image.

This is an educational rule as much as a correctness rule. It should help users understand why a scanner, registry UI, or `docker image inspect` does
not show labels they added earlier in the file.

Report when:

- A stage sets publication metadata labels and the default exported stage does not inherit from that stage.
- A stage is referenced only through filesystem-copy mechanisms such as `COPY --from` or `RUN --mount=from`, and its labels are not repeated or
  inherited by the exported stage.
- Schema or baseline rules are configured for final image metadata, but the matching labels only appear in non-exported builder stages.

Publication metadata labels include:

- `org.opencontainers.image.*`
- `org.label-schema.*`
- `maintainer`
- configured schema labels when `tally/labels/schema` is enabled

Do not report when:

- The labeled stage is the default final stage.
- The labeled stage is an explicit configured export target.
- A later stage inherits from the labeled stage with `FROM <stage>`.
- The labels are clearly internal stage metadata and are not in a publication metadata namespace.
- The project config declares multiple published targets and the labeled stage is one of them.

Default target model:

- Without build invocation context, assume the last stage is the default exported image.
- If Tally has Bake, Buildx, or CLI target context, use the configured target stages.
- If multiple targets are configured, evaluate each target independently.

Fix strategy:

- No automatic fix in v1.
- The correct edit depends on intent:
  - move the label block to the exported stage,
  - put the label block on a base stage inherited by the exported stage, or
  - mark the intermediate stage as an intentional published target in config.

Message examples:

```text
label "org.opencontainers.image.source" is set in stage "builder", but the default final image only copies files from that stage; this label will not appear on the built image
```

```text
labels from stage "metadata" are inherited only by stages that use FROM metadata, not by COPY --from=metadata
```

## Rule Specification: `tally/labels/no-invocation-conflicts`

Purpose: prevent split ownership between Dockerfile labels and orchestrator-provided image labels.

Why: Bake `target.labels`, Compose `build.labels`, and CLI `--label` are build-time image label inputs. When they set the same key as a Dockerfile
`LABEL`, the effective value depends on builder merge behavior and is not obvious during code review. Even when values are identical, the duplication
creates future drift.

Report when:

- A Dockerfile `LABEL` key is also set by Bake `target.labels` for the current target.
- A Dockerfile `LABEL` key is also set by Compose `build.labels` for the current service.
- Multiple invocation image-label sources set the same key with different values, for example Compose-derived Bake config plus `docker-bake.hcl`.
- The duplicate key is one of the volatile OCI keys and the orchestrator value appears to be generated from CI variables.

Do not report when:

- A Bake label is explicitly `null`, because Docker documents that as "use the Dockerfile label value".
- The duplicate is a Compose service/container label, because that labels a different object. Use `no-image-keys-in-service` or
  `no-service-keys-in-image` for object-plane mistakes.
- The Dockerfile is being linted directly with no invocation labels.

Severity:

- `warning` when values differ.
- `info` when values are identical but duplicated.

Fix strategy:

- No automatic fix in v1. The right source of truth depends on whether the label is stable image identity or invocation-specific metadata.
- Message should name both sources:

```text
label "org.opencontainers.image.version" is set in this Dockerfile and in bake target "release"; keep stable labels in one place and generated labels in Bake
```

Implementation requirements:

- The current `BuildInvocation.Labels` map is not enough because it loses origin and scope.
- Invocation providers should preserve label facts separately for `bake.target.labels`, `compose.build.labels`, and Compose service `labels`.
- Diagnostics should ideally point at the orchestrator file for orchestrator-only conflicts; if the first implementation can only point at the
  Dockerfile, include the invocation label in the message.

## Rule Specification: `tally/labels/prefer-invocation-scope`

Purpose: recommend moving volatile labels from Dockerfiles into the build invocation when Tally knows an orchestrator owns the build.

Why: Values such as git SHA, build timestamp, released version, target flavor, and build entrypoint are properties of a build run, not of static
Dockerfile source. Projects often thread these through `ARG` solely to feed `LABEL`, but Bake and Compose build labels can express the same metadata
closer to the source of truth.

Report when all are true:

- The lint run has a Bake or Compose invocation context.
- A Dockerfile label is in the volatile set.
- The value is static and likely stale, or the value comes from an `ARG` used only by labels.
- The orchestrator already supplies build labels, tags, platforms, target stage, or CI-style variables that indicate it is the build metadata owner.

Volatile set:

- `org.opencontainers.image.created`
- `org.opencontainers.image.revision`
- `org.opencontainers.image.version`
- `org.opencontainers.image.ref.name`
- `org.opencontainers.image.base.name`
- `org.opencontainers.image.base.digest`
- `com.docker.image.source.entrypoint`
- optionally `org.opencontainers.image.source` when Buildx git labels or orchestrator variables provide the repository URL

Do not report when:

- The Dockerfile is linted directly without invocation context.
- The label is stable identity metadata such as `title`, `description`, `licenses`, `authors`, `vendor`, `documentation`, or `url`.
- The Dockerfile is a standalone reusable artifact and no orchestrator source of truth is visible.
- The label is in a base stage intentionally inherited by all exported targets.

Severity:

- `info` by default.
- `warning` only for obviously stale static values, such as a literal 40-character git hash or a literal RFC3339 `created` timestamp checked into the
  Dockerfile.

Message examples:

```text
label "org.opencontainers.image.revision" is populated from ARG GIT_SHA only for metadata; with bake target "release", prefer target.labels or Buildx git labels
```

```text
label "org.opencontainers.image.created" is a checked-in timestamp; generated build timestamps belong in Bake, Compose build.labels, CI --label, or provenance
```

Good target shapes:

```hcl
target "release" {
  labels = {
    "org.opencontainers.image.revision" = GIT_SHA
    "org.opencontainers.image.version" = VERSION
  }
}
```

```yaml
services:
  api:
    build:
      context: .
      labels:
        org.opencontainers.image.version: ${VERSION}
```

## Rule Specification: `tally/labels/no-image-keys-in-service`

Purpose: catch OCI image metadata declared in Compose service/container labels.

Why: Compose service `labels:` and `label_file` label containers, not the image produced by the service build. Users sometimes put
`org.opencontainers.image.*` labels under the nearest `labels:` block and assume registries or scanners will see them as image metadata.

Report when:

- The invocation source is Compose.
- A service label key matches `org.opencontainers.image.*`, `org.label-schema.*`, or a configured image-label schema key.
- The same key is absent from Dockerfile labels and Compose `build.labels`, making the mistake likely.

Do not report when:

- The key is intentionally a container label and is not in an image metadata namespace.
- The service has no build section and only runs a third-party image. In that case Compose service labels are the only available local metadata
  surface.
- The user config declares that OCI labels should be mirrored onto containers for a specific integration.

Severity:

- `warning` when a service has `build:` and the image label exists only under service `labels:`.
- `info` when the service is image-only or when labels are intentionally mirrored.

Fix strategy:

- No auto-fix in v1.
- Suggested destination:
  - `services.<name>.build.labels` when the label varies by Compose service.
  - Dockerfile `LABEL` when the label is stable for the image regardless of service.

Message example:

```text
compose service label "org.opencontainers.image.source" labels containers, not the built image; move it to build.labels or the Dockerfile if registries should see it
```

## Rule Specification: `tally/labels/no-service-keys-in-image`

Purpose: discourage baking deployment routing, monitoring, update, and dashboard policy into image labels when an orchestrator is available.

Why: Labels such as Traefik routers, Watchtower update hooks, Homepage dashboard entries, Prometheus scrape hints, and backup policies describe a
particular container deployment. Putting them in a Dockerfile or Compose `build.labels` makes every consumer of the image inherit deployment policy.
That can create surprising behavior when the image is reused in another stack.

Report when:

- A Dockerfile `LABEL` or Compose/Bake build label matches a known service-label namespace.
- The lint run has a Compose invocation context that can express the same label as a service label.
- The label value includes deployment-specific details such as hostnames, router names, ports, dashboard grouping, or update policy.

Initial namespace candidates:

| Namespace | Typical owner |
|---|---|
| `traefik.*` | Compose service labels, Swarm service labels, Kubernetes Ingress/CRDs |
| `caddy`, `caddy.*` | Compose service labels for caddy-docker-proxy style integrations |
| `com.centurylinklabs.watchtower.*` | Container/service labels |
| `autoheal.*` | Container/service labels |
| `diun.*` | Container/service labels |
| `homepage.*` | Compose service labels or dashboard config |
| `prometheus.io/*` | Kubernetes annotations/labels, service labels, or scraper config |
| `ofelia.*` | Container/service labels |
| backup-tool labels such as `docker-volume-backup.*` | Container/service labels |

Do not report when:

- The label is part of the image product itself. For example, Docker extension marketplace labels should remain image labels.
- The integration explicitly documents image labels as the intended source and no Compose service context exists.
- The Dockerfile includes files referenced by the label and the label is intentionally baked for all deployments. Watchtower lifecycle hook labels can
  be intentional in this category, so default severity should stay low.

Severity:

- `info` by default.
- `warning` only for labels with obvious environment-specific values, such as hardcoded public hostnames in `traefik.http.routers.*.rule`.

Fix strategy:

- No auto-fix in v1.
- If the invocation source is Compose and the service is known, suggest moving the label to `services.<name>.labels`.

Message example:

```text
label "traefik.http.routers.web.rule" configures a specific deployment; with compose service "web", prefer service labels instead of image labels
```

## Rule Specification: `tally/labels/no-kubernetes-security-context`

Purpose: prevent Dockerfile, Bake, or Compose labels from pretending to enforce Kubernetes container security settings.

Why: Kubernetes does not read image labels, Docker container labels, or Compose labels to set `securityContext`. Settings such as `runAsNonRoot`,
`runAsUser`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation`, capabilities, and seccomp belong in a Kubernetes Pod or workload manifest, Helm
chart, Kustomize patch, policy engine, or admission controller. Putting them in `LABEL` creates security theater: the file looks hardened to a human
reviewer while the runtime behavior is unchanged.

Report when:

- A Dockerfile `LABEL`, Bake `target.labels`, Compose `build.labels`, or Compose service `labels` key has prefix `io.kubernetes.container.` and the
  suffix maps to a Kubernetes `securityContext` field.
- Multiple matching keys appear together, because that strongly signals an attempted security profile encoded as metadata.
- Nearby comments use enforcement language such as `security hardening`, `runtime hardening`, `CIS`, `Pod Security`, `restricted`, `non-root`, or
  `seccomp`.

Initial exact keys:

| Label key | Kubernetes field that actually enforces it |
|---|---|
| `io.kubernetes.container.runAsNonRoot` | `spec.securityContext.runAsNonRoot` or `spec.containers[*].securityContext.runAsNonRoot` |
| `io.kubernetes.container.runAsUser` | `spec.securityContext.runAsUser` or `spec.containers[*].securityContext.runAsUser` |
| `io.kubernetes.container.readOnlyRootFilesystem` | `spec.containers[*].securityContext.readOnlyRootFilesystem` |
| `io.kubernetes.container.allowPrivilegeEscalation` | `spec.containers[*].securityContext.allowPrivilegeEscalation` |
| `io.kubernetes.container.capabilities.drop` | `spec.containers[*].securityContext.capabilities.drop` |
| `io.kubernetes.container.capabilities.add` | `spec.containers[*].securityContext.capabilities.add` |
| `io.kubernetes.container.seccompProfile` | `spec.securityContext.seccompProfile.type` or `spec.containers[*].securityContext.seccompProfile.type` |
| `io.kubernetes.container.seccompProfile.type` | `spec.securityContext.seccompProfile.type` or `spec.containers[*].securityContext.seccompProfile.type` |
| `io.kubernetes.container.privileged` | `spec.containers[*].securityContext.privileged` |
| `io.kubernetes.container.procMount` | `spec.containers[*].securityContext.procMount` |
| `io.kubernetes.container.appArmorProfile` | `spec.containers[*].securityContext.appArmorProfile.type` |

Do not report when:

- The key is an unrelated Kubernetes or OpenShift catalog/image label, such as `io.k8s.display-name`, `io.k8s.description`, or `io.openshift.tags`.
- The rule is configured with `allowed-keys` or `allowed-prefixes` for a documented custom scanner, admission controller, or policy pipeline that
  consumes these labels.
- The file is an example or test fixture whose surrounding text explicitly demonstrates that these labels are not enforced by Kubernetes.

Severity:

- `info` for a single key without misleading comments.
- `warning` when several keys appear as a profile, or when comments claim runtime hardening or compliance.
- `warning` when future invocation facts prove the image is deployed through Kubernetes manifests, Helm, or Kustomize.

Fix strategy:

- No auto-fix. Tally should not synthesize Kubernetes manifests or delete claimed security intent.
- Suggest moving the policy to the actual runtime surface.
- For Kubernetes, show the equivalent shape:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  seccompProfile:
    type: RuntimeDefault
```

For Compose-only projects, the message can mention service-level Compose fields such as `user`, `read_only`, `cap_drop`, and `security_opt`, but
should avoid implying that those are Kubernetes equivalents.

Message examples:

```text
label "io.kubernetes.container.runAsNonRoot" looks like a Kubernetes securityContext setting, but Kubernetes will not enforce it from image labels; set spec.containers[*].securityContext.runAsNonRoot instead
```

```text
these io.kubernetes.container.* labels describe a security profile but are only metadata; move the hardening to the Kubernetes Pod or workload securityContext, or configure an allowlist if a custom admission controller consumes them
```

Configuration:

```toml
[rules.tally.labels.no-kubernetes-security-context]
allowed-keys = []
allowed-prefixes = []
```

Implementation notes:

- This rule should use the shared label facts layer and the invocation label facts once available.
- Keep matching exact-key based in v1. Do not report all `io.kubernetes.*` labels.
- The rule is educational and should link to Kubernetes security context documentation, not Docker label syntax documentation.

## Rule Specification: `tally/labels/prefer-grouped`

Purpose: organize labels into a stable, reviewable block.

Why: The corpus found 49 of 130 files with three or more `LABEL` instructions, while 33 already used multi-line labels. Grouping improves diffs
because related metadata changes appear together. Ordering is handled by `tally/labels/prefer-stable-order`; the grouping fixer should call the same
ordering helper when it emits a new grouped block.

Report when:

- A stage has at least `min-labels` label pairs spread across multiple adjacent `LABEL` instructions.
- A stage has many single-pair `LABEL` instructions that can be safely grouped.

Do not report when:

- Labels are separated by instructions that affect value expansion, such as `ARG` or `ENV`.
- Comments create meaningful subsections and v1 cannot preserve them.
- Labels are in different stages.
- The file already uses a multi-line grouped `LABEL` instruction.

Default config:

```toml
[rules.tally.labels.prefer-grouped]
min-labels = 3
```

Fix strategy:

- Safe fix only for adjacent label instructions in the same stage.
- Emit one multi-line `LABEL` instruction.
- Preserve existing quote style where possible.
- Use the same stable-order helper as `tally/labels/prefer-stable-order`.
- Skip the fix when duplicate keys exist, because duplicate-key order changes behavior.

Preferred output example:

```dockerfile
LABEL org.opencontainers.image.title="tigervnc-devenv" \
      org.opencontainers.image.description="TigerVNC server environment" \
      org.opencontainers.image.source="https://github.com/drengskapur/tigervnc" \
      org.opencontainers.image.authors="Example Maintainers" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.base.name="tigervnc-base"
```

This order is not purely alphabetical. It follows how humans usually scan image metadata: what the image is, where its source and documentation live,
who owns it, what version or revision it represents, and what it is based on.

## Rule Specification: `tally/labels/prefer-stable-order`

Purpose: keep label keys in a deterministic, human-readable order inside each logical label block.

Why: Humans in the corpus were already spending review effort on ordering. Some files used clearly grouped multi-line labels, and Bake examples often
used shared label maps with a consistent order. The most common keys also cluster into stable conceptual groups: source, description, authors, title,
licenses, vendor, version, URL, documentation, created, revision, and base image metadata. Tally should preserve that maintenance value with a stable
ordering rule instead of leaving each Dockerfile to grow by copy/paste accident.

This should be a separate rule from grouping:

- `prefer-grouped` decides whether scattered `LABEL` instructions should become one block.
- `prefer-stable-order` decides whether keys inside an existing block, or a block produced by a fixer, are ordered consistently.

Default config:

```toml
[rules.tally.labels.prefer-stable-order]
order = "oci-logical" # oci-logical, lexical
sort-unknown = false
```

Report when:

- A single multi-pair `LABEL` instruction has known labels out of configured order.
- Adjacent `LABEL` instructions form one logical label block and their keys are out of configured order.
- Bake `target.labels` or Compose `build.labels` are out of order, once orchestrator source-map diagnostics are available.

Do not report when:

- Duplicate keys exist in the block. `tally/labels/no-duplicate-keys` should report first because reordering duplicates can change the effective
  image metadata.
- Comments split the block into meaningful subsections.
- Labels are separated by instructions or blank-line/comment structure that suggests distinct logical sections.
- The block has only unknown/custom keys and `sort-unknown = false`.

Default `oci-logical` order:

| Rank | Group | Key order |
|---:|---|---|
| 1 | Identity | `org.opencontainers.image.title`, `org.opencontainers.image.description` |
| 2 | Source and references | `org.opencontainers.image.source`, `org.opencontainers.image.url`, `org.opencontainers.image.documentation` |
| 3 | Ownership and legal | `org.opencontainers.image.authors`, `org.opencontainers.image.vendor`, `org.opencontainers.image.licenses` |
| 4 | Release and provenance | `org.opencontainers.image.version`, `org.opencontainers.image.revision`, `org.opencontainers.image.created`, `org.opencontainers.image.ref.name` |
| 5 | Base image | `org.opencontainers.image.base.name`, `org.opencontainers.image.base.digest` |
| 6 | OpenShift and Kubernetes image catalog metadata | `io.k8s.display-name`, `io.k8s.description`, `io.openshift.tags`, `io.openshift.expose-services`, `io.openshift.s2i.scripts-url` |
| 7 | Docker ecosystem metadata | `com.docker.extension.*`, known `com.docker.*` allowlisted keys |
| 8 | Legacy metadata | `org.label-schema.*`, `maintainer` |
| 9 | Unknown reverse-DNS namespaces | Preserve current relative order by default; optionally sort lexically within namespace when `sort-unknown = true`. |
| 10 | Unknown unqualified keys | Preserve current relative order by default. |

Rationale:

- Start with what the image is. `title` and `description` are the fastest review anchors.
- Put `source` before generic URL/documentation because GHCR and other registries use it as a repository connection signal.
- Keep ownership/legal before volatile build values so stable policy metadata is separated from generated metadata.
- Keep `created` after `revision`; timestamps churn more often and should not visually lead the block.
- Keep base-image labels after release/provenance because they describe dependency context, not the image identity.
- Keep ecosystem and legacy keys after OCI keys so modern portable metadata stays first.

Implementation notes:

- Use OCI image-spec Go constants from `github.com/opencontainers/image-spec/specs-go/v1` for known OCI keys. The string table above is documentation,
  not an instruction to hardcode literals in rule code.
- The comparator should return a stable `(groupRank, keyRank, namespaceRank, originalIndex)` tuple.
- Unknown reverse-DNS keys should be grouped by namespace when `sort-unknown = true`, but the default should preserve their relative order to avoid
  disruptive churn.
- The rule should be reusable by Dockerfile labels, Bake `target.labels`, and Compose `build.labels` once source maps are available for orchestrator
  files.

Fix strategy:

- Safe fix within a single `LABEL` instruction when keys are unique and no inline comments are embedded.
- Safe fix across adjacent one-pair `LABEL` instructions only when `prefer-grouped` is also applying or when the block is already semantically
  contiguous.
- No fix across comments, `ARG`, `ENV`, stage boundaries, or duplicate keys.

## Rule Specification: `tally/labels/valid-key`

Implementation status: implemented for Dockerfile `LABEL` keys. Bake and Compose label sources still need source-aware invocation facts before this
rule can validate non-Dockerfile labels.

Purpose: catch malformed label keys and risky namespace use.

Why: Docker has documented label-key guidance, but BuildKit accepts many keys that violate it. Hadolint's DL3048 proves this catches real mistakes.
Tally should implement useful validation without over-reporting on legitimate Docker ecosystem keys.

Scope:

- Dockerfile `LABEL` keys.
- Bake `target.labels` and Compose `build.labels` once source-mapped invocation labels are available.
- Compose service labels only for reserved Docker namespaces in v1. Service-label ecosystems often have their own key syntax, so full validation there
  would be noisy.

Report when:

- The effective key starts or ends with punctuation.
- The effective key contains whitespace, control characters, uppercase letters, or characters outside Docker's documented label-key guidance.
- The effective key has repeated separators that are likely typos, such as `..`, `--`, `.-`, or `-.`.
- The effective key uses a reserved Docker namespace without being on an allowlist.
- The key cannot be statically expanded because the key itself contains Dockerfile variable expansion. This should be `info` because the build may
  still be valid, but Tally cannot reliably check duplicates, order, or schema for that key.

Do not report when:

- The Dockerfile is syntactically invalid in a way BuildKit already rejects, such as `LABEL`, `LABEL key`, `LABEL =value`, or a trailing unquoted
  word.
- The key is unqualified but otherwise valid. That is handled by `tally/labels/prefer-reverse-dns-keys`, not this rule.
- The key is a known OCI, Docker Buildx, Docker extension, OpenShift, Kubernetes image-catalog, or configured organization key.

Reserved namespace handling:

- Docker reserves `com.docker.*`, `io.docker.*`, and `org.dockerproject.*` for internal use.
- Do not blindly flag known ecosystem keys such as `com.docker.image.source.entrypoint` or `com.docker.extension.*`.
- Use lower severity for reserved-namespace issues than for syntactically invalid keys.

Hadolint compatibility:

- This rule can back DL3048 compatibility later.
- Tally should not copy Hadolint's reserved-namespace behavior exactly unless compatibility mode is explicitly enabled.

Implementation notes:

- Validate the BuildKit-style effective key, not the raw token. `LABEL "com.example.vendor"="ACME"` is valid and should be checked as
  `com.example.vendor`.
- Keep raw key/value spans in shared label facts for fixes and precise diagnostics.
- Do not hardcode OCI key strings. Use `github.com/opencontainers/image-spec/specs-go/v1` constants such as `v1.AnnotationSource` and
  `v1.AnnotationRevision` for known OCI annotations.

Message examples:

```text
label key "Bad.Key" uses uppercase characters; Docker recommends lower-case label keys
```

```text
label key "bad/key" contains "/" which is outside Docker's documented label-key guidance; use a reverse-DNS key such as "com.example.bad-key"
```

## Rule Specification: `tally/labels/prefer-reverse-dns-keys`

Purpose: teach Docker's namespacing recommendation for custom label keys.

Why: Docker's object-label guide recommends that custom label keys use reverse-DNS namespaces controlled by the author. It also says un-namespaced
keys are reserved for CLI use. BuildKit will still accept playful or ambiguous keys such as `LABEL flavor=spicy`, so a low-severity Tally rule can
improve long-term metadata ownership without blocking valid Dockerfiles.

Report when:

- A Dockerfile `LABEL`, Bake `target.labels`, or Compose `build.labels` key is otherwise valid but has no namespace separator, for example `version`,
  `description`, `flavor`, `team`, or `service`.
- A custom image-label key uses a vague pseudo-namespace that does not look owned, when strict mode is enabled. Examples: `app.name`, `service.role`,
  `project.url`.

Do not report when:

- The key is an OCI annotation key, using constants from the OCI image-spec Go package.
- The key is in a known ecosystem namespace, such as `io.k8s.*`, `io.openshift.*`, `org.label-schema.*`, allowlisted `com.docker.*`, or configured
  organization prefixes.
- The key is a known legacy key handled by a more specific migration rule, such as `maintainer`.
- The key is a Compose service label in v1. Many service integrations intentionally use non-reverse-DNS namespaces such as `traefik.*` or
  `homepage.*`; placement rules should handle those instead.
- The user has configured `allowed-keys` or `allowed-prefixes`.

Severity:

- `info` by default. This is educational guidance, not a Docker parser error.
- `style` when the output format groups style-only maintainability suggestions separately.
- `warning` only in strict mode.

Configuration:

```toml
[rules.tally.labels.prefer-reverse-dns-keys]
strict = false
allowed-keys = []
allowed-prefixes = []
example-prefix = "com.example"
```

Fix strategy:

- No auto-fix. Tally cannot know which domain the project controls.
- The suggestion should show a placeholder based on `example-prefix` and preserve the original key tail:

```text
label key "flavor" is un-namespaced; Docker recommends reverse-DNS namespaces for custom labels, for example "com.example.flavor"
```

Implementation notes:

- This rule should run after `valid-key`, so invalid keys do not produce both diagnostics.
- Source-aware label facts should expose whether a key is standard, configured, ecosystem-known, reverse-DNS-like, unqualified, or dynamic.
- The rule is especially useful for Dockerfiles with otherwise organized OCI blocks plus stray custom keys, where review intent is clear but ownership
  is not.

## Rule Specification: `tally/labels/schema`

Purpose: provide one configurable engine for required labels and typed values.

Why: Required labels and typed validators are valuable for organizations, but noisy as default lint rules. Hadolint spreads this across DL3049 through
DL3058. A single schema engine is easier to configure and easier to document.

Supported types:

| Type | Validation |
|---|---|
| `text` | non-empty text, or any value if `allow-empty = true` |
| `url` | absolute URL with scheme and host |
| `rfc3339` | RFC3339 timestamp |
| `spdx` | SPDX license expression or identifier |
| `hash` | git hash, preferably 40 lowercase hex characters with optional 7-character short mode |
| `semver` | semantic version |
| `email` | RFC5322 mailbox syntax |

Report when:

- A required configured label is missing from the final target stage.
- A configured label is present but empty.
- A configured label has a value that does not match its configured type.
- Strict mode is enabled and an unconfigured label is present.

Dynamic value handling:

- Default: skip typed validation for values containing Dockerfile expansion, such as `${VERSION}`.
- If `allow-dynamic = false`, report dynamic values for typed labels because the linter cannot prove the value.
- If the schema type is `text`, dynamic values are allowed.

Example config:

```toml
[rules.tally.labels.schema]
strict = false
allow-dynamic = true

[rules.tally.labels.schema.labels]
"org.opencontainers.image.title" = "text"
"org.opencontainers.image.description" = "text"
"org.opencontainers.image.source" = "url"
"org.opencontainers.image.licenses" = "spdx"
"org.opencontainers.image.revision" = "hash"
```

## Rule Specification: `tally/labels/require-oci-baseline`

Purpose: help published images expose a useful minimum metadata set.

Default: off.

Recommended default baseline when enabled:

- `org.opencontainers.image.title`
- `org.opencontainers.image.description`
- `org.opencontainers.image.source`
- `org.opencontainers.image.licenses`

Optional organization-specific baseline:

- `org.opencontainers.image.authors`
- `org.opencontainers.image.vendor`
- `org.opencontainers.image.documentation`
- `org.opencontainers.image.version`

Do not require by default:

- `org.opencontainers.image.created`: often injected by CI or build provenance.
- `org.opencontainers.image.revision`: Buildx can generate it.
- `org.opencontainers.image.base.name`: useful, but not universal and can be misleading with multi-stage builds.
- `org.opencontainers.image.base.digest`: valuable for SBOM/provenance workflows, but many Dockerfiles cannot maintain it manually.

This rule may be redundant if `tally/labels/schema` is configured. It should be either a convenience wrapper over schema or a documented preset, not a
separate independent engine.

## Rule Specification: `tally/labels/prefer-oci-over-legacy-schema`

Post-v1.

Purpose: migrate legacy `org.label-schema.*` metadata to OCI annotations.

Why: `LABEL org.label-schema` still had 3,928 GitHub code-search hits, but OCI labels are the modern convention and were dominant in the corpus.

Potential mapping:

| Legacy key | OCI key |
|---|---|
| `org.label-schema.name` | `org.opencontainers.image.title` |
| `org.label-schema.description` | `org.opencontainers.image.description` |
| `org.label-schema.url` | `org.opencontainers.image.url` |
| `org.label-schema.vcs-url` | `org.opencontainers.image.source` |
| `org.label-schema.vcs-ref` | `org.opencontainers.image.revision` |
| `org.label-schema.version` | `org.opencontainers.image.version` |
| `org.label-schema.vendor` | `org.opencontainers.image.vendor` |
| `org.label-schema.license` | `org.opencontainers.image.licenses` |
| `org.label-schema.schema-version` | no direct replacement; usually remove |

This should be a migration rule with careful messaging, not an immediate default warning.

## Rule Specification: `tally/labels/no-deprecated-maintainer`

Post-v1.

Purpose: prefer OCI `authors` metadata over the legacy `maintainer` label.

Why: The corpus found `LABEL maintainer=...` in 18 of 130 files. BuildKit already reports the deprecated `MAINTAINER` instruction, but that does not
cover the label key.

Report when:

- `LABEL maintainer=...` is present.
- `org.opencontainers.image.authors` is absent or has a conflicting value.

Fix strategy:

- Safe fix when there is exactly one `maintainer` label and no existing `org.opencontainers.image.authors`.
- Convert to:

```dockerfile
LABEL org.opencontainers.image.authors="..."
```

This can be bundled with `prefer-oci-over-legacy-schema` as a broader metadata-modernization pass.

## Additional Misuse Patterns Worth Tracking

These do not all need v1 rules, but they are strong candidates for the `tally/labels/*` namespace once the label facts and invocation-label facts
exist.

### `tally/labels/validate-base`

Purpose: keep `org.opencontainers.image.base.name` and `org.opencontainers.image.base.digest` honest.

Misuse:

- A Dockerfile changes `FROM node:22-alpine` to `FROM node:24-alpine`, but the base label still says `node:22-alpine`.
- A Dockerfile pins `FROM ubuntu@sha256:...`, but `base.digest` is absent or points at an older digest.
- A final stage uses `FROM scratch`, but inherited or copied label blocks claim a runtime base image.

Rule shape:

- Only report when the label is present.
- Compare against the final exported stage's `FROM` reference, after applying known build args when invocation context provides them.
- Do not require base labels by default.
- Severity should be `warning` for mismatches and `info` for unverifiable dynamic bases.

### `tally/labels/prefer-shared-bake`

Purpose: reduce duplicated label maps across Bake targets.

Misuse:

- Ten Bake targets each repeat the same OCI label map.
- A multi-platform matrix repeats all labels except one variable.
- Labels are copied between Bake targets and drift in one target.

Rule shape:

- Requires an orchestrator-level view of the whole Bake file, not just per-Dockerfile rule execution.
- Suggest a shared inherited target, such as `_labels`, or a Bake function returning the shared label map.
- Do not report for two targets only; duplication must be meaningful enough to avoid noisy advice.

Good pattern from corpus:

```hcl
target "_labels" {
  labels = {
    "org.opencontainers.image.source" = "https://github.com/vllm-project/vllm"
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
}

target "openai" {
  inherits = ["_common", "_labels"]
}
```

### `tally/labels/no-static-volatile`

Purpose: catch values that are almost guaranteed to age badly.

Misuse:

- `org.opencontainers.image.revision` is a literal git hash in a checked-in Dockerfile.
- `org.opencontainers.image.created` is a literal timestamp.
- `org.opencontainers.image.version` is hardcoded while Bake tags or Compose image tags vary per target/service.

Rule shape:

- Direct Dockerfile lint can report literal `revision` and `created` values as `info`.
- Invocation-aware lint can upgrade when the orchestrator already has tags, CI variables, or labels for the same value.
- Avoid reporting literal semantic versions by default; many images intentionally version their packaged software in source.

### `tally/labels/no-managed-platform-keys`

Purpose: prevent users from setting labels owned by the runtime or orchestrator.

Misuse:

- Compose service labels include `com.docker.compose.project` or `com.docker.compose.service`.
- Dockerfile labels use Docker-reserved namespaces without being known Buildx or Docker extension labels.
- A user copies platform-injected labels from `docker inspect` output into source.

Rule shape:

- Compose service labels with `com.docker.compose.*` should be `warning` because Docker documents this as a runtime error.
- Dockerfile image labels in Docker-reserved namespaces should be lower severity unless they are definitely invalid. Buildx and Docker extension keys
  need allowlists.
- This can share validation machinery with `tally/labels/valid-key`.

### `tally/labels/no-misleading-image-name`

Purpose: catch stale references between tags, image names, and labels.

Misuse:

- Bake target tags publish `ghcr.io/org/api`, but `org.opencontainers.image.ref.name` or title says `worker`.
- Compose service `image:` is `org/frontend`, but `build.labels` declares a backend title.
- A copied Dockerfile keeps the old `source` repository or documentation URL.

Rule shape:

- Best as an invocation-aware info rule.
- Use strong signals only: Bake target name, Compose service name, image tags, and labels all disagree in obvious ways.
- Do not require label values to equal tags; only report clear copy/paste drift.

### Existing Coverage: Secrets In Labels

Hardcoded secrets in label values are already covered by `tally/secrets-in-code`, which scans `LABEL` values. The labels namespace should not
duplicate that rule. It can, however, link to it from label docs because labels are visible through `docker inspect` and registry metadata.

## Auto-fix Strategy

Auto-fixes should be conservative.

Safe in v1:

- Remove an earlier duplicate pair from the same contiguous label block when the value is identical.
- Group adjacent `LABEL` instructions within the same stage when no comments or dependency-changing instructions are crossed.
- Reorder pairs inside one existing multi-line `LABEL` block when no comments are embedded and all pairs are parseable.

Unsafe in v1:

- Moving labels across `ARG`, `ENV`, `RUN`, `FROM`, `SHELL`, or `USER`.
- Deleting Buildx-overlapping labels automatically.
- Migrating legacy `org.label-schema.*` keys when both old and new keys exist with different values.

The fixer should emit the same style expected by `tally/newline-per-chained-call`, with one key/value pair per physical continuation line.

## Test Plan

Unit tests:

- Duplicate labels in the same instruction.
- Duplicate labels across adjacent instructions.
- Duplicate labels across non-adjacent instructions.
- Same key in separate stages.
- Publication labels in a builder stage that is referenced only through `COPY --from`.
- Publication labels in a base stage inherited by the final stage with `FROM base` should not report.
- Publication labels in a non-last stage should not report when that stage is the configured build target.
- Dockerfile and Bake target set the same image label with different values.
- Bake label set to `null` should suppress a Dockerfile conflict.
- Dockerfile and Compose `build.labels` set the same image label.
- Compose service `labels` contain OCI image keys and should report as container-label placement.
- Compose `build.labels` contain Traefik or Watchtower labels and should suggest service labels.
- Dockerfile contains Traefik labels while linting through a Compose service invocation.
- Compose service labels use `com.docker.compose.*` and should report as managed platform labels.
- Dockerfile contains `io.kubernetes.container.*` security-context labels and should report as non-enforcing metadata.
- A clustered `io.kubernetes.container.*` profile with a `security hardening` comment should report at warning severity.
- `io.k8s.display-name`, `io.k8s.description`, and `io.openshift.tags` should not report as security-context labels.
- Configured `allowed-keys` and `allowed-prefixes` should suppress `no-kubernetes-security-context`.
- Volatile labels populated only from ARGs with a Bake invocation should suggest invocation-scoped labels.
- Base-name and base-digest labels mismatch the final stage base when the base reference is static.
- Buildx overlap for `off`, `true`, and `full` modes.
- `org.opencontainers.image.source` with no Buildx evidence should not report.
- Grouping adjacent one-pair `LABEL` instructions.
- Grouping should not cross comments, `ARG`, `ENV`, or stage boundaries.
- Stable-order diagnostics for known OCI labels in one multi-line `LABEL` block.
- Stable-order fix should preserve unknown custom labels when `sort-unknown = false`.
- Stable-order fix should sort unknown reverse-DNS labels by namespace only when `sort-unknown = true`.
- Stable-order should skip blocks with duplicate keys.
- Key validation for valid OCI keys, invalid punctuation, whitespace, repeated separators, and reserved namespaces.
- Key validation should normalize quoted keys before checking, so `LABEL "com.example.vendor"="ACME"` is valid.
- Key validation should not duplicate BuildKit parse errors for `LABEL`, `LABEL key`, `LABEL =value`, or trailing unquoted words.
- Reverse-DNS preference should report unqualified custom keys such as `flavor` and suppress configured `allowed-keys` and `allowed-prefixes`.
- Reverse-DNS preference should not report OCI keys, known ecosystem keys, or Compose service labels by default.
- Schema validation for URL, RFC3339, SPDX, hash, SemVer, email, missing labels, strict mode, and dynamic values.

Integration tests:

- Add one fixture for a clean grouped OCI block.
- Add one fixture for scattered labels that receive grouped diagnostics and a fix snapshot if the rule supports fixes.
- Add one fixture for a grouped label block with stable-order diagnostics and fix output.
- Add one fixture for duplicate labels in a multi-stage Dockerfile.
- Add one fixture showing an ineffective builder-stage label and the expected educational diagnostic.
- Add one Bake entrypoint fixture with inherited labels and Dockerfile duplicates.
- Add one Bake entrypoint fixture with a `null` label fallback.
- Add one Compose entrypoint fixture where `build.labels` owns image metadata.
- Add one Compose entrypoint fixture where service labels own Traefik labels.
- Add one Compose entrypoint fixture with OCI labels accidentally placed under service `labels:`.
- Add one fixture with Kubernetes security-context-like labels in a Dockerfile and the expected educational diagnostic.
- Add one fixture with playful unqualified image labels and the expected reverse-DNS educational diagnostic.
- Add one fixture for Buildx overlap configured as `full`.

Compatibility tests:

- If Hadolint compatibility aliases are added, verify DL3048 and DL3049-DL3058 map to the same underlying diagnostics as Tally-native schema rules.

## Rollout Plan

1. [x] Add the shared label facts layer and `tally/labels/no-duplicate-keys`.
2. [x] Add `tally/labels/valid-key`, with Docker-reserved namespace allowlists.
3. [ ] Add `tally/labels/prefer-reverse-dns-keys` on top of the same key-classification helper.
4. [ ] Add `tally/labels/no-ineffective-stage-metadata` while the stage graph and label facts are fresh.
5. [ ] Extend invocation label facts so rules can distinguish Bake `target.labels`, Compose `build.labels`, and Compose service labels.
6. [ ] Add `tally/labels/no-invocation-conflicts`.
7. [ ] Add Compose placement rules: `no-image-keys-in-service` and `no-service-keys-in-image`.
8. [ ] Add `tally/labels/no-kubernetes-security-context` as a low-noise educational rule.
9. [ ] Add `tally/labels/prefer-stable-order` and its shared comparator.
10. [ ] Add `tally/labels/prefer-grouped` diagnostics first, then adjacent-block fixes that reuse the stable-order comparator.
11. [ ] Add `tally/labels/no-buildx-git-overlap` with `buildx-git-labels = "auto"` and explicit config modes.
12. [ ] Add `tally/labels/schema` and wire Hadolint compatibility aliases only after schema behavior is stable.
13. [ ] Add docs for all new rules under `_docs/rules/tally/labels/` and make the namespace visible in docs navigation/config schema. Partially
    complete for the first two rules.

Notes:

- Item 13 is partially complete for the first two implemented rules and the namespace navigation.
- The `no-duplicate-keys` auto-fix remains intentionally unimplemented until pair-level source edits are reliable.

## Open Questions

- Should `tally/labels/prefer-grouped` be enabled by default as `info`, or experimental until the fixer proves quiet?
- Should `tally/labels/prefer-stable-order` default to `oci-logical`, or should teams have an easy global switch to pure lexical order?
- Should `tally/labels/no-ineffective-stage-metadata` default to the last stage only, or should it try to infer common multi-target Bake layouts even
  without explicit invocation context?
- Should orchestrator-label diagnostics be allowed to point at Compose or Bake files, or should all current rule diagnostics remain attached to the
  Dockerfile invocation until orchestrator-file source maps are implemented?
- Should `no-service-keys-in-image` default on as `info`, or should it be opt-in because some Docker integrations intentionally read image labels
  through container config?
- Should Tally add a small catalog of well-known service-label namespaces, or should teams configure their own runtime-label namespaces?
- Should repeated Bake labels be handled by per-invocation rules, or does Tally need a separate orchestrator-level lint pass for whole-file
  maintainability rules?
- Should Buildx overlap detection read the process environment in ordinary `tally lint Dockerfile`, or only explicit config and invocation facts?
- Should `tally/labels/prefer-reverse-dns-keys` report common unqualified Docker examples such as `version` and `description` by default, or keep
  those in an initial compatibility allowlist?
- Should schema validation target all stages or only the final target stage by default?
- Should `spdx` accept full SPDX license expressions or only identifiers in v1?
- Should Tally expose Hadolint DL3048-DL3058 codes directly, or document the Tally-native equivalents and defer compatibility?

## Recommendation

Start with the rules that prevent concrete mistakes and improve reviewability without requiring organization policy:

1. `tally/labels/no-duplicate-keys`
2. `tally/labels/valid-key`
3. `tally/labels/prefer-reverse-dns-keys`
4. `tally/labels/no-ineffective-stage-metadata`
5. invocation label facts that preserve label source and scope
6. `tally/labels/no-invocation-conflicts`
7. Compose-aware placement rules for image labels vs service labels
8. `tally/labels/no-kubernetes-security-context`
9. `tally/labels/prefer-stable-order`
10. `tally/labels/prefer-grouped`
11. `tally/labels/no-buildx-git-overlap`

Treat required labels and typed validation as configuration-driven schema behavior, not as always-on defaults. That gives teams Hadolint-equivalent
policy controls without making ordinary Dockerfiles noisy.

The main product direction should be educational and placement-aware: Tally should teach users that labels attach to specific Docker objects. Once
Bake and Compose are entrypoints, Tally can explain whether a label belongs to the reusable image, the build invocation, or the deployed service.
