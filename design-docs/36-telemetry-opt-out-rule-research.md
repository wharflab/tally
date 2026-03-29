# 36. Telemetry Opt-Out Rule Research

## Goal

Build a source-backed knowledge base for a new tally rule that disables telemetry, analytics, or tracking for tools used inside a container image,
while keeping the rule low-noise and stage-scoped.

The target behavior is:

- detect a tool from signals already present in the Dockerfile or copied build context
- inject the tool-specific opt-out only for the stages that actually use that tool
- avoid broad unconditional edits that are weakly supported or likely to be ineffective

## Executive Summary

The research outcome is not "one universal telemetry switch". The durable pattern is a small set of tool-specific opt-outs:

- some tools expose a first-class environment variable that works well in Dockerfiles
- some tools only document a CLI command or config-file switch
- Windows Server has OS-level diagnostic-data controls, but Microsoft does not document a clean container-image-wide telemetry toggle for `servercore`
  or `nanoserver`

The safest v1 rule shape is:

- stage-scoped
- tool-scoped
- `ENV`-first when the vendor officially supports an environment variable
- conservative about command-based opt-outs
- no unconditional "always inject this everywhere" baseline

My recommendation for the rule contract is:

| Property | Recommendation |
|---|---|
| Rule code | `tally/prefer-telemetry-opt-out` |
| Severity | `info` |
| Category | `privacy` |
| Default | `off` initially (`IsExperimental: true`) |
| Auto-fix | yes, mostly sync `SuggestedFix.Edits` |
| Primary edit shape | insert one or more `ENV ...` lines before the first matching instruction in the stage |

If the project prefers stronger wording, `tally/require-telemetry-opt-out` is a possible follow-up once corpus validation proves the rule is quiet
enough.

## High-Confidence Design Conclusions

1. Do not inject anything unconditionally in v1.
2. Do not use `DO_NOT_TRACK=1` as a universal default, but do treat it as a first-class Bun opt-out.
3. Keep Windows handling tool-level, not OS-level.
4. Prefer one-stage-at-a-time insertion and inheritance-aware suppression.
5. Prefer official environment variables over stateful CLI commands.

## Bare Minimum Always-On Injection

The "always inject" set should be empty for v1.

`DO_NOT_TRACK=1` is not a good unconditional baseline, but it is a real vendor-documented Bun mechanism. In this rule it should be treated as:

- a Bun-specific auto-fix when the stage clearly uses Bun, or
- an optional adjunct for the narrow set of other tools that explicitly document it

It should not be injected into every stage, and it should not replace tool-specific variables such as `NEXT_TELEMETRY_DISABLED=1` or
`DOTNET_CLI_TELEMETRY_OPTOUT=1`.

## Knowledge Base

### Tier A: Good v1 Auto-Fix Targets

These have both a container-friendly mechanism and enough supporting evidence to justify targeted injection.

| Tool | Strong Dockerfile signals | Opt-out mechanism | Suggested v1 action | Notes | Sources |
|---|---|---|---|---|---|
| Bun | `FROM oven/bun:*`, `bun install`, `bun add`, `bunx`, `bun run`, copied `bun.lock`/`bun.lockb` when observable | `ENV DO_NOT_TRACK=1` | auto-fix | Bun documents `DO_NOT_TRACK` directly, and `bunfig.toml` `telemetry = false` is explicitly equivalent. This makes `DO_NOT_TRACK` strong for Bun stages even though it is too weak as a universal default. | [Bun bunfig](https://bun.sh/docs/runtime/bunfig), [ReceiptHero Bun Dockerfile](https://github.com/smashah/receipthero-ng/blob/a92a7e9b29c7ed6de1b0ad5815eee1e5b2a1637c/Dockerfile) |
| Azure CLI | `az login`, `az account`, `az deployment`, installed `azure-cli`, official install-script patterns | `ENV AZURE_CORE_COLLECT_TELEMETRY=0` | auto-fix | Microsoft Learn explicitly documents `core.collect_telemetry` plus the `AZURE_{section}_{name}` environment-variable mapping. This is a strong `ENV`-friendly target for container stages that use `az`. | [Azure CLI configuration](https://learn.microsoft.com/en-us/cli/azure/azure-cli-configuration?view=azure-cli-latest) |
| Cloudflare Wrangler | `wrangler deploy`, `wrangler dev`, `npx wrangler`, observable `wrangler.toml` / `wrangler.jsonc` | `ENV WRANGLER_SEND_METRICS=false` | auto-fix | Cloudflare’s official Wrangler telemetry doc supports a command, config-file setting, and environment-variable opt-out. The env var is the best Dockerfile edit. | [Wrangler CLI telemetry](https://github.com/cloudflare/workers-sdk/blob/main/packages/wrangler/telemetry.md) |
| Hugging Face Hub ecosystem | `hf` or `huggingface-cli`, `python -m huggingface_hub`, direct install of `huggingface_hub`, `transformers`, `datasets`, `diffusers`, or `gradio`; observable `requirements*.txt` / `pyproject.toml` / `uv.lock` naming those packages | `ENV HF_HUB_DISABLE_TELEMETRY=1` | auto-fix | Official docs state this globally disables telemetry in the Hugging Face Python ecosystem, and also note that `DO_NOT_TRACK` is equivalent. The Hugging Face-specific variable is clearer for this rule. Detection should stay conservative and require direct Python-side Hugging Face ecosystem evidence, not generic Python stages. As of 2026-03-29, this should not trigger on Node-only usage such as `npx @huggingface/hub upload`, because the official Node `@huggingface/hub` docs reviewed do not document telemetry or `HF_HUB_DISABLE_TELEMETRY` support. | [Hugging Face environment variables](https://huggingface.co/docs/huggingface_hub/en/package_reference/environment_variables), [Hugging Face JS hub CLI](https://huggingface.co/docs/huggingface.js/hub/README), [mcp-memory-service Dockerfile](https://github.com/doobidoo/mcp-memory-service/blob/a901227372a607b8ee6aac7dc1eda21545b6fa03/tools/docker/Dockerfile), [vllm-zarf Dockerfile](https://github.com/fabian1heinrich/vllm-zarf/blob/167ffb6d3d85b3999bfd57e3719520d151137fb8/Dockerfile) |
| Yarn Berry / modern Yarn | `corepack prepare yarn@...`, `corepack enable` plus observable `.yarnrc.yml`, `yarn set version`, observable `package.json` with `packageManager: "yarn@2+"`, copied `.yarn/` tree | `ENV YARN_ENABLE_TELEMETRY=0` | auto-fix | Official docs define `enableTelemetry` and explicitly state that simple settings can be overridden via `YARN_*` snake-case environment variables. The main caveat is detection: plain `yarn install` is too ambiguous because Yarn Classic and Berry share the same command surface. | [Yarn telemetry](https://yarnpkg.com/advanced/telemetry), [Yarn config](https://yarnpkg.com/configuration/yarnrc), [anzusystems/docker-node](https://github.com/anzusystems/docker-node/blob/5ce62125d04c3e30eb30f3be20fdbcf13cfb1699/build/node24/base/Dockerfile) |
| Next.js | `next build`, `next start`, `next dev`, `CMD ["next", ...]`, `ENTRYPOINT ["next", ...]`; optionally `package.json` with `next` when observable | `ENV NEXT_TELEMETRY_DISABLED=1` | auto-fix | Official docs explicitly support env-var opt-out for build and runtime. Real-world Dockerfiles commonly set it. | [Next.js telemetry](https://nextjs.org/telemetry), [vercel/next.js example Dockerfile](https://github.com/vercel/next.js/blob/db08109f6b09b2605b2d06fd9a7df11640e4c668/examples/with-docker/Dockerfile), [makeplane/plane](https://github.com/makeplane/plane/blob/f0468a9173da7834ea20e046b49f1530356c87f3/apps/web/Dockerfile.web), [LiveCVEBench example](https://github.com/livecvebench/LiveCVEBench-Preview/blob/b11057ac5288aa4ed595452f7e5f1df9919dbc00/tasks/LiveCVEBench/cve-2025-7107/Dockerfile) |
| Nuxt | `nuxt`, `nuxi`, `CMD ["nuxt", ...]`, `RUN nuxi build`, or observable `package.json` with `nuxt` | `ENV NUXT_TELEMETRY_DISABLED=1` | auto-fix | Nuxt also supports CLI/config-file disablement, but env var is the most Docker-friendly. | [Nuxt telemetry config](https://v2.nuxt.com/docs/configuration-glossary/configuration-telemetry/), [baserow Dockerfile](https://github.com/baserow/baserow/blob/5b773470663d98b3d52c90851988563e08394780/web-frontend/Dockerfile) |
| Gatsby | `gatsby build`, `gatsby develop`, `CMD ["gatsby", ...]`, or observable `package.json` with `gatsby` | `ENV GATSBY_TELEMETRY_DISABLED=1` | auto-fix | Official env var exists and a few real-world Dockerfiles use it. | [Gatsby telemetry](https://www.gatsbyjs.com/docs/telemetry/), [modino website Dockerfile](https://github.com/Modino-io/modino-website/blob/b3e3abb14a341992f991b2715041b06d0507c908/Dockerfile) |
| Astro | `astro build`, `astro check`, `CMD ["astro", ...]`, or observable `package.json` with `astro` | `ENV ASTRO_TELEMETRY_DISABLED=1` | auto-fix, but experimental | Official docs support the env var. Dockerfile corpus is smaller than Next/Nuxt/Gatsby, so keep under the experimental rule. | [Astro CLI reference](https://docs.astro.build/en/reference/cli-reference/), [playfulprogramming Dockerfile](https://github.com/playfulprogramming/playfulprogramming/blob/facb9e68b50e9d7604c5fc70104ecbbdea7b1c72/Dockerfile) |
| Turborepo | `turbo run`, `turbo prune`, `pnpm turbo ...`, installed `turbo` CLI | `ENV TURBO_TELEMETRY_DISABLED=1` | auto-fix | Official docs also accept `DO_NOT_TRACK=1`, but the tool-specific variable is clearer. | [Turborepo telemetry](https://turborepo.dev/docs/telemetry), [makeplane/plane](https://github.com/makeplane/plane/blob/f0468a9173da7834ea20e046b49f1530356c87f3/apps/web/Dockerfile.web) |
| Vercel CLI | `vercel build`, `vercel deploy`, `npm install -g vercel`, `pnpm add -g vercel` | `ENV VERCEL_TELEMETRY_DISABLED=1` | auto-fix | Real-world Dockerfile evidence is weaker than Next, but the official env var is explicit and container-friendly. | [Vercel CLI telemetry](https://vercel.com/docs/cli/about-telemetry), [LiveCVEBench example](https://github.com/livecvebench/LiveCVEBench-Preview/blob/b11057ac5288aa4ed595452f7e5f1df9919dbc00/tasks/LiveCVEBench/cve-2025-7107/Dockerfile) |
| .NET CLI / SDK | `dotnet build`, `dotnet test`, `dotnet publish`, `dotnet restore`, `dotnet tool`, base image `mcr.microsoft.com/dotnet/sdk:*` | `ENV DOTNET_CLI_TELEMETRY_OPTOUT=1` | auto-fix | Official docs explicitly require setting the variable before install or first CLI run for full coverage. Real-world Linux and Windows Dockerfiles use it. | [.NET CLI telemetry](https://learn.microsoft.com/en-us/dotnet/core/tools/telemetry), [super-linter Dockerfile](https://github.com/super-linter/super-linter/blob/df4f15eb789f18a645638b175f73bfb7d26b2ab5/Dockerfile), [JetBrains TeamCity NanoServer image](https://github.com/JetBrains/teamcity-docker-images/blob/8eb1ba4c4f5e3b4637e97e15abb41640bfd7b693/configs/windows/Agent/nanoserver/NanoServer2022.Dockerfile) |
| PowerShell | `pwsh`, `powershell`, `SHELL ["pwsh", ...]`, `SHELL ["powershell", ...]`, PowerShell base images | `ENV POWERSHELL_TELEMETRY_OPTOUT=1` | auto-fix | Official docs explicitly list accepted values and require setting before process start. Works for Linux and Windows images. | [PowerShell environment variables](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_environment_variables?view=powershell-7.6), [PowerShell-Docker examples](https://github.com/PowerShell/PowerShell-Docker/tree/c074f9414e564cf3d35ac256f0e4219d6f4e6f02), [democratic-csi Windows Dockerfile](https://github.com/democratic-csi/democratic-csi/blob/3974268272a84e9c22c47cae2fca847a8d422bad/Dockerfile.Windows) |
| vcpkg | `vcpkg install`, `bootstrap-vcpkg`, `bootstrap-vcpkg.bat`, copied `vcpkg.exe` | `ENV VCPKG_DISABLE_METRICS=1` | auto-fix | Strong Windows-container fit. Official docs say any value disables telemetry when set. | [vcpkg privacy](https://learn.microsoft.com/en-us/vcpkg/about/privacy), [vcpkg env vars](https://learn.microsoft.com/en-us/vcpkg/users/config-environment), [EttusResearch UHD Windows Dockerfile](https://github.com/EttusResearch/uhd/blob/dd41a0801f185264cd0af1fb8a3ab1306db310b2/.ci/docker/uhd-builder-vs2022-v143-x64-py310.Dockerfile) |
| Homebrew | `brew install`, `/bin/bash -c "$(curl ... Homebrew/install ...)"`, copied `Brewfile` when observable | `ENV HOMEBREW_NO_ANALYTICS=1` | auto-fix | Official docs explicitly support both `brew analytics off` and `HOMEBREW_NO_ANALYTICS=1`. The env var is the better Dockerfile edit, and the signals are specific enough that this does not need to wait for a later version. | [Homebrew Analytics](https://docs.brew.sh/Analytics), [home-operations actions-runner](https://github.com/home-operations/containers/blob/ed4fa4b980f3e6d848fc52d8f6c7fbd4cbc63737/apps/actions-runner/Dockerfile) |

### Tier B: Useful, But Needs More Care

These are real and useful, but they are less attractive as automatic Dockerfile edits.

| Tool | Dockerfile signals | Official mechanism | Why not a simple v1 `ENV` fix | Sources |
|---|---|---|---|---|
| Google Cloud CLI / `gcloud` | `gcloud auth`, `gcloud config`, `gcloud builds`, package install `google-cloud-cli`, base images with Cloud SDK | `gcloud config set disable_usage_reporting true`; env/property form `CLOUDSDK_CORE_DISABLE_USAGE_REPORTING=true` | Google documents both the usage-statistics setting and the generic property-to-environment-variable mapping. However, the same docs also state that `gcloud` does not collect usage statistics unless the user opted in during installation, so injecting a disablement unconditionally is often redundant in containers. | [Usage statistics](https://docs.cloud.google.com/sdk/docs/usage-statistics), [gcloud properties](https://docs.cloud.google.com/sdk/docs/properties), [gcloud topic configurations](https://cloud.google.com/sdk/gcloud/reference/topic/configurations) |
| Go toolchain / `go` command | `go build`, `go test`, `go mod download`, `go install`, base image `golang:*` | `RUN go telemetry off` on Go 1.23+; older toolchains use `gotelemetry off` | This is a real official telemetry system, but it is command-based and version-sensitive. It also writes per-user tool state, which is awkward in multi-user Dockerfiles or stages that later switch `USER`. In addition, Go 1.23 defaults to local-only collection and requires explicit opt-in for uploads, so the privacy urgency is lower than always-on upload systems. | [Go telemetry](https://go.dev/doc/telemetry), [Go 1.23 release notes](https://go.dev/doc/go1.23), [Go telemetry privacy policy](https://telemetry.go.dev/privacy) |
| Gradle | `gradle build --scan`, `./gradlew build --scan`, observable `settings.gradle*` applying the Develocity or Build Scan plugin | `--no-scan` | Build Scan publishing is explicit opt-in, not a default background telemetry channel. The suppression mechanism is command-line based, and stripping or overriding `--scan` is more invasive than injecting an `ENV`. This is a possible future targeted rule, but not a good fit for the current env-first design. | [Build Scan Basics](https://docs.gradle.org/current/userguide/build_scans.html), [Gradle 3.4 release notes](https://docs.gradle.org/3.4/release-notes.html) |
| Angular CLI | `ng`, `npm install -g @angular/cli`, `ng new`, `ng build` | `ng analytics disable` or `ng analytics off` | Official docs prefer a stateful CLI command, not an env var. Real-world Dockerfiles set `NG_CLI_ANALYTICS=off`, but the current official docs do not foreground it. Safer to defer or gate behind a config flag. | [Angular analytics](https://angular.dev/cli/analytics), [Angular analytics disable](https://angular.dev/cli/analytics/disable), [ngcli-docker example](https://github.com/doggy8088/ngcli-docker/blob/5de982cd8125d11ec59c8ba0e2a6c5d46eab0e08/Dockerfile.v6.2) |
| Dart SDK | `dart`, `dart pub`, base image `dart:*` | `RUN dart --disable-analytics` | Official mechanism is command-based, not env-based. This is workable in Dockerfiles, but command insertion is more invasive than `ENV`. | [Dart SDK archive](https://dart.dev/get-dart/archive), [super-linter Dockerfile](https://github.com/super-linter/super-linter/blob/df4f15eb789f18a645638b175f73bfb7d26b2ab5/Dockerfile) |

### Package-Manager Boundary Notes

The package-manager research matters because it is easy to accidentally turn this rule into a "disable random network behavior" rule. The line should
stay tighter than that.

| Tool | Signals the rule might see | What the docs support | Recommendation |
|---|---|---|---|
| Cargo / crates.io | `cargo build`, `cargo check`, `cargo install`, `cargo fetch`, `Cargo.toml`, `Cargo.lock` | In the official Cargo docs reviewed on 2026-03-29, no Cargo telemetry feature or client-side telemetry opt-out surfaced. Cargo does document alternate registries and self-hosted registries, and crates.io counts downloads server-side. | Treat Cargo as a non-target for this rule unless first-party telemetry guidance appears later. If the privacy concern is crates.io-side logging or download statistics, the mitigation is to use an alternate registry, a self-hosted registry, or vendoring, not a telemetry env var. Sources: [Cargo registries](https://doc.rust-lang.org/cargo/reference/registries.html), [Running a registry](https://doc.rust-lang.org/cargo/reference/running-a-registry.html), [crates.io download changes](https://blog.rust-lang.org/2024/03/11/crates-io-download-changes/) |
| Chocolatey / `choco` | `choco install`, `choco upgrade`, `choco source`, Windows build stages using Chocolatey bootstrap/install scripts | Chocolatey’s official docs say the client itself has "zero call home" / "no data collection". Separately, the public Chocolatey Community Repository records package downloads, IP address, and timestamp for install-count statistics, and site usage analytics. | Do not inject a client-side telemetry env var because there is no documented `choco` telemetry toggle to set. If the privacy goal is to avoid community-repository-side data collection, the mitigation is architectural: remove the default `chocolatey` source and use an internal repository. That is better treated as a repository/source-hardening rule or org policy, not this telemetry-env rule. Sources: [Chocolatey security](https://docs.chocolatey.org/en-us/information/security/), [Chocolatey getting started](https://docs.chocolatey.org/en-us/getting-started/), [Chocolatey organizational deployment guide](https://docs.chocolatey.org/en-us/guides/organizations/organizational-deployment-guide), [Chocolatey source command](https://docs.chocolatey.org/en-us/choco/commands/source/) |
| npm | `npm install`, `npm ci`, `npm exec`, `.npmrc`, `package-lock.json` | Official npm docs expose `audit`, `fund`, and `update-notifier` knobs. `audit=true` submits audit reports, `fund=true` prints funding information, and `update-notifier=true` shows version notices. | Do not treat npm as a telemetry target in this rule. `update-notifier=false` is noise reduction, not telemetry. `fund=false` is output suppression. `audit=false` changes a security-relevant behavior and should stay out of scope. Sources: [npm config](https://docs.npmjs.com/cli/v11/using-npm/config), [anzusystems/docker-node](https://github.com/anzusystems/docker-node/blob/5ce62125d04c3e30eb30f3be20fdbcf13cfb1699/build/node24/base/Dockerfile) |
| pnpm | `pnpm install`, `pnpm fetch`, `pnpm add`, `pnpm dlx`, `pnpm-lock.yaml` | In the official-doc review completed on 2026-03-29, no first-party telemetry or analytics toggle surfaced for pnpm. | Do not inject anything for pnpm alone. Use pnpm only as a carrier signal for other tools such as Turbo, Next.js, or Vercel that are installed and run through pnpm. |
| RubyGems / Bundler | `gem install`, `bundle install`, `Gemfile`, `Gemfile.lock`, `*.gemspec` | In the official RubyGems and Bundler docs reviewed on 2026-03-29, no client telemetry feature or telemetry opt-out surfaced. The docs do describe mirrors, private gem servers, and source overrides for `rubygems.org`. | Treat RubyGems and Bundler as non-targets for this rule. If the concern is RubyGems.org-side request logging or public download counts, use a mirror or private gem server instead of inventing a telemetry flag. Sources: [Run your own gem server](https://guides.rubygems.org/run-your-own-gem-server/), [Bundler mirror config](https://bundler.io/man/bundle-config.1.html) |
| Composer / Packagist | `composer install`, `composer update`, `composer global require`, `composer.json`, `composer.lock` | In the official Composer docs reviewed on 2026-03-29, no Composer client telemetry feature or telemetry opt-out surfaced. The docs do explicitly support disabling the default Packagist.org repository and replacing it with private/custom repositories. | Treat Composer as a non-target for this rule. If the concern is Packagist-side traffic or metadata exposure, the mitigation is repository configuration such as disabling Packagist.org or using a private Composer repository, not a telemetry env var. Sources: [Composer repositories](https://getcomposer.org/doc/05-repositories.md), [Composer CLI](https://getcomposer.org/doc/03-cli.md) |
| uv | `uv sync`, `uv pip install`, `uv run`, `uv tool install`, `uv.lock`, `pyproject.toml` | In the official-doc and repository review completed on 2026-03-29, no first-party telemetry or analytics feature, opt-out knob, or telemetry-related environment variable surfaced. | Treat uv as a non-target for this rule unless new first-party guidance appears. Normal registry traffic to indexes such as PyPI is expected package-manager behavior, not evidence of a separate client telemetry system. Sources: [uv environment variables](https://docs.astral.sh/uv/reference/environment/), [astral-sh/uv repository code search for telemetry](https://github.com/astral-sh/uv/search?q=telemetry&type=code), [astral-sh/uv repository code search for analytics](https://github.com/astral-sh/uv/search?q=analytics&type=code) |
| Bun | `bun install`, `bun add`, `bun run`, `bunx`, `FROM oven/bun:*` | Bun explicitly documents `DO_NOT_TRACK=1` and `bunfig.toml` `telemetry = false`. | Treat Bun as the exception that proves the rule: `DO_NOT_TRACK` is justified here because it is vendor-documented for this tool, not because it is a generic privacy convention. Sources: [Bun bunfig](https://bun.sh/docs/runtime/bunfig), [ReceiptHero Bun Dockerfile](https://github.com/smashah/receipthero-ng/blob/a92a7e9b29c7ed6de1b0ad5815eee1e5b2a1637c/Dockerfile) |
| Yarn Classic vs Berry | plain `yarn install` is ambiguous; Berry-specific signals include `.yarnrc.yml`, `.yarn/`, `packageManager: "yarn@2+"`, or `corepack prepare yarn@...` | The modern Yarn docs support telemetry disablement via `enableTelemetry` and environment-variable overrides, but those docs do not apply to Yarn Classic. | Restrict the rule to Berry/modern-Yarn evidence. A bare `yarn` command should not be enough to report or fix. Sources: [Yarn telemetry](https://yarnpkg.com/advanced/telemetry), [Yarn config](https://yarnpkg.com/configuration/yarnrc) |

### Public Cloud CLI Notes

The public-cloud CLI ecosystem is mixed. Some tools clearly ship client telemetry with a documented opt-out. Others primarily generate ordinary API
traffic and do not appear to have a separate telemetry channel.

| Tool | What the official sources show | Recommendation |
|---|---|---|
| AWS CLI / `aws` | In the official docs and repository review completed on 2026-03-29, no first-party AWS CLI client telemetry feature or telemetry opt-out surfaced. The CLI obviously makes API requests to AWS services, but that is not the same as a separate usage-analytics channel. This is an inference from the sources reviewed, not an AWS statement of "no telemetry". Sources: [AWS CLI data protection](https://docs.aws.amazon.com/cli/v1/userguide/data-protection.html), [aws/aws-cli](https://github.com/aws/aws-cli) | Treat the core `aws` CLI as a non-target for this rule unless AWS later documents a dedicated client telemetry system. |
| AWS SAM CLI / `sam` | AWS explicitly documents telemetry collection and `SAM_CLI_TELEMETRY=0` as the environment-variable opt-out. Sources: [Telemetry in the AWS SAM CLI](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/serverless-sam-telemetry.html) | Valid telemetry target in the abstract, but out of scope for this Dockerfile-focused rule. `sam` is primarily a deployment/dev CLI rather than a tool that commonly belongs inside application image builds. |
| AWS CDK CLI / `cdk` | AWS explicitly documents CLI telemetry in CDK versions `2.1100.0` and above, with `CDK_DISABLE_CLI_TELEMETRY=true`, `cdk cli-telemetry --disable`, and config-file alternatives. Sources: [Configure AWS CDK CLI telemetry](https://docs.aws.amazon.com/cdk/v2/guide/cli-telemetry.html) | Valid telemetry target in the abstract, but out of scope for this Dockerfile-focused rule. It is both version-sensitive and primarily a deployment CLI, which makes it a poor fit for the default container-lint collection. |
| Azure CLI / `az` | Microsoft Learn documents anonymous usage-data collection for Azure CLI and exposes `core.collect_telemetry`, with a documented environment-variable mapping to `AZURE_CORE_COLLECT_TELEMETRY`. Sources: [Azure CLI configuration](https://learn.microsoft.com/en-us/cli/azure/azure-cli-configuration?view=azure-cli-latest) | Strong candidate for stage-scoped auto-fix when `az` is present. |
| Google Cloud CLI / `gcloud` | Google documents anonymized usage statistics and the disablement setting `disable_usage_reporting`, but also states that `gcloud` does not collect usage statistics unless the user opted in during installation. The same docs describe the generic property-to-environment-variable mapping, which yields `CLOUDSDK_CORE_DISABLE_USAGE_REPORTING`. Sources: [Usage statistics](https://docs.cloud.google.com/sdk/docs/usage-statistics), [gcloud properties](https://docs.cloud.google.com/sdk/docs/properties) | Real setting, but lower-value for this rule because standard installs are already non-collecting unless opted in. Better as a strict-mode or later-expansion candidate. |
| Cloudflare Wrangler | Cloudflare explicitly documents Wrangler telemetry and supports `WRANGLER_SEND_METRICS=false`, `wrangler telemetry disable`, and `send_metrics=false` in `wrangler.toml`. Sources: [Wrangler CLI telemetry](https://github.com/cloudflare/workers-sdk/blob/main/packages/wrangler/telemetry.md) | Strong candidate for stage-scoped auto-fix when Wrangler is present. |

### Java / Apache Boundary Notes

For the Java ecosystem, the main risk is conflating three different things:

- client telemetry sent back to a vendor or public service
- ordinary dependency-resolution traffic to public repositories
- operator-configured monitoring and diagnostics such as JMX, JFR, `mod_status`, or Spark metrics

Those should not be treated as the same category.

| Tool | What the official sources show | Recommendation |
|---|---|---|
| JDK / JVM in common container distributions (`openjdk`, Eclipse Temurin, Corretto-style images) | The first-party material reviewed on 2026-03-29 focused on JMX and Java Flight Recorder as management, monitoring, profiling, and troubleshooting features. A repository code search on `openjdk/jdk` for `telemetry` did not surface an obvious built-in client telemetry subsystem. This is an inference from the sources reviewed, not an explicit vendor statement of "no telemetry". Sources: [JDK Mission Control](https://docs.oracle.com/en/java/java-components/jdk-mission-control/index.html), [JMX Guide](https://docs.oracle.com/javase/8/docs/technotes/guides/jmx/index.html), [openjdk/jdk search: telemetry](https://github.com/openjdk/jdk/search?q=telemetry&type=code) | Treat the JDK itself as a non-target for this rule. JMX and JFR are observability features under operator control, not something this rule should try to disable. |
| Maven | Official docs emphasize repository configuration and mirrors. The relevant privacy control is where dependencies are fetched from, not a telemetry knob. I did not find first-party Maven client telemetry or an official telemetry opt-out in the docs reviewed on 2026-03-29. Sources: [Using Mirrors for Repositories](https://maven.apache.org/guides/mini/guide-mirror-settings), [Maven settings](https://maven.apache.org/settings.html) | Treat Maven as a non-target for telemetry-env injection. If needed later, handle it under a separate mirror/private-repository rule. |
| Gradle | Build Scan publishing sends build metadata to the Build Scan Service, but it is explicitly enabled with `--scan`, and official docs also document `--no-scan`. This is an explicit opt-in feature rather than silent default telemetry. Sources: [Build Scan Basics](https://docs.gradle.org/current/userguide/build_scans.html), [Gradle 3.4 release notes](https://docs.gradle.org/3.4/release-notes.html) | Do not treat plain Gradle usage as a telemetry target. At most, a future rule could flag or suppress explicit `--scan` usage. |
| Spring Boot / Spring ecosystem | The official Spring Boot docs reviewed on 2026-03-29 focus on Actuator, Micrometer, and OpenTelemetry/OTLP integration as explicit observability features that application operators choose to enable and route. I did not find first-party framework telemetry guidance or an opt-out. This is an inference from the reviewed sources. Sources: [Spring Boot observability](https://docs.spring.io/spring-boot/reference/actuator/observability.html), [Spring Boot monitoring](https://docs.spring.io/spring-boot/reference/actuator/monitoring.html) | Treat Spring Boot as a non-target for this rule. Actuator and OpenTelemetry wiring are application observability decisions, not vendor telemetry toggles. |
| Apache Tomcat | The official docs reviewed on 2026-03-29 focus on JMX, JMXProxyServlet, and management/monitoring features. I did not find first-party Tomcat client telemetry guidance or an opt-out. This is an inference from the reviewed sources. Sources: [Monitoring and Managing Tomcat](https://tomcat.apache.org/tomcat-8.0-doc/monitoring.html), [Tomcat security considerations](https://tomcat.apache.org/tomcat-9.0-doc/security-howto.html) | Treat Tomcat as a non-target for this rule. Monitoring endpoints and JMX exposure are separate hardening questions. |
| Apache HTTP Server (`httpd`) | The official docs reviewed on 2026-03-29 expose server-status and heartbeat modules for operator-visible status and load-balancer coordination. I did not find first-party client telemetry guidance or an opt-out. This is an inference from the reviewed sources. Sources: [mod_status](https://httpd.apache.org/docs/2.4/mod/mod_status.html), [mod_heartmonitor](https://httpd.apache.org/docs/current/mod/mod_heartmonitor.html) | Treat `httpd` as a non-target for this rule. Status and heartbeat modules are operational features, not vendor telemetry. |
| Apache Spark | The official docs reviewed on 2026-03-29 focus on monitoring UIs, event logs, metrics sinks, REST endpoints, and external instrumentation. I did not find first-party Spark client telemetry guidance or an opt-out. This is an inference from the reviewed sources. Sources: [Spark monitoring and instrumentation](https://spark.apache.org/docs/3.5.2/monitoring.html), [Spark Web UI](https://spark.apache.org/docs/latest/web-ui.html) | Treat Spark as a non-target for this rule. Spark metrics, event logs, and UIs are cluster observability features, not vendor telemetry switches. |

### Tier C: Keep Out Of v1

These are real knobs or reasonable ideas, but the detection surface is too weak, the vendor guidance is too indirect, or the mechanism is not a clean
container-image edit.

| Candidate | Why it should stay out of v1 |
|---|---|
| `DO_NOT_TRACK=1` as a universal default | Useful for Bun and a few adjacent tools, but still too weak and underspecified as a blanket rule for every stage. |
| `SCARF_NO_ANALYTICS=true` | Valid for software that embeds Scarf-based telemetry, but Dockerfile signals are usually too weak to know when it applies. |
| Windows OS-level telemetry registry/policy changes such as `AllowTelemetry` | Microsoft documents Windows Server diagnostic-data policy, not a clean container-image-wide switch for `servercore` or `nanoserver`. |
| AWS SAM CLI, AWS CDK CLI, Google Cloud CLI, miscellaneous one-off env vars | These are real or plausible knobs, but the current evidence is either version-sensitive, default-off already, or too weakly connected to typical in-image Dockerfile usage for the smallest v1. |

## Generic Signals vs Tool-Specific Signals

The rule should prefer direct tool signals in this order:

1. direct command execution in `RUN`, `CMD`, `ENTRYPOINT`, or `SHELL`
2. explicit installation of the CLI in the same stage
3. copied observable config or manifest files that clearly name the tool
4. base image identity

Examples:

- `RUN next build` is a stronger Next.js signal than `COPY package.json .`
- `RUN bun install` or `FROM oven/bun:1` is a strong Bun signal, and justifies `ENV DO_NOT_TRACK=1`
- `RUN dotnet publish` is a stronger .NET signal than `FROM mcr.microsoft.com/dotnet/runtime:*`
- `RUN bootstrap-vcpkg.bat` is a stronger vcpkg signal than copying a random `vcpkg.json`
- `RUN yarn install` is not strong enough by itself for Yarn telemetry because it does not distinguish Yarn Classic from Berry

For JavaScript frameworks, `package.json` inspection becomes valuable when the Dockerfile only runs `npm run build` or `yarn build`. This repo already
has observable-file and build-context machinery that makes that possible without inventing a second parser path from scratch.

## Windows Containers

### Tool-level telemetry in Windows containers

Windows containers are well-supported for tool-level opt-outs:

- `POWERSHELL_TELEMETRY_OPTOUT=1`
- `DOTNET_CLI_TELEMETRY_OPTOUT=1`
- `VCPKG_DISABLE_METRICS=1`

These all appear in real Windows Dockerfiles and have primary-source documentation.

### OS-level Windows telemetry

Microsoft documents Windows diagnostic-data controls for Windows Server devices and organizations. The docs explicitly describe:

- Windows diagnostic data collection
- a "Diagnostic data off" setting for Windows Server
- management through policy and MDM

However, that is host and device management guidance, not container-image authoring guidance.

Separately, Microsoft documents that Nano Server omits:

- PowerShell
- WMI
- the Windows servicing stack

That makes a general "inject one Windows OS telemetry-off tweak into every container" a poor fit for this rule.

Recommendation:

- do not auto-inject Windows OS telemetry registry or policy changes in this rule
- keep Windows support focused on tool-level telemetry only
- if OS-wide Windows hardening is desired later, make it a separate Windows policy rule with very explicit scope

Sources:

- [Configure Windows diagnostic data in your organization](https://learn.microsoft.com/en-us/windows/privacy/configure-windows-diagnostic-data-in-your-organization)
- [Overview of Windows Container Base Images](https://learn.microsoft.com/en-us/virtualization/windowscontainers/manage-containers/container-base-images)

## Facts Layer Assessment

The new rule should reuse the facts framework, but it probably does not need a new shared fact type in the first implementation.

Existing facts already cover most of what the rule needs:

- `StageFacts.EffectiveEnv`
- `StageFacts.BaseImageOS`
- `RunFacts.CommandInfos`
- `RunFacts.SourceScript`
- `StageFacts.ObservableFiles`
- inherited env and stage ancestry

That is enough for:

- "already opted out" suppression
- per-stage detection
- inheritance-aware suppression in child stages
- direct command detection from parsed `RUN` commands
- manifest/config inspection when files are observable

If the signal table grows significantly, the reusable part is more likely to be a shared lookup/helper package than a brand-new facts primitive. A
reasonable extraction target would be something like:

- `internal/telemetry/signal.go`
- `internal/telemetry/catalog.go`

with pure functions that consume `RunFacts`, `StageFacts`, and observable files.

## Reuse / Extraction Candidates

The following existing rule behavior is directly relevant:

| Existing code | Reuse value |
|---|---|
| `internal/rules/tally/prefer_curl_config.go` | Good model for stage-scoped "insert config before first matching RUN" with inheritance-aware suppression. |
| `internal/rules/tally/prefer_package_cache_mounts.go` | Good model for removing or suppressing known env-based behavior once a better stage-level mechanism exists, and for working facts-first. |
| `internal/rules/tally/powershell/prefer_shell_instruction.go` | Good detection surface for PowerShell stages and Windows-safe handling. |
| `internal/facts/facts.go` | Existing env inheritance, stage ancestry, command parsing, and observable file support cover most of the rule's data needs. |

## Overlap Inventory

There is no existing telemetry-specific tally rule, so the main coordination risk is shared edit surface.

| Rule | Overlap type | Shared surface | Expected failure mode | Coordination strategy |
|---|---|---|---|---|
| `tally/prefer-curl-config` | indirect | stage-level insertion before first matching `RUN` | dual pre-`RUN` insertions in the same stage | keep telemetry priority lower than `prefer-curl-config` or ensure deterministic insertion order; add combined fix test |
| `tally/powershell/prefer-shell-instruction` | indirect | PowerShell stages, possible insertion near `SHELL` or first PowerShell `RUN` | insertion order churn around `SHELL` and `ENV POWERSHELL_TELEMETRY_OPTOUT` | if both fire, prefer telemetry `ENV` immediately after inserted `SHELL` or before first PowerShell `RUN`; add Windows combined-fix test |
| `tally/prefer-package-cache-mounts` | indirect | stages using `pnpm`, `npm`, `yarn`, etc. | both rules may act on the same stage around the first package-manager `RUN` | ensure edits do not overlap; facts-based suppression for already-set env vars; add combined test on Node stage |
| `tally/prefer-run-heredoc` | indirect | same `RUN` chosen as telemetry insertion anchor | structural rewrite shifts lines around insertion target | keep telemetry fix sync and anchored before the `RUN`; heredoc rule can still rewrite the `RUN` body later |
| `tally/newline-between-instructions` | indirect | blank-line normalization after new `ENV`/`RUN`/`SHELL` insertion | formatting drift | rely on its async resolver; add integration test with rule co-enabled |
| `tally/no-multiple-empty-lines` | indirect | formatting around injected lines | extra blank lines after combined fixes | rely on later formatting pass and add coverage if needed |

Recommended first-pass priority:

- telemetry fix should run before `newline-between-instructions`
- telemetry fix should not overlap text ranges used by content-rewrite rules
- telemetry fix should be line-level insertion only

## Suggested v1 Scope

If the first implementation needs to stay very small, the most defensible subset is:

- Bun
- Azure CLI
- Cloudflare Wrangler
- Hugging Face Hub ecosystem with direct signals only
- Yarn Berry with Berry-specific signals only
- Next.js
- Nuxt
- Gatsby
- Astro
- Turborepo
- .NET CLI
- PowerShell
- vcpkg
- Homebrew

That gives:

- Linux and Windows coverage
- several popular JavaScript frameworks
- ML / model-download stages with direct Hugging Face ecosystem usage
- cloud CLIs with explicit environment-variable opt-outs
- two package managers with explicit Linux-container env-var support
- several Microsoft ecosystems
- one broadly used C/C++ package manager on Windows

The next expansion set would be:

- Google Cloud CLI
- Vercel CLI
- Dart

The explicitly deferred package-manager cases are:

- npm, because the documented knobs are not telemetry toggles and one of them changes security posture
- pnpm, because this research pass did not surface a first-party telemetry switch
- Dart, because the documented opt-out is command-based rather than a simple `ENV`

Angular should likely wait until the rule can confidently manage command-based fixes or the team explicitly accepts the env-var inference path.

## Proposed Test Matrix

The implementation should include at least:

- Linux Bun stage with `bun install` or `bun run`
- Linux modern-Yarn stage with Berry-specific evidence such as `.yarnrc.yml` or `packageManager: "yarn@4"`
- Linux Azure CLI stage with `az` usage
- Linux Wrangler stage with `npx wrangler deploy` or `wrangler deploy`
- Linux Hugging Face stage with `pip install huggingface_hub` or direct `huggingface_hub` / `transformers` usage
- Linux Next.js stage with direct `next build`
- Linux Nuxt stage with `nuxi build`
- Linux Gatsby stage with `gatsby build`
- Linux Astro stage with `astro build`
- Linux Turbo stage with `pnpm turbo run build`
- Linux Homebrew install stage
- Linux .NET SDK stage with `dotnet publish`
- Linux PowerShell stage with `pwsh -Command`
- Windows PowerShell stage
- Windows .NET stage
- Windows vcpkg bootstrap/install stage
- already-disabled env var present
- child stage inherits opt-out from parent stage
- npm-only stage should not report
- pnpm-only stage should not report
- generic Python stage without direct Hugging Face signals should not report
- Node-only `npx @huggingface/hub ...` stage should not report with current evidence
- aws-only stage should not report
- combined-fix case with `prefer-curl-config`
- combined-fix case with `powershell/prefer-shell-instruction`
- combined-fix case with `newline-between-instructions`

## Recommendation

Proceed with a research-driven experimental rule rather than a blanket privacy rule.

Specifically:

- implement `tally/prefer-telemetry-opt-out` as experimental and off by default
- ship v1 with only Tier A tools
- do not inject `DO_NOT_TRACK=1` unconditionally
- do inject `DO_NOT_TRACK=1` for Bun stages
- do not touch Windows OS telemetry in this rule
- use facts plus observable manifest inspection to keep Bun/Yarn/Node-framework detection quiet
- require direct Hugging Face ecosystem signals before injecting `HF_HUB_DISABLE_TELEMETRY=1`
- treat Azure CLI and Cloudflare Wrangler as good cloud-CLI candidates for the first pass

That keeps the rule useful, explainable, and much less noisy than a generic "telemetry bad, set random env vars" approach.
