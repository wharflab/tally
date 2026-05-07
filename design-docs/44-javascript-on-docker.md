# 44. JavaScript Container Rules (`tally/js/*`)

**Status:** Draft

**Research date:** 2026-05-07

**Design Focus:** Propose a dedicated `tally/js/*` namespace for high-value Dockerfile and Containerfile rules that understand how Node.js, Bun,
Deno, and common JavaScript frameworks are actually built and run in containers.

---

## 1. Decision

Introduce a dedicated `tally/js/*` namespace with an initial proposal of **30 JavaScript-oriented rules**.

The namespace should focus on:

1. Package-manager correctness for npm, pnpm, Yarn, and Bun in container builds, including a preference for pnpm's two-step
   `fetch`/offline-install workflow where migration is practical, and BuildKit bind-mounted install inputs instead of copying lockfiles into image
   layers.
2. Runtime-image mistakes that are specific to JavaScript containers: `npm start` as PID 1, dev-server commands in final images, PM2 daemon usage,
   copied `node_modules`, missing production dependency pruning, and weak health probes.
3. Framework-aware Docker patterns, especially Next.js standalone output, public build-time environment variables, and static-export vs server
   runtime confusion.
4. Runtime-specific security wins for Node.js, Bun, and Deno that generic Docker rules cannot express well.
5. Narrow warnings about redundant same-container proxy layers, without rejecting legitimate reverse proxies, ingress controllers, load balancers, or
   static Nginx runtimes.

The namespace should **not** absorb generic Docker hygiene. Avoid broad rules for `apt-get`, generic non-root users, generic multi-stage builds,
generic `COPY . .` advice, or broad "use a reverse proxy" advice. A rule belongs here only when the trigger and remediation are anchored in
JavaScript ecosystem signals such as `package.json`, lockfiles, `node_modules`, `next build`, `.next/standalone`, `bun.lock`, `deno run`, `pm2`,
`NODE_AUTH_TOKEN`, `NEXTAUTH_SECRET`, or known official runtime users like `node` and `bun`.

---

## 2. Ground Truth

### 2.1 Official guidance

The official sources are unusually useful for this namespace:

- The Node.js Docker image docs recommend `NODE_ENV=production`, the provided `node` user, direct `CMD ["node", ...]` instead of `npm start`, and an
  init wrapper such as `--init`, Tini, or `dumb-init` because Node was not designed to run as PID 1.
  Source: [nodejs/docker-node best practices](https://github.com/nodejs/docker-node/blob/main/docs/BestPractices.md)
- The Node.js release page says production applications should use Active LTS or Maintenance LTS lines.
  Source: [Node.js releases](https://nodejs.org/en/about/previous-releases)
- Docker's Node.js guide shows a production multi-stage Dockerfile with `npm ci --omit=dev`, BuildKit cache mounts, a non-root runtime user, and a
  direct Node command.
  Source: [Docker Node.js guide](https://docs.docker.com/guides/nodejs/containerize/)
- Docker's Dockerfile reference documents `RUN --mount=type=bind` as a read-only-by-default way to expose build-context files to one instruction,
  `RUN --mount=type=secret` as a way to use tokens without baking them into an image, and `RUN --network=none` as a way to prove a build step uses
  only already-provided artifacts.
  Source: [Dockerfile reference](https://docs.docker.com/reference/dockerfile)
- Docker's Build secrets docs include the exact private npm registry pattern this namespace should promote: mount `.npmrc` as a secret for the
  package-manager install command.
  Sources: [Docker Build secrets](https://docs.docker.com/build/building/secrets/),
  [Docker GitHub Actions secrets](https://docs.docker.com/build/ci/github-actions/secrets/)
- npm documents `npm ci` as the install mode for automated environments and says it fails instead of updating the lockfile when `package.json` and
  the lockfile disagree. npm also documents `omit=dev` and the `NODE_ENV=production` interaction.
  Sources: [npm ci](https://docs.npmjs.com/cli/v11/commands/npm-ci/), [npm install](https://docs.npmjs.com/cli/v11/commands/npm-install/)
- npm documents `package-lock.json` as describing the exact generated dependency tree so installs can reproduce identical trees regardless of
  intermediate dependency updates. This is the missing transitive-dependency guarantee that package-name version pins do not provide.
  Source: [npm package-lock.json](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/)
- pnpm's Docker recipe recommends small images, multi-stage builds, BuildKit cache mounts, `pnpm install --prod --frozen-lockfile`, `pnpm fetch` for
  CI layer caching, and `pnpm deploy --prod` for workspace deployment artifacts. The `pnpm fetch` docs are unusually explicit that the command is
  designed to improve Docker image builds, uses lockfile/config input instead of package manifests, and supports `pnpm install --offline` afterward.
  Sources: [pnpm Docker recipe](https://pnpm.io/docker), [pnpm fetch](https://pnpm.io/cli/fetch), [pnpm deploy](https://pnpm.io/cli/deploy)
- Yarn recommends `yarn install --immutable` for lockfile immutability and `yarn workspaces focus -A --production` for focused production installs.
  Sources: [Yarn install](https://yarnpkg.com/cli/install), [Yarn workspaces focus](https://yarnpkg.com/cli/workspaces/focus)
- Next.js documents `output: "standalone"` as a way to copy only required runtime files, notes that `.next/standalone` does not include `public` or
  `.next/static` by default, and documents `HOSTNAME=0.0.0.0` for the generated server.
  Sources: [Next.js output](https://nextjs.org/docs/app/api-reference/config/next-config-js/output),
  [Docker Next.js guide](https://docs.docker.com/guides/nextjs/containerize/)
- Next.js self-hosting docs recommend a reverse proxy in front of internet-exposed Next.js servers, and also explain that `NEXT_PUBLIC_*` values are
  inlined during `next build`, which matters for reusable Docker images promoted across environments.
  Source: [Next.js self-hosting](https://nextjs.org/docs/app/guides/self-hosting)
- Node.js documents the Permission Model as stable since v22.13.0/v23.5.0. `node --permission` restricts file-system, network, child process,
  worker, native addon, WASI, and inspector access unless explicitly allowed.
  Source: [Node.js permissions](https://nodejs.org/api/permissions.html)
- Node.js includes a stable global `fetch`, which makes a small JavaScript health probe possible without installing `curl` or `wget` into slim
  runtime images.
  Source: [Node.js globals](https://nodejs.org/docs/latest/api/globals.html)
- node-gyp documents Python, `make`, and a C/C++ compiler toolchain as Unix build prerequisites, downloads Node headers for the target version, and
  exposes `--devdir` as the SDK/header download directory. Its docs recommend `npm_package_config_node_gyp_*` names for node-gyp command options,
  while npm v11 still documents `npm_config_`/`NPM_CONFIG_` environment variables for npm configuration generally.
  Sources: [node-gyp README](https://github.com/nodejs/node-gyp), [npm config](https://docs.npmjs.com/cli/v11/using-npm/config/)
- Vite and Create React App make the same public-env warning: `VITE_*` and `REACT_APP_*` values are build-time client bundle values, not runtime
  secrets.
  Sources: [Vite env variables](https://vite.dev/guide/env-and-mode/),
  [Create React App env variables](https://create-react-app.dev/docs/adding-custom-environment-variables/)
- Bun's Docker guide uses `oven/bun`, separate dev and production installs, `bun install --frozen-lockfile --production`, `.dockerignore` with
  `node_modules`, and final `USER bun`.
  Sources: [Bun Docker guide](https://bun.sh/docs/guides/ecosystem/docker), [bun install](https://bun.sh/docs/cli/install)
- Deno's Docker guide recommends explicit permission flags and multi-stage builds; Deno's security docs emphasize that Deno has no file, network,
  subprocess, or environment access by default. `deno run -A` is documented as testing-only.
  Sources: [Deno and Docker](https://docs.deno.com/runtime/reference/docker/),
  [Deno security](https://docs.deno.com/runtime/fundamentals/security/), [deno run](https://docs.deno.com/runtime/reference/cli/run/)
- Express still recommends `NODE_ENV=production`, a restart mechanism, load balancing, and a reverse proxy such as Nginx or HAProxy for production,
  especially for compression, caching, error pages, and request handling.
  Source: [Express production performance](https://expressjs.com/en/advanced/best-practice-performance.html)
- Fastify explicitly notes that Docker deployments should listen on `0.0.0.0` when the app needs to expose mapped ports.
  Source: [Fastify server reference](https://fastify.dev/docs/latest/Reference/Server/)
- NestJS deployment docs require at least an LTS Node.js runtime, `NODE_ENV=production`, building `dist`, health checks, logging, and a Dockerfile
  shape that can be optimized further for production dependencies.
  Source: [NestJS deployment](https://docs.nestjs.com/deployment)
- PM2 documents `pm2-runtime` as the Docker integration path when PM2 is intentionally used in a container.
  Source: [PM2 Docker integration](https://pm2.io/docs/runtime/integration/docker/)
- Hadolint's `DL3016` still treats `npm install express@4.1.1` as the correct form of npm pinning, while `DL3060` recommends `yarn cache clean`
  after `yarn install`. In a BuildKit-era JavaScript namespace, lockfile-driven frozen installs and cache mounts should supersede both patterns.
  Current upstream `DL3056` is label semantic-version validation, not an npm global-install rule.
  Sources: [Hadolint DL3016](https://github.com/hadolint/hadolint/wiki/DL3016),
  [Hadolint DL3060](https://github.com/hadolint/hadolint/wiki/DL3060),
  [Hadolint DL3056](https://github.com/hadolint/hadolint/wiki/DL3056)

### 2.2 GitHub corpus methodology

I used the GitHub MCP code search tool for seed discovery, then materialized a larger corpus with authenticated GitHub CLI code search plus raw blob
fetches. Seed queries covered:

- general Node Dockerfiles: `FROM node`, `npm ci`, `npm install`, `npm run build`, `NODE_ENV production`
- pnpm-specific files: `pnpm install`, `pnpm fetch`, `pnpm deploy`
- framework and runtime probes: Next.js, NestJS, PM2, Bun, Deno, dev-server commands, and nginx proxy hints

GitHub code-search rate limits cut off some later framework-specific seed queries, so the final materialized corpus is strongest for Node/npm/pnpm
and weaker for Bun, Deno, Fastify, and exact nginx proxy configs. That is acceptable for this proposal because Bun and Deno have strong official
guidance and the rules proposed for them are narrow.

Corpus totals:

| Corpus | Files | Repositories | Notes |
|---|---:|---:|---|
| Raw materialized corpus | 650 | 631 | Fetched Dockerfile/Containerfile-like files after JS relevance filtering. |
| Balanced corpus | 631 | 630 | Excludes known archive/repair/generated repositories and caps any repo at 8 files. |

The raw corpus was not dominated by one template repository. The largest excluded repository was `irvin-s/docker_repair` with 19 Dockerfile-like
files. After exclusion, almost every repository contributed one file.

### 2.3 Corpus findings

Counts below use the balanced corpus.

| Finding | Count |
|---|---:|
| Node official image used somewhere | 557 / 631 |
| Multi-stage Dockerfile | 382 / 631 |
| Explicit `USER` in final stage | 164 / 631 |
| `NODE_ENV=production` signal | 214 / 631 |
| npm signal | 592 / 631 |
| `npm install` | 314 / 631 |
| `npm ci` | 201 / 631 |
| `package-lock.json` plus `npm install` but no `npm ci` | 44 / 631 |
| Direct versioned `npm install <package>@<version>` command | 16 / 631 |
| `npm ci` without explicit `--omit=dev`, `--only=production`, or `--production` on same line | 129 / 201 |
| `npm install` without explicit production pruning flag on same line | 282 / 314 |
| pnpm signal | 171 / 631 |
| `pnpm install` | 145 / 631 |
| `pnpm install` without `--frozen-lockfile` | 50 / 145 |
| `pnpm install` without `--prod` or `--production` on same line | 102 / 145 |
| `pnpm fetch` | 7 / 631 |
| `pnpm deploy` | 18 / 631 |
| Yarn signal | 64 / 631 |
| `yarn install` | 27 / 631 |
| `yarn install` without `--immutable` or `--frozen-lockfile` | 13 / 27 |
| `yarn cache clean` after install | 0 / 631 |
| `corepack enable` | 87 / 631 |
| Global `npm install -g yarn` or `npm install -g pnpm` | 64 / 631 |
| JS native build toolchain signal (`python3`, `make`, `g++`, `build-base`, `build-essential`, or `node-gyp`) | 105 / 631 |
| Explicit native addon or `node-gyp` rebuild signal | 25 / 631 |
| Node-gyp-specific cache or tmpfs optimization | 0 / 631 |
| `COPY` of JS package manifests or lockfiles | 313 / 631 |
| BuildKit `RUN --mount=type=bind` in a JS-relevant Dockerfile | 1 / 631 |
| Final `CMD`/`ENTRYPOINT` delegates to `npm start` | 139 / 631 |
| Final dev-server-like command (`npm run dev`, `next dev`, `vite`, `nodemon`, etc.) | 25 / 631 |
| Next.js signal | 108 / 631 |
| `.next/standalone` copied | 64 / 631 |
| `NEXT_PUBLIC_*`, `VITE_*`, or `REACT_APP_*` in Dockerfile `ARG`/`ENV` | 28 / 631 |
| `HOSTNAME=0.0.0.0` signal | 51 / 631 |
| Next.js telemetry disabled | 53 / 631 |
| Nginx plus Node/npm/pnpm/Next signal | 83 / 631 |
| PM2 signal | 9 / 631 |
| PM2 signal without `pm2-runtime` | 5 / 9 |
| `HEALTHCHECK` | 59 / 631 |
| Healthcheck uses `node -e`, `node --eval`, or `bun --eval` | 16 / 631 |
| Healthcheck uses `curl` or `wget` | 35 / 631 |
| BuildKit `RUN --mount=type=secret` | 5 / 631 |
| JS-auth or runtime-secret-looking variable in Dockerfile | 13 / 631 |
| `pnpm install --offline` | 5 / 631 |
| `pnpm install --offline` with `RUN --network=none` | 0 / 631 |
| Node.js Permission Model flags (`--permission`, `--allow-fs-*`, `--allow-net`) | 0 / 631 |
| Bun signal | 9 / 631 |
| `bun install` | 7 / 631 |
| Deno signal | 2 / 631 |

Interpretation:

- npm reproducibility is still noisy. `npm ci` is common, but `npm install` remains more common even in Dockerfiles that mention a lockfile. Direct
  `npm install <package>@<version>` commands also appear in real Dockerfiles; pinning only the requested package does not lock the transitive tree.
- Copying lockfiles and package manifests remains the default community pattern. BuildKit bind-mounted install inputs are almost absent even though
  Docker's builder can expose those files read-only to one `RUN`.
- pnpm has strong official Docker guidance, but the high-value `pnpm fetch`, offline install, `--network=none`, and `pnpm deploy` patterns are still
  rare in the sampled corpus.
- Native addon cost is under-addressed. More than one hundred Dockerfiles install native build toolchain components or mention node-gyp-adjacent
  packages, but the corpus had no node-gyp-specific header cache, ccache, or tmpfs scratch optimization. This is a good place for Tally to teach a
  modern BuildKit pattern instead of mirroring current practice.
- Many projects now understand Next.js standalone output, but Dockerfiles still bake public client env vars and rely on full framework/runtime images.
- `npm start` as a final container command is common despite Node's official image docs recommending a direct Node command for signal delivery.
- Healthchecks are not rare, but many install or rely on `curl`/`wget`. A smaller JS-specific pattern is to use the runtime already present in the
  image: `node -e` with `http`/`fetch`, or `bun --eval` for Bun images. Bad healthchecks that only print "healthy" should be treated as false
  positives, not good practice.
- Node's Permission Model appears to have near-zero Dockerfile adoption in the sampled corpus and targeted GitHub searches. This is exactly the kind
  of new runtime feature where `tally/js/*` can lead the community rather than codify current habit.
- Secret mounts exist in real Dockerfiles, especially for `.npmrc`, but adoption is low. Copying `.npmrc` or injecting `NPM_TOKEN`/`NODE_AUTH_TOKEN`
  remains a frequent shape that a JS-specific rule can catch with better precision than a generic secret rule.
- Bun and Deno are a small slice of the public Dockerfile corpus, so default-enabled rules for them should be precise and low-noise.
- Nginx appears frequently with JavaScript, but many cases are good static SPA or Next export runtimes. A rule must distinguish "Nginx serves static
  files" from "Nginx and Node run in the same image just to proxy localhost".

### 2.4 Representative examples

These examples are point-in-time Dockerfiles from public repositories, not judgments about the projects as a whole.

| Pattern | Examples |
|---|---|
| Good pnpm `fetch`/offline layer-cache pattern | [`gpitot/fullstack-starter` Dockerfile](https://github.com/gpitot/fullstack-starter/blob/857a46bc97f46626beceb246fa51b1a9e4b96285/Dockerfile), [`depot/examples` pnpm Fastify Dockerfile](https://github.com/depot/examples/blob/ef3967a3c3cddd2ad57d989a798e725291ad8c3a/node/pnpm-fastify/Dockerfile) |
| Good healthcheck uses the JS runtime instead of installing curl/wget | [`lucaszub/sql-practice` Dockerfile](https://github.com/lucaszub/sql-practice/blob/ac8f583d5cacc330244da8ab1a7663f388f7aad0/Dockerfile), [`coltonsteinbeck/silo` Dockerfile](https://github.com/coltonsteinbeck/silo/blob/5d5473f3553cd1fe03a0ac2c302f9c5922d6207a/Dockerfile) |
| Good `.npmrc` secret mount pattern | [`beantownpub/beantown` Dockerfile](https://github.com/beantownpub/beantown/blob/1c69173f5a1ec8eadfb5afaeabf44b263514a9a6/Dockerfile), [`to-nexus/cross-sdk-js` Dockerfile](https://github.com/to-nexus/cross-sdk-js/blob/5224117a9276a647df1ccc8eee1cb90c9709a416/Dockerfile) |
| Node-gyp-related BuildKit cache mounts found by targeted search | [`apache/superset` Dockerfile](https://github.com/apache/superset/blob/5b5dd010285890b4b5b45e707a9c3b0da413f75e/Dockerfile), [`activepieces/activepieces` Dockerfile](https://github.com/activepieces/activepieces/blob/3d4980c120146f955635f6d70ba2e82349b20a20/Dockerfile) |
| Good pnpm workspace deployment pattern | [`nikosyfer/n8n` Dockerfile](https://github.com/nikosyfer/n8n/blob/6fa4a3eaa110c3c1292aac4aac58e3c4c7479f72/docker/images/n8n/Dockerfile), [`dunglas/react-esi` Dockerfile](https://github.com/dunglas/react-esi/blob/b3901888a5182864d9fcd4c38da679f726e30703/Dockerfile) |
| Next.js standalone output copied into runtime | [`302ai/302_llm_playground` Dockerfile](https://github.com/302ai/302_llm_playground/blob/25ae34cca63cf5d0c2ed72d7c1d2b839eb58c5e3/Dockerfile), [`koreo-dev/koreo-ui` Dockerfile](https://github.com/koreo-dev/koreo-ui/blob/f42f7fd23f684dcd78281a53b893781c8331c654/Dockerfile) |
| `COPY .` before package install, hurting JS dependency cache locality | [`Shyam-Chen/Express-Starter` Dockerfile](https://github.com/Shyam-Chen/Express-Starter/blob/90d73b02836c734c2256fe71b741cd3b0823dd19/Dockerfile), [`RanKey1496/nodejs-starter` Dockerfile](https://github.com/RanKey1496/nodejs-starter/blob/67ba3de74a278c62fef41552f63279d964d70283/Dockerfile) |
| `package-lock.json` with `npm install` instead of `npm ci` | [`cainenielsen/plex-cloud` app Dockerfile](https://github.com/cainenielsen/plex-cloud/blob/04f6aa2a691999c08e2c4fbc07571e9f9a0d5b73/app/Dockerfile), [`JoanWu5/cc-music-recommendation-frontend` Dockerfile](https://github.com/JoanWu5/cc-music-recommendation-frontend/blob/21dd89f2a5b7a366cc683ba2baa35a28c33b90f6/Dockerfile) |
| Final container command delegates to `npm start` | [`steeply/gbot-trader` Dockerfile](https://github.com/steeply/gbot-trader/blob/c8240aae4d895338b1658b5346883a25a53a094c/Dockerfile), [`togetherchicago/chi77` frontend Dockerfile](https://github.com/togetherchicago/chi77/blob/09dab3454596d7f461eed0ae7eb9250308f88581/frontend/Dockerfile) |
| Final dev server command | [`abhishek-rs/dockerized-weatherapp` backend Dockerfile](https://github.com/abhishek-rs/dockerized-weatherapp/blob/cd68940d61be41a13a6bcb7dc578b9d0218ddd14/backend/Dockerfile), [`grilario/js-expert-spotify` Dockerfile](https://github.com/grilario/js-expert-spotify/blob/02cd242a416eba58a1de4f24b7a759839be7f238/Dockerfile) |
| Public build-time client env vars in Dockerfile | [`Amarilha/Pricely` Dockerfile](https://github.com/Amarilha/Pricely/blob/0fe9ba43fdc1618a28b63c41e5ca57d515127022/Dockerfile), [`naren-m/katamaster_ui` Dockerfile](https://github.com/naren-m/katamaster_ui/blob/b36887d1cd2c72559fe65f75da795b8d21cfebf2/Dockerfile) |
| Nginx with Node/Next/npm signal requiring classification | [`Redocly/redoc` Dockerfile](https://github.com/Redocly/redoc/blob/d41fd46f7cbee86bf83dc17b7ec51baf54f72a54/config/docker/Dockerfile), [`project-rally/website-rally` Dockerfile](https://github.com/project-rally/website-rally/blob/776090e38d40a821d79cc5e1e143a4b938a08df4/Dockerfile) |

---

## 3. Myth Checks

### 3.1 "Node must always be behind Nginx"

This is too broad.

Official Express and Next.js docs still recommend a reverse proxy for real production edges because it can handle request validation, compression,
caching, rate limits, slow clients, malformed requests, TLS, and load balancing better than the app server. That remains reality-grounded.

The weak pattern is different: one image starts Nginx and Node together, and Nginx only proxies `localhost:3000` with little or no caching, static
serving, TLS, rate limiting, or connection policy. In orchestrated deployments, that layer often duplicates ingress/load-balancer behavior, adds
another process to supervise, hides direct app metrics, and complicates logs and graceful shutdown.

Recommended rule stance:

- Do **not** warn on static SPA/Next export images where Nginx is the only runtime.
- Do **not** warn on Dockerfiles that clearly configure Nginx for static assets, caching, TLS termination, compression, or request limits.
- Do warn, at info/suggestion severity, when the final image appears to co-run Nginx and Node only to proxy localhost.

### 3.2 "PM2 is required in Docker"

Not generally. Docker, Compose, Kubernetes, ECS, Nomad, and systemd units can restart containers, scale replicas, collect logs, and expose health
state. PM2 is still useful when teams intentionally want PM2 cluster behavior, ecosystem files, or PM2-specific monitoring inside one container.

Recommended rule stance:

- Do not ban PM2.
- Warn when a Docker final command uses `pm2 start` or the PM2 daemon instead of `pm2-runtime`.
- If PM2 is not doing cluster or ecosystem-file work, prefer a direct Node command plus an init wrapper.

### 3.3 "`NODE_ENV=production` is enough"

Sometimes, but it is package-manager-specific and easy to misread.

npm uses `NODE_ENV=production` to default `omit` to `dev`, but pnpm, Yarn, and Bun each have their own production/frozen flags. Even with npm,
explicit `npm ci --omit=dev` is easier to review in Dockerfiles and avoids relying on instruction ordering.

Recommended rule stance:

- Allow `NODE_ENV=production` as a compatibility signal.
- Prefer explicit production install flags in runtime dependency stages.
- Do not ask builder stages to omit dev dependencies before `next build`, `vite build`, `tsc`, `nest build`, or native addon compilation.

### 3.4 "Alpine is always smaller and therefore better"

Not enough for a default rule. Alpine is useful, but Node native dependencies often need `python3`, `make`, `g++`, libc compatibility packages, or
framework-specific binaries such as `sharp` or Prisma engines. The Node Docker docs specifically call out `node-gyp` toolchain handling on Alpine.

Recommended rule stance:

- Defer a default `node:alpine` warning.
- Consider a future source-aware rule that flags `node:alpine` plus native packages when the Dockerfile installs build tools in the final stage or
  when known native modules are visible from `package.json`.

### 3.5 "Frontend env vars are runtime config"

For Next.js public env vars, Vite `VITE_*`, and CRA `REACT_APP_*`, this is false. Those values are embedded in the client bundle at build time and
are visible to users. In Docker this creates two failure modes:

- teams expect `docker run -e NEXT_PUBLIC_API_URL=...` to change an already-built image, but it cannot;
- teams accidentally bake public API keys or tenant-specific config into reusable images.

This is a strong default rule candidate because the Dockerfile itself often exposes the mistake through `ARG` or `ENV` names.

### 3.6 "Copying lockfiles is best practice because most official examples do it"

This is becoming stale. Copying `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`, `bun.lock`, or `deno.lock` into a dependency stage was the older
layer-cache pattern. It works, but Tally should not promote it anymore: lockfiles should be inputs to install commands, not files committed into
image layers.

BuildKit gives a better primitive: bind-mount package-manager inputs read-only for the install command, and copy application artifacts only after the
install completes. This keeps install inputs available for cache keys without committing them to a layer.

Recommended rule stance:

- Prefer bind-mounted package-manager inputs for installs. A lockfile should be available to the install command, not committed to an image layer.
- Treat copying lockfiles into a throwaway build stage as a legacy fallback for non-BuildKit builders, not a Tally-recommended target state.
- Keep the older `COPY package*.json ./` guidance out of the initial default rule set except as compatibility guidance for builders without BuildKit.

### 3.7 "Offline pnpm install already proves dependency hermeticity"

Only partly. `pnpm fetch` plus `pnpm install --offline` is valuable because the network download and dependency linking phases are separated. But if
the offline install step still has default build networking, lifecycle scripts and tooling can still make unexpected network calls.

Recommended rule stance:

- Promote `RUN --network=none pnpm install --offline ...` after a `pnpm fetch` step.
- Make this a pnpm-specific rule because npm, Yarn, Bun, and Deno do not have the same first-class `fetch`/offline Docker workflow.
- Treat build failures after adding `--network=none` as useful signal: the install is not actually hermetic.

### 3.8 "Package managers are neutral inside Docker"

Not anymore. npm, Yarn, Bun, and pnpm all have valid lockfile and production-install workflows, but pnpm has a Docker-specific capability that changes
the security and cache story: `pnpm fetch` can populate the store from lockfile/config input before source is copied, then `pnpm install --offline`
can link dependencies after source is present. Adding `RUN --network=none` to that offline install makes unexpected registry or postinstall network
access fail visibly.

Recommended rule stance:

- Prefer pnpm for new or migratable Node.js Docker builds because it has the clearest two-step dependency acquisition and offline linking model.
- Keep this rule at info severity by default because changing package managers rewrites lockfiles and can affect monorepo policy, package scripts,
  hoisting assumptions, and deployment tooling.
- Escalate under a future strict container-hardening profile when an npm/Yarn Dockerfile is already being structurally rewritten for cache/security
  reasons and no package-manager pin blocks migration.
- Do not ask Bun or Deno projects to migrate to pnpm. This rule is about Node package-manager choice, not replacing distinct runtimes.

### 3.9 "Healthchecks are generic, not JavaScript-specific"

The existence of a healthcheck is generic. The shape can be JavaScript-specific.

Node and Bun runtime images already include an HTTP-capable JavaScript runtime. A probe such as `node -e "fetch(...)"` or a small `http.get` snippet
can avoid installing `curl`/`wget` into a slim production image solely for healthchecks. The corpus also showed bad `node -e "console.log('healthy')"`
healthchecks, which only prove the runtime starts and do not test the app.

Recommended rule stance:

- Do not require every Dockerfile to have `HEALTHCHECK`; Kubernetes and other orchestrators often own probes outside the image.
- When a Node/Bun image installs `curl` or `wget` only for a healthcheck, suggest a native JS probe.
- Warn on healthchecks that do not touch the app process, socket, HTTP endpoint, or readiness file.

### 3.10 "Container isolation makes Node's Permission Model redundant"

Container boundaries and Node's Permission Model operate at different layers. Containers restrict Linux resources; `node --permission` restricts
what trusted JavaScript can accidentally access inside that container. This is especially relevant for server apps that should read only `/app`, write
only `/tmp` or an upload directory, and avoid child processes or native addons.

Recommended rule stance:

- Add a low-noise rule for final direct `node` commands on stable Permission Model runtimes, especially Node 22.13+ and 24+.
- Keep the default severity at info while the ecosystem adopts the feature.
- Suggest scoped allow flags, not blanket `--allow-fs-read=* --allow-fs-write=*`.

### 3.11 "Secrets are safe if they are only used during build"

Not if they are passed through `ARG`, committed with `ENV`, copied via `.npmrc`, or written to a layer and removed later. Docker explicitly warns that
build args can be visible in image history and provenance, and Docker Build secrets exist for this case.

Recommended rule stance:

- Promote `.npmrc` secret mounts for private registry installs.
- Warn on `NPM_TOKEN`, `NODE_AUTH_TOKEN`, GitHub tokens, and private registry auth written through `ARG`, `ENV`, `RUN echo`, or copied `.npmrc`.
- Treat runtime secrets such as `JWT_SECRET`, `NEXTAUTH_SECRET`, `AUTH_SECRET`, and `SESSION_SECRET` differently: they should normally be injected by
  the runtime platform, not baked into the image or provided as build secrets.

### 3.12 "Pinning npm package arguments is enough"

This is stale Hadolint-era guidance. `npm install express@4.1.1` pins one requested package spec, but it does not make the full transitive dependency
tree reproducible in the way a committed lockfile plus `npm ci` does. It also encourages Dockerfiles to become dependency manifests instead of
building from the project's package metadata.

Recommended rule stance:

- Do not port `hadolint/DL3016` as-is for JavaScript. Direct package-spec installs should generally move into `package.json` plus a lockfile-backed
  frozen install.
- Add a deprecation entry for `hadolint/DL3016` once `tally/js/no-ad-hoc-npm-install` ships, using the existing superseded-rule mechanism.
- Keep `hadolint/DL3060` as not planned and mark it superseded by `tally/prefer-package-cache-mounts`; `yarn cache clean` is a layer-cleanup habit
  that BuildKit cache mounts make unnecessary in most production Dockerfiles.
- Do not deprecate `hadolint/DL3056` for npm reasons. Current upstream `DL3056` validates label semantic versions; the older local reference that
  described it as npm-global pinning is stale documentation, not an upstream rule to replace.

### 3.13 "tmpfs mounts cache node-gyp builds"

Not exactly. `RUN --mount=type=tmpfs` can put temporary scratch files on memory-backed storage and keep transient compiler output out of layers, but
it does not persist across builds. The persistent wins for node-gyp-heavy images come from cache mounts: package manager caches, node-gyp's header
download directory, and optionally ccache if the image installs and configures it.

Recommended rule stance:

- Promote cache mounts for the package-manager store/cache and node-gyp `--devdir`.
- Accept both node-gyp's documented `npm_package_config_node_gyp_devdir` form and npm's documented `NPM_CONFIG_DEVDIR`/`npm_config_devdir`
  configuration mechanism; do not claim the general npm config environment-variable mechanism is deprecated.
- Optionally suggest `--mount=type=tmpfs,target=/tmp` for scratch I/O during native addon installs, but do not present tmpfs as a replacement for
  cache mounts.
- Never suggest tmpfs for `node_modules` or addon package `build/` output because compiled `.node` artifacts must persist into the image layer.

### 3.14 "Dockerfile-only JavaScript linting is enough"

It is enough for some checks, but the highest-value fixes increasingly need build context. With direct `--context`, Bake, and Compose entrypoints,
Tally can often see whether `package-lock.json`, `pnpm-lock.yaml`, `.npmrc`, `pnpm-workspace.yaml`, `binding.gyp`, or `package.json` are actually
present and not ignored.

Recommended rule stance:

- Use Dockerfile-only signals for conservative diagnostics.
- Use context-aware mode for stronger fixes: verify that bind-mounted lockfiles exist, read `package.json` `packageManager` and workspace metadata,
  detect native dependencies, and avoid suggesting mounts for files that `.dockerignore` excludes.
- Respect Compose/Bake-provided build secrets, healthchecks, target stages, and build contexts when deciding whether a Dockerfile-level warning is
  still actionable.

---

## 4. Applicability Detection

Treat a Dockerfile/Containerfile as JavaScript-relevant when any of these signals appear:

1. Base image matches `node:*`, `oven/bun:*`, `denoland/deno:*`, `keymetrics/pm2:*`, or a JavaScript-focused framework image.
2. Dockerfile copies or bind-mounts package manager files: `package.json`, `package-lock.json`, `npm-shrinkwrap.json`, `pnpm-lock.yaml`,
   `pnpm-workspace.yaml`, `yarn.lock`, `.yarnrc.yml`, `bun.lock`, `bun.lockb`, `deno.json`, `deno.jsonc`, or `deno.lock`.
3. Dockerfile runs package manager commands: `npm ci`, `npm install`, `npm prune`, `pnpm install`, `pnpm fetch`, `pnpm deploy`, `yarn install`,
   `yarn workspaces focus`, `bun install`, `deno run`, `deno install`, or `deno task`.
4. Framework signals appear: `next build`, `.next/standalone`, `.next/static`, `next start`, `next dev`, `vite build`, `react-scripts build`,
   `nest build`, `dist/main`, `fastify`, `express`, `nuxt`, `astro`, or `svelte-kit`.
5. Runtime process manager, probe, hardening, or proxy signals appear with Node commands: `pm2`, `pm2-runtime`, `nginx`, `proxy_pass`,
   `HEALTHCHECK`, `node -e`, `bun --eval`, `--permission`, `--allow-fs-read`, `--allow-net`, `supervisord`, or `concurrently`.
6. JavaScript package-auth or runtime-secret signals appear: `.npmrc`, `NPM_TOKEN`, `NODE_AUTH_TOKEN`, `NEXTAUTH_SECRET`, `AUTH_SECRET`,
   `JWT_SECRET`, or `SESSION_SECRET`.
7. Native addon build signals appear: `binding.gyp`, `node-gyp`, `npm rebuild`, `pnpm rebuild`, `yarn rebuild`, `prebuild-install`, `node-pre-gyp`,
   `python3`, `make`, `g++`, `build-base`, `build-essential`, or known native packages such as `sharp`, `canvas`, `bcrypt`, `sqlite3`,
   `better-sqlite3`, `node-rdkafka`, `grpc`, and `isolated-vm`.

Rules should gate themselves more narrowly than this broad namespace gate. Examples:

- npm rules require npm commands.
- pnpm rules require pnpm commands or pnpm lock/workspace files.
- Next.js rules require `next` or `.next` signals.
- Deno rules require `deno` signals.
- Nginx proxy rules require both Nginx and Node runtime signals in the same final image.
- Secret rules require JS-specific token names or package-manager auth files, not generic secret-looking words alone.

---

## 5. Proposed Rules

### 5.1 Summary table

| Rule ID | Severity | Default | Fix | Why it belongs |
|---|---|---:|---|---|
| `tally/js/prefer-npm-ci` | Warning | on | safe | Lockfile-backed npm Docker builds should not mutate lockfiles. |
| `tally/js/no-ad-hoc-npm-install` | Warning | on | suggestion | Direct `npm install <pkg>@<version>` does not lock the transitive dependency tree. |
| `tally/js/omit-dev-deps-in-runtime` | Warning | on | suggestion | Prevents shipping test/build tooling and larger attack surface. |
| `tally/js/bind-package-manager-inputs` | Info | on | suggestion | Modern BuildKit builds should bind-mount install inputs instead of copying lockfiles. |
| `tally/js/no-host-node-modules-copy` | Warning | on | no | Host `node_modules` frequently break Linux/arch/libc correctness. |
| `tally/js/prefer-corepack-package-manager` | Info | on | suggestion | Avoids unpinned global Yarn/pnpm installs in Node images. |
| `tally/js/node-gyp-cache-mounts` | Info | on | suggestion | Native addon builds should persist header/cache downloads and use tmpfs only for scratch. |
| `tally/js/prefer-pnpm-two-step-install` | Info | on | suggestion | pnpm uniquely supports a Docker-friendly fetch/offline install split that can run with network disabled. |
| `tally/js/pnpm-frozen-lockfile` | Warning | on | safe | pnpm Docker installs should honor the lockfile. |
| `tally/js/pnpm-fetch-for-docker-cache` | Info | on | suggestion | High-leverage pnpm-specific cache pattern, especially in CI. |
| `tally/js/pnpm-offline-install-network-none` | Warning | on | suggestion | `pnpm install --offline` should prove hermeticity with BuildKit network disabled. |
| `tally/js/pnpm-deploy-workspace-runtime` | Info | on | suggestion | Produces portable pruned runtime artifacts for pnpm workspaces. |
| `tally/js/yarn-immutable-install` | Warning | on | safe | Yarn Berry lockfile immutability is the modern Docker/CI path. |
| `tally/js/no-yarn-cache-clean` | Info | on | suggestion | `yarn cache clean` is stale layer-cleanup advice compared with BuildKit cache mounts. |
| `tally/js/no-npm-start-entrypoint` | Warning | on | suggestion | Improves signal delivery and process shape. |
| `tally/js/no-dev-server-in-runtime` | Warning | on | suggestion | Catches `next dev`, Vite dev server, `nodemon`, and similar final images. |
| `tally/js/prefer-pm2-runtime` | Info | on | suggestion | PM2's Docker integration is `pm2-runtime`, not daemonized `pm2 start`. |
| `tally/js/native-healthcheck-probe` | Info | on | suggestion | Node/Bun images can probe HTTP endpoints without adding curl/wget. |
| `tally/js/node-permission-model` | Info | on | suggestion | Node 22.13+ can enforce application-level runtime permissions. |
| `tally/js/next-use-standalone-output` | Info | on | suggestion | Next.js Docker images can be much smaller and cleaner with standalone output. |
| `tally/js/next-copy-standalone-assets` | Warning | on | safe-ish | Standalone output does not include `public` or `.next/static` by default. |
| `tally/js/next-bind-standalone-host` | Warning | on | safe | Generated `server.js` should bind `0.0.0.0` in containers. |
| `tally/js/no-public-build-env-in-runtime-image` | Warning | on | no | `NEXT_PUBLIC_*`, `VITE_*`, and `REACT_APP_*` are build-time client values. |
| `tally/js/npmrc-secret-mount` | Warning | on | suggestion | Private registry credentials should use BuildKit secrets, not copied `.npmrc` or `ARG` tokens. |
| `tally/js/no-runtime-secret-in-image` | Warning | on | no | `JWT_SECRET`, `NEXTAUTH_SECRET`, and session secrets should not be baked into images. |
| `tally/js/bun-frozen-production-install` | Warning | on | safe | Bun production Docker installs should be frozen and production-only. |
| `tally/js/bun-use-bun-user` | Info | on | safe | The official Bun image provides a `bun` user. |
| `tally/js/deno-no-allow-all` | Warning | on | suggestion | `deno run -A` discards Deno's container-relevant security model. |
| `tally/js/deno-frozen-lockfile` | Info | on | safe | Deno supports lockfile freshness checks for reproducible container starts. |
| `tally/js/no-same-container-nginx-node-proxy` | Info | on | no | Flags a narrow redundant-proxy pattern without rejecting real reverse proxies. |

### 5.2 Batch 1: should ship first

#### `tally/js/prefer-npm-ci`

**Problem**

Dockerfiles with `package-lock.json` or `npm-shrinkwrap.json` often run `npm install`, which may update lockfiles and makes builds less strictly
reproducible than `npm ci`.

**Grounding**

npm documents `npm ci` for automated environments and says it exits instead of updating the lockfile when package metadata disagree. In the balanced
corpus, **44 / 631** files mentioned `package-lock.json` and ran `npm install` without `npm ci`.

**Trigger shape**

- A stage runs `npm install` with no package spec arguments.
- The Dockerfile copies or mentions `package-lock.json` or `npm-shrinkwrap.json`.
- The command is not clearly a developer-only stage.

**Guardrails**

- Do not flag `npm install <package>` used to install a one-off global or OS helper. Other rules can cover global package installs.
- Do not flag when a comment or command explicitly says the lockfile is intentionally updated.
- Preserve flags such as `--legacy-peer-deps`, `--install-links`, workspaces flags, and registry settings.

**Fix story**

Usually `FixSafe`: replace `npm install` with `npm ci`, preserving compatible flags.

#### `tally/js/no-ad-hoc-npm-install`

**Problem**

Dockerfiles sometimes run `npm install express@4.1.1`, `npm install -g typescript@5.4.0`, or similar direct package-spec installs. This can look
pinned, but only the requested package spec is pinned; npm still resolves a transitive dependency tree unless the project lockfile owns the install.
It also splits JavaScript dependency policy between the Dockerfile and `package.json`.

**Grounding**

Hadolint `DL3016` treats direct npm package version pins as the compliant Dockerfile shape. npm's own lockfile docs make the stronger point: the
lockfile describes the exact generated dependency tree, which is the reproducibility property container builds need. The corpus found **16 / 631**
Dockerfiles with versioned direct npm install commands.

**Trigger shape**

- A `RUN npm install` or `RUN npm i` command includes package spec arguments, especially `name@version`, scoped package specs, tarball URLs, Git URLs,
  or `-g` global tool installs.
- The command is part of a production or build image rather than an intentional local-development fixture.
- The installed package is not clearly npm itself being upgraded for a known engine constraint.

**Guardrails**

- Do not flag package-manager install commands with no package specs; `tally/js/prefer-npm-ci` owns those.
- Downgrade for one-off installer CLIs where the Dockerfile also pins by digest/checksum or immediately removes the package from the final image.
- Prefer `tally/js/prefer-corepack-package-manager` for `npm install -g yarn` and `npm install -g pnpm`, but this rule can still explain the
  transitive-locking problem for other global tools.

**Fix story**

No safe automatic edit. Suggest moving the dependency into `package.json`, committing the lockfile, and installing with `npm ci` or another
lockfile-frozen package-manager command. For build-only CLIs, suggest a builder stage, Corepack-managed tool, or checksum-pinned standalone binary
when that better fits the tool.

#### `tally/js/omit-dev-deps-in-runtime`

**Problem**

Final runtime images frequently install dev dependencies. This ships test frameworks, bundlers, TypeScript compilers, linters, and native build tools
that are not needed at runtime.

**Grounding**

Docker's Node guide uses `npm ci --omit=dev`. pnpm, Yarn, and Bun all document explicit production/dependency-pruning modes. In the corpus, `npm ci`
without an explicit production omit flag appeared **129 / 201** times, and `pnpm install` without `--prod` or `--production` appeared **102 / 145**
times.

**Trigger shape**

- A final or runtime-like stage runs one of:
  - `npm ci` or `npm install`
  - `pnpm install`
  - `yarn install`
  - `yarn workspaces focus`
  - `bun install`
- The command lacks the package-manager-specific production dependency omission flag.

**Guardrails**

- Skip builder stages that run `next build`, `vite build`, `tsc`, `nest build`, native addon compilation, tests, or bundling after the install.
- Accept a same-stage `npm prune --omit=dev` or equivalent after build.
- Accept `ENV NODE_ENV=production` before npm install as a weaker compatibility signal, but still prefer explicit flags at info severity.
- Do not force production-only installs for static SPA builder stages before asset compilation.

**Fix story**

`FixSuggestion` for most commands because build stages often need dev dependencies. Safe fixes are possible only when the stage is clearly final and
does not build assets.

#### `tally/js/bind-package-manager-inputs`

**Problem**

Copying lockfiles into an image just to run `npm ci`, `pnpm install`, `yarn install`, or `bun install` is an older cache pattern. It still beats
`COPY . .` before install, but it commits install-only inputs to a layer and teaches users to copy `.npmrc` and lockfiles around instead of using
BuildKit's instruction-scoped mounts.

**Grounding**

Docker's Dockerfile reference documents `RUN --mount=type=bind` as read-only by default and explains that mount contents are available to one build
instruction without being committed to the image layer. In the corpus, **313 / 631** files copied JS package manifests or lockfiles, while only
**1 / 631** used a BuildKit bind mount in a JS-relevant Dockerfile.

**Trigger shape**

- A stage copies a JS lockfile (`package-lock.json`, `npm-shrinkwrap.json`, `pnpm-lock.yaml`, `yarn.lock`, `bun.lock`, `bun.lockb`, or `deno.lock`)
  before a package-manager install.
- Or a stage uses `COPY . ...` before the first package-manager install, forcing source changes to invalidate the dependency install step.
- For lockfile-specific findings, the copied file is not needed later in the same stage except for the install.
- For `COPY .` findings, the source tree can be copied after install or after a `pnpm fetch` step.
- The Dockerfile uses BuildKit syntax, already uses `RUN --mount`, or can reasonably add `# syntax=docker/dockerfile:1`.

**Guardrails**

- Do not flag final runtime copies of metadata that the application needs at runtime, such as `package.json` for version introspection.
- Elevate from info to warning when a copied lockfile or `.npmrc`-like install input is present in the final/runtime stage.
- Downgrade non-BuildKit Dockerfiles without a syntax directive to an adoption hint that also suggests adding `# syntax=docker/dockerfile:1`.
- In context-aware mode, only suggest bind mounts for files that are observable in the resolved build context and not excluded by `.dockerignore`.
  If the lockfile is missing or ignored, report that first instead of proposing an impossible mount.
- For `.npmrc`, delegate to `tally/js/npmrc-secret-mount` when auth tokens or private registries are involved.
- For pnpm workspaces, prefer combining this with `pnpm fetch` and `pnpm install --offline`.

**Fix story**

`FixSuggestion`: replace manifest/lockfile copies used only for install with read-only bind mounts on the install instruction. Example shape:

```dockerfile
RUN --mount=type=bind,source=package.json,target=package.json,ro \
    --mount=type=bind,source=package-lock.json,target=package-lock.json,ro \
    --mount=type=cache,target=/root/.npm,sharing=locked \
    npm ci --omit=dev
```

When `InvocationContext` provides a local build context through direct `--context`, Bake, or Compose, the fix should choose the actual observable
files: `package.json`, the active lockfile, `.npmrc` only as a secret, `pnpm-workspace.yaml`, `.yarnrc.yml`, and workspace manifests. Without context,
the rule should stay at suggestion level and avoid inventing paths.

#### `tally/js/node-gyp-cache-mounts`

**Problem**

Native Node addon builds are a major Docker rebuild cost. They pull Node headers, compile C/C++ code, and require Python plus compiler tooling. Many
Dockerfiles install `python3`, `make`, `g++`, `build-base`, or `build-essential`, but almost none cache node-gyp's header directory or separate
persistent caches from temporary scratch I/O.

**Grounding**

node-gyp documents the Unix toolchain requirements and the `--devdir` SDK/header download directory. Docker documents both persistent cache mounts
and tmpfs mounts. In the corpus, **105 / 631** files had native build toolchain signals, **25 / 631** had explicit native addon or node-gyp rebuild
signals, and **0 / 631** used a node-gyp-specific cache or tmpfs optimization.

**Trigger shape**

- A JS package-manager install/rebuild command appears in a stage that also installs or mentions `python3`, `make`, `g++`, `gcc`, `build-base`,
  `build-essential`, `node-gyp`, `prebuild-install`, or `node-pre-gyp`.
- Or context-aware package metadata exposes likely native dependencies such as `sharp`, `canvas`, `bcrypt`, `sqlite3`, `better-sqlite3`,
  `node-rdkafka`, `grpc`, or `isolated-vm`.
- The install/rebuild instruction lacks a package-manager cache mount and lacks a node-gyp header/devdir cache mount.

**Guardrails**

- Info severity by default because native packages, prebuilt binaries, libc, and package-manager behavior vary.
- Do not suggest tmpfs for `node_modules`, package `build/` directories, or any path whose compiled `.node` output must remain in the image.
- Accept both `npm_package_config_node_gyp_devdir` and `NPM_CONFIG_DEVDIR`/`npm_config_devdir`. Do not flag either form as deprecated without a
  version-specific npm/node-gyp basis.
- Skip stages that are already using language-appropriate native-build caches, ccache, or a custom prebuild artifact cache.
- In context-aware mode, use observable `package.json` and lockfile content to raise confidence and avoid warning on images that merely install
  `make` for unrelated shell tasks.

**Fix story**

`FixSuggestion`: add persistent cache mounts for the package-manager cache/store and node-gyp header directory, and optionally add tmpfs for `/tmp`
scratch. Example npm shape:

```dockerfile
RUN --mount=type=cache,id=npm,target=/root/.npm,sharing=locked \
    --mount=type=cache,id=node-gyp,target=/root/.cache/node-gyp,sharing=locked \
    --mount=type=tmpfs,target=/tmp \
    NPM_CONFIG_DEVDIR=/root/.cache/node-gyp \
    npm ci --omit=dev
```

For pnpm, combine this with the pnpm store cache and the `pnpm fetch`/offline workflow rather than replacing it.

#### `tally/js/no-npm-start-entrypoint`

**Problem**

Final `CMD ["npm", "start"]` or shell-form `npm start` adds npm as the foreground process. Node's Docker image docs recommend baking the real Node
command directly into the image so signals reach Node instead of being swallowed by npm.

**Grounding**

The Node Docker best-practices guide explicitly recommends direct `CMD ["node","index.js"]`. In the corpus, final `CMD`/`ENTRYPOINT` delegating to
`npm start` appeared **139 / 631** times.

**Trigger shape**

- Final stage `CMD` or `ENTRYPOINT` runs `npm start`, `npm run start`, `yarn start`, `pnpm start`, or `bun run start`.
- The image is a Node/Bun runtime image and not clearly a dev/test image.

**Guardrails**

- Downgrade to info when the script name is the only visible entrypoint and Tally cannot infer the underlying file.
- Skip `pm2-runtime npm -- start` and similar intentional process-manager wrappers.
- Do not flag builder-stage `RUN npm start` used for smoke tests unless it is final command.

**Fix story**

`FixSuggestion`: replace with the direct runtime command from known local signals, such as `node server.js`, `node dist/index.js`, `node dist/main`,
`node .next/standalone/server.js`, or `bun run index.ts`. Do not auto-fix when the script cannot be inferred.

#### `tally/js/no-dev-server-in-runtime`

**Problem**

Production containers sometimes run development servers: `next dev`, Vite dev server, `nest start --watch`, `nodemon`, `tsx watch`, or `npm run dev`.
These commands are slower, noisier, less hardened, and often bind unexpected ports.

**Grounding**

Official framework docs separate dev commands from build/start production commands. The balanced corpus found **25 / 631** final dev-server commands
and **3** final stages exposing Vite's dev port `5173`.

**Trigger shape**

- Final `CMD`/`ENTRYPOINT` contains dev-server commands or package scripts named `dev`, `start:dev`, `watch`, or `serve:dev`.
- Or final stage exposes known dev ports such as `5173` with Vite signals.

**Guardrails**

- Skip files named `Dockerfile.dev`, `dev.Containerfile`, or stages explicitly named `dev` unless that stage is also the only/final stage and no
  production stage exists.
- Do not flag `RUN npm run dev` in test fixtures unless it is a final command.

**Fix story**

`FixSuggestion`: use `npm run build` in a builder stage and a production command in the final stage, for example `node dist/main`, `next start`, or
`node server.js` depending on framework signals.

#### `tally/js/native-healthcheck-probe`

**Problem**

Node and Bun images frequently install or retain `curl`/`wget` only to run a Docker `HEALTHCHECK`. Modern Node and Bun runtimes can perform the same
local HTTP probe without adding another package to the runtime image. The opposite mistake also appeared: a healthcheck that only runs
`node -e "console.log('healthy')"`, which proves nothing about the server.

**Grounding**

Docker documents `HEALTHCHECK` as a way to verify that a container is still working, not merely that the process exists. Node has stable global
`fetch`, and Bun supports `bun --eval` with `fetch`. In the corpus, **59 / 631** files had `HEALTHCHECK`, **35 / 631** used `curl` or `wget`, and
**16 / 631** already used a JS runtime probe.

**Trigger shape**

- Final Node/Bun runtime image has a `HEALTHCHECK` using `curl` or `wget`.
- The Dockerfile installs `curl`/`wget` in the final image, or the base image is otherwise slim enough that avoiding those tools is valuable.
- Or a `HEALTHCHECK` runs `node -e`/`bun --eval` but does not call a socket, HTTP endpoint, readiness file, or application-specific command.

**Guardrails**

- Do not require a Dockerfile-level `HEALTHCHECK` when Compose, Kubernetes, ECS, Nomad, or Helm config likely owns probes.
- Do not flag Nginx-only static runtimes where `wget --spider http://localhost/` is the natural probe.
- Downgrade when `curl` is already required by the application for other reasons.

**Fix story**

`FixSuggestion`: replace curl/wget probes with a native runtime probe when the port and path are clear:

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:3000/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]
```

Tally already has a precedent for deterministic HTTP command-family rewrites in the `hadolint/DL4001` curl/wget normalization work. This rule
should reuse that parsing/lowering path for simple `curl`/`wget` healthchecks: preserve interval/timeout/retry options, extract URL/method/header
semantics when safe, lower to a Node/Bun probe, and then let the cleanup resolver remove `curl` or `wget` from the final stage only when no other
instruction still needs it.

#### `tally/js/node-permission-model`

**Problem**

Containers restrict the OS boundary, but they do not stop trusted JavaScript from accidentally reading secrets inside the container, writing outside
intended directories, spawning child processes, loading native addons, or opening the network. Node's Permission Model is stable now, but Dockerfile
adoption is effectively absent.

**Grounding**

Node documents `node --permission` as stable since v22.13.0/v23.5.0 and says it restricts file-system, network, child-process, worker, native addon,
WASI, and inspector access unless allowed. The corpus and targeted GitHub searches found **0** Dockerfile uses of `--permission`, `--allow-fs-read`,
`--allow-fs-write`, or `--allow-net`.

**Trigger shape**

- Final stage uses a direct `node` command, not `npm start`.
- Base image is clearly Node 22.13+, 23.5+, 24+, 25+, 26+, or an unpinned `node:lts`/`node:current` tag where the feature is likely available.
- The command and `NODE_OPTIONS` lack `--permission`, and no Node config-file permission block is visible.

**Guardrails**

- Info severity by default; warning only under a future security profile.
- Do not flag Node 20/21 or ambiguous custom images unless `node --version` is pinned elsewhere.
- Do not suggest broad `--allow-fs-read=* --allow-fs-write=*` as the target state. That preserves too much ambient authority.
- Skip CLIs or build tools that intentionally need broad filesystem access, child processes, or native addons.

**Fix story**

No safe automatic fix. Message should suggest starting with a scoped runtime profile, for example:

```dockerfile
CMD ["node", "--permission", "--allow-net", "--allow-fs-read=/app", "--allow-fs-write=/tmp", "dist/server.js"]
```

If the image intentionally keeps a package-manager wrapper, the message can suggest `NODE_OPTIONS` as a propagation path, but only with a warning
that it affects every Node process launched in the container.

#### `tally/js/next-use-standalone-output`

**Problem**

Next.js Dockerfiles that build with `next build` but ship the full source tree and full `node_modules` miss the framework's standalone output path.
This produces larger images and weaker dependency boundaries.

**Grounding**

Next.js documents `output: "standalone"` for production deployments and Docker's Next.js guide uses it for the Node-server runtime path. The corpus
found **108 / 631** Next.js-signal Dockerfiles and **64** already copying `.next/standalone`, which makes this an established good pattern rather
than speculative advice.

**Trigger shape**

- Dockerfile runs `next build` or has `.next` signals.
- Runtime stage copies all source or all `node_modules` from a builder.
- No `.next/standalone` copy appears.

**Guardrails**

- Skip static export images using `output: "export"` or copying `out/` into Nginx/static servers.
- Skip custom server deployments where the Dockerfile clearly runs a custom Express/Fastify server file and comments/config mention custom server.
- Severity should be info because enabling standalone requires `next.config.*` changes outside the Dockerfile.

**Fix story**

`FixSuggestion`: enable `output: "standalone"` in `next.config.*`, then copy `.next/standalone`, `.next/static`, and optionally `public`.

#### `tally/js/next-copy-standalone-assets`

**Problem**

Next.js standalone output does not automatically include `public` or `.next/static`. Dockerfiles that copy only `.next/standalone` can produce missing
static assets at runtime.

**Grounding**

Next.js output docs explicitly call out this caveat and show manual copies. Docker's Next.js guide copies both `public` and `.next/static`.

**Trigger shape**

- Runtime stage copies `.next/standalone`.
- No copy of `.next/static` appears.
- Optional second diagnostic if project has a visible `public` copy in builder/source but final stage does not copy `public`.

**Guardrails**

- Do not require `public` if the Dockerfile or context signals no public assets.
- Do not flag if assets are intentionally served by a CDN and the Dockerfile comments say so.

**Fix story**

Often `FixSafe`: add `COPY --from=builder /app/.next/static ./.next/static`. `public` copy is safer as `FixSuggestion` unless the source path is
obvious.

#### `tally/js/next-bind-standalone-host`

**Problem**

Next.js generated standalone `server.js` should bind `0.0.0.0` in containers. Otherwise, depending on the server path and environment, mapped ports
may not be reachable as expected.

**Grounding**

Next.js output docs and Docker's Next.js guide show `HOSTNAME=0.0.0.0` for standalone Docker runtime. Fastify documents the same general container
networking issue for apps that otherwise default to localhost. The corpus found **51 / 631** `HOSTNAME=0.0.0.0` signals in JS Dockerfiles.

**Trigger shape**

- Final command runs `node server.js` in a Next.js standalone runtime or copies `.next/standalone`.
- No same-stage `ENV HOSTNAME=0.0.0.0` or equivalent command env is present.

**Guardrails**

- Skip static export images.
- Skip orchestrator-specific comments that intentionally bind only loopback behind a same-container proxy, though that pattern may trigger the nginx
  rule separately.

**Fix story**

`FixSafe`: add `ENV HOSTNAME=0.0.0.0` before `CMD`.

#### `tally/js/no-public-build-env-in-runtime-image`

**Problem**

Dockerfiles often declare `ARG` or `ENV` values such as `NEXT_PUBLIC_API_URL`, `VITE_API_URL`, or `REACT_APP_API_URL` in a way that suggests runtime
configuration. In these frameworks, those values are built into client bundles and are visible to users.

**Grounding**

Next.js, Vite, and Create React App all document public/client env variables as build-time bundle values. The corpus found **28 / 631** Dockerfiles
with public client env prefixes in `ARG` or `ENV`.

**Trigger shape**

- Dockerfile has `ARG` or `ENV` with names matching:
  - `NEXT_PUBLIC_*`
  - `VITE_*`
  - `REACT_APP_*`
- The same file runs a corresponding production build (`next build`, `vite build`, `react-scripts build`, `npm run build`, etc.).

**Guardrails**

- Do not flag if the Dockerfile uses explicit placeholder replacement at container startup and comments document it.
- Keep severity warning when variable names look secret-bearing (`TOKEN`, `KEY`, `SECRET`, `PASSWORD`), otherwise info/warning.
- Do not suggest removing the variable blindly; the correct fix depends on whether it is public build config or true runtime config.

**Fix story**

No automatic fix. Message should explain that changing `docker run -e` after build will not change bundled client code.

#### `tally/js/npmrc-secret-mount`

**Problem**

Private registry installs often need `.npmrc`, `NPM_TOKEN`, `NODE_AUTH_TOKEN`, GitHub tokens, or scoped registry credentials. Copying `.npmrc`,
using `ARG NPM_TOKEN`, or writing auth with `RUN echo` can leave credentials in layers, image history, or provenance.

**Grounding**

Docker's build-secrets docs recommend `RUN --mount=type=secret` for sensitive build inputs, and Docker's GitHub Actions docs include a private npm
registry example that mounts `.npmrc` only for `npm ci`. The corpus found only **5 / 631** files with BuildKit secret mounts, but **20 / 631** with
`.npmrc` signals and **13 / 631** with JS-auth or runtime-secret-looking variable names.

**Trigger shape**

- Dockerfile copies `.npmrc`, writes `.npmrc`, declares `ARG NPM_TOKEN`, `ARG NODE_AUTH_TOKEN`, or uses registry auth/token variables near
  `npm`, `pnpm`, `yarn`, or `bun install`.
- No same-instruction `RUN --mount=type=secret` is used for the package-manager auth file or token.

**Guardrails**

- Do not flag `.npmrc` files that clearly contain only non-secret project config such as `legacy-peer-deps=true`, `engine-strict=true`, or registry
  URLs without credentials.
- Do not suggest runtime secrets for package install credentials. Registry credentials are build-time secrets and should be available only to the
  install instruction.
- Do not flag public registry configuration that has no auth token, `_auth`, password, or private scope indicator.

**Fix story**

`FixSuggestion`: mount `.npmrc` as a secret only for the install step:

```dockerfile
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc \
    npm ci --omit=dev
```

For pnpm/Yarn, set the package manager's userconfig path inside the same instruction if needed.

#### `tally/js/no-runtime-secret-in-image`

**Problem**

Server-side JavaScript Dockerfiles sometimes set runtime secrets in the image with `ENV JWT_SECRET=...`, `ENV NEXTAUTH_SECRET=...`,
`ENV AUTH_SECRET=...`, `ENV SESSION_SECRET=...`, or framework-specific auth secrets. That bakes deploy-environment state into a reusable image and
can expose secrets through `docker inspect`, image history, or registry access.

**Grounding**

Docker warns against passing secrets through build arguments because they can appear in history and provenance. Runtime application secrets are even
less appropriate for Dockerfiles: they should normally be injected by Compose, Kubernetes, ECS, Nomad, Docker secrets, or the deployment platform at
container start. The corpus included JS-auth/runtime-secret signals in **13 / 631** files.

**Trigger shape**

- Dockerfile `ARG` or `ENV` uses JS/web-auth secret names such as `JWT_SECRET`, `NEXTAUTH_SECRET`, `AUTH_SECRET`, `SESSION_SECRET`,
  `COOKIE_SECRET`, `CSRF_SECRET`, or `BETTER_AUTH_SECRET`.
- Or `RUN` writes those values into `.env`, `.env.production`, Next.js runtime config files, or generated server config.

**Guardrails**

- Allow known non-secret placeholders only when they are obviously fake and used in a build-only command that does not persist into the final image.
- Do not flag public client env prefixes here; `tally/js/no-public-build-env-in-runtime-image` owns those.
- Do not recommend BuildKit secret mounts for runtime-only secrets unless the build truly needs a temporary secret. The usual remediation is runtime
  injection, not build-time injection.

**Fix story**

No automatic fix. Message should distinguish build-time package credentials from runtime app secrets and suggest the deployment platform's runtime
secret mechanism.

#### `tally/js/prefer-pnpm-two-step-install`

**Problem**

npm and Yarn can be made reproducible with lockfiles and frozen installs, but they do not have pnpm's Docker-oriented split between dependency
fetching and dependency linking. pnpm can fetch packages from `pnpm-lock.yaml` and config before application source is copied, then link/install
offline after source is present. That second step can run with `RUN --network=none`, making unexpected registry or lifecycle-script network access
fail during the image build.

**Grounding**

pnpm documents `pnpm fetch` as specifically designed for Docker image builds and says it works from lockfile/config input, including monorepos,
before `pnpm install --offline`. Docker documents `RUN --network=none` for build steps that should consume only already-provided artifacts. The
corpus found broad npm/Yarn usage, only **7 / 631** `pnpm fetch` examples, and **0 / 631** `pnpm install --offline` examples with
`RUN --network=none`.

**Trigger shape**

- A Node.js Dockerfile uses npm or Yarn for dependency installation in a production or CI-like build.
- The file already has BuildKit signals, multi-stage install structure, cache-mount advice, or other evidence that dependency caching/security is a
  goal.
- The project is not clearly blocked from pnpm migration by Yarn PnP/Zero-Install, package-manager-specific deployment tooling, or explicit comments.

**Guardrails**

- Info severity by default; changing package managers is not a safe local Dockerfile edit.
- Do not flag Bun or Deno projects, even if they are JavaScript/TypeScript projects.
- Do not flag Yarn PnP/Zero-Install projects or npm/Yarn projects with known tooling constraints that pnpm migration may not preserve.
- If `package.json` pins `npm` or `yarn` in `packageManager`, keep the finding as a migration note rather than a direct Dockerfile fix.
- Do not replace `tally/js/prefer-npm-ci` or `tally/js/yarn-immutable-install`; those remain correctness rules for projects that intentionally stay
  on npm or Yarn.
- In context-aware mode, raise confidence when no `packageManager` is pinned, when no lockfile exists yet, when no package-manager-specific feature is
  visible, or when the Dockerfile is already adopting Corepack and BuildKit cache mounts.

**Fix story**

No automatic fix. Suggest a migration shape rather than editing the Dockerfile blindly:

```dockerfile
RUN corepack enable
RUN --mount=type=bind,source=pnpm-lock.yaml,target=pnpm-lock.yaml,ro \
    --mount=type=bind,source=pnpm-workspace.yaml,target=pnpm-workspace.yaml,ro \
    --mount=type=cache,id=pnpm,target=/pnpm/store,sharing=locked \
    pnpm fetch --prod

COPY . .
RUN --network=none \
    --mount=type=cache,id=pnpm,target=/pnpm/store,sharing=locked \
    pnpm install --offline --prod --frozen-lockfile
```

If the project has no `pnpm-lock.yaml`, the message should explicitly say migration requires regenerating and reviewing the lockfile outside the
Dockerfile change.

#### `tally/js/pnpm-frozen-lockfile`

**Problem**

`pnpm install` in Docker without `--frozen-lockfile` can update resolution state instead of enforcing the lockfile.

**Grounding**

pnpm's Docker docs consistently use `--frozen-lockfile`. In the corpus, **50 / 145** `pnpm install` Dockerfiles lacked it.

**Trigger shape**

- A `RUN pnpm install` command appears.
- No `--frozen-lockfile` flag and no explicit `--lockfile-only` or update workflow signal.

**Guardrails**

- Skip commands in dev-only stages if the Dockerfile is clearly for local development.
- Skip `pnpm install --fix-lockfile` or lockfile-maintenance images.

**Fix story**

`FixSafe`: append `--frozen-lockfile`.

#### `tally/js/pnpm-fetch-for-docker-cache`

**Problem**

pnpm monorepos and CI builds often copy many `package.json` files before install or copy the full source before install. `pnpm fetch` can populate the
store from a bind-mounted lockfile first, preserving Docker layer cache until dependencies actually change.

**Grounding**

pnpm documents `pnpm fetch` specifically for Docker image builds and says it works for simple and monorepo projects. The corpus found only **7 / 631**
files using `pnpm fetch`, which means this is a high-value teaching rule.

**Trigger shape**

- Dockerfile has `pnpm-lock.yaml` and `pnpm install`.
- It also has `pnpm-workspace.yaml`, multiple workspace manifest copies, or `COPY .` before install.
- No `pnpm fetch` appears.

**Guardrails**

- Info severity only.
- Do not flag small single-package apps that already use bind-mounted install inputs and BuildKit cache mounts.
- Do not flag when local `file:` dependencies are clearly required before fetch, because pnpm docs note those can be skipped during fetch.

**Fix story**

`FixSuggestion`: bind-mount `pnpm-lock.yaml` and `pnpm-workspace.yaml`, run `pnpm fetch --prod` or full `pnpm fetch`, copy source, then run
`RUN --network=none pnpm install --offline --prod --frozen-lockfile`.

#### `tally/js/pnpm-offline-install-network-none`

**Problem**

`pnpm install --offline` after `pnpm fetch` is a strong pattern, but leaving default build networking enabled misses the main hermetic-build benefit.
Lifecycle scripts, postinstall hooks, and helper tools can still call the network even though dependency tarballs came from the pnpm store.

**Grounding**

Docker's Dockerfile reference documents `RUN --network=none` for build steps that should consume only already-provided artifacts. The corpus found
**5 / 631** `pnpm install --offline` files and **0 / 631** combining offline pnpm installs with `RUN --network=none`. A targeted GitHub code search
for that combination also returned no examples, which suggests the pattern is high value and under-adopted.

**Trigger shape**

- A Dockerfile runs `pnpm fetch` before `pnpm install`.
- A later `RUN` uses `pnpm install --offline`.
- That `RUN` instruction lacks `--network=none`.

**Guardrails**

- Skip if a comment explicitly says a lifecycle script must reach an internal network service.
- Skip if the command is in a dev/test stage and not part of a production image build.
- Do not apply this rule to npm, Yarn, Bun, or Deno; it is specifically about pnpm's first-class `fetch` plus offline workflow.

**Fix story**

`FixSuggestion`: add `--network=none` to the `RUN` instruction, preserving existing `--mount` options:

```dockerfile
RUN --network=none \
    --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --offline --frozen-lockfile --prod
```

#### `tally/js/pnpm-deploy-workspace-runtime`

**Problem**

pnpm workspace images often copy the whole monorepo, root `node_modules`, or every workspace into a runtime image. `pnpm deploy --filter ... --prod`
can produce a portable runtime directory containing only the selected package and its dependencies.

**Grounding**

pnpm's `deploy` docs include a Docker image example. The corpus found **18 / 631** files already using `pnpm deploy`, including real application
images, which makes this practical.

**Trigger shape**

- Dockerfile has `pnpm-workspace.yaml`, `pnpm install`, and a final runtime stage.
- Runtime stage copies root `/app` or root `node_modules` rather than a filtered/pruned workspace artifact.
- No `pnpm deploy` or equivalent prune command appears.

**Guardrails**

- Info severity only.
- Skip single-service monorepos where the final runtime intentionally contains multiple workspace apps.
- Skip projects using Turbo/Nx/monorepo deploy tooling that already prunes the output (`turbo prune`, `nx deploy`, `yarn workspaces focus`, etc.).

**Fix story**

`FixSuggestion`: add a pruned stage with `pnpm --filter <app> --prod deploy <dir>` and copy that directory into the final stage.

### 5.3 Batch 2: strong follow-ups

#### `tally/js/yarn-immutable-install`

Warn when a Yarn Berry project runs `yarn install` in Docker without `--immutable` or `--frozen-lockfile`.

Trigger on `yarn.lock` plus either `.yarnrc.yml`, `packageManager: yarn@2+` when package metadata facts are available, or `corepack enable yarn`.
Use `FixSafe` to append `--immutable` for Yarn 2+ and `--frozen-lockfile` for Yarn classic.

#### `tally/js/no-yarn-cache-clean`

Warn when a Dockerfile runs `yarn cache clean` as layer-cleanup advice after `yarn install`.

This is mostly a compatibility and education rule. Hadolint `DL3060` recommends `yarn cache clean`, but BuildKit cache mounts are the better Docker
primitive: the cache can stay outside the image layer and remain useful across builds. The balanced corpus had **0 / 631** `yarn cache clean`
examples, so this should not be prioritized for frequency. It is valuable because it gives Tally a precise supersession target for stale Hadolint
guidance.

Fix story: suggest removing `yarn cache clean` and moving Yarn's cache to a BuildKit cache mount on the install instruction. Do not auto-remove it if
the same stage lacks any cache-mount replacement or the command is part of a deliberate final-size experiment.

#### `tally/js/prefer-corepack-package-manager`

Warn when a Node-image Dockerfile globally installs `yarn` or `pnpm` with npm instead of using a pinned package-manager path.

The rule should be conservative because Corepack distribution changed around modern Node releases. Recommended behavior:

- Prefer `packageManager` plus `corepack enable` when the base image is a Node version that includes Corepack or the Dockerfile already installs
  Corepack.
- Warn on unversioned `npm install -g yarn` or `npm install -g pnpm`.
- Downgrade or skip on Node versions where Corepack is not available unless the Dockerfile installs it.

#### `tally/js/no-host-node-modules-copy`

Warn when Dockerfile copies `node_modules` from the build context rather than from another stage.

This catches OS/architecture/libc mismatches, leaked dev dependencies, and huge build contexts. It should not flag
`COPY --from=deps /app/node_modules` because that is a valid pattern used by Node, Bun, pnpm, and Next.js examples.

#### `tally/js/prefer-pm2-runtime`

Warn when the final image runs `pm2 start`, `pm2 start ecosystem.config.js`, or a daemonized PM2 command. PM2's Docker integration is
`pm2-runtime`.

Guardrails:

- Do not ban `pm2-runtime`.
- Do not flag docs/examples that are not final runtime stages.
- If no PM2-specific feature is visible, the message can suggest a direct Node command plus container restart policy instead.

#### `tally/js/bun-frozen-production-install`

Warn when a Bun production/runtime stage runs `bun install` without `--frozen-lockfile`, and warn when the runtime dependency install lacks
`--production` or equivalent omission flags.

Grounding: Bun's Docker guide uses separate dev and production installs with `bun install --frozen-lockfile --production`.

#### `tally/js/bun-use-bun-user`

Info-level rule for final `oven/bun` images that do not switch to `USER bun`.

This is intentionally not a generic non-root rule. It fires only when the official image-provided runtime user is available and the remediation is
obvious.

#### `tally/js/deno-no-allow-all`

Warn when a final `deno run` command uses `-A` or `--allow-all`.

Deno's docs say all permissions are not recommended and should only be used for testing. Prefer scoped flags such as `--allow-net=api.example.com` or
`--allow-read=/data`.

#### `tally/js/deno-frozen-lockfile`

Info-level rule when a Deno Dockerfile has `deno.lock` or `deno.json` but final `deno run`/`deno task` does not use `--frozen`, `--lock`, or a
pre-cached dependency stage.

This should stay info because Deno lockfile workflows vary and projects may intentionally resolve at build time.

#### `tally/js/no-same-container-nginx-node-proxy`

Info-level rule for a narrow redundant proxy shape:

- final image installs or runs both Nginx and Node;
- Nginx config is copied into the same image;
- config or command proxies to `localhost`, `127.0.0.1`, or a same-container Node port;
- no strong signal exists for static asset serving, caching, compression, TLS, rate limiting, payload limits, or deliberate multi-process supervision.

This rule should avoid false positives:

- Do not flag `FROM nginx` final stages serving `dist`, `build`, `out`, or static Next export output.
- Do not flag separate Compose/Kubernetes/ingress proxy services.
- Do not flag Nginx configs that clearly implement edge concerns beyond local pass-through.

The message should be nuanced: a reverse proxy is still recommended at the production edge, but it often belongs outside the Node application image.

### 5.4 Source-aware or post-v1 candidates

These are valuable but need either package metadata facts or source-code facts beyond a single Dockerfile:

| Candidate | Why deferred |
|---|---|
| `tally/js/node-lts-runtime` | Needs up-to-date Node release data and tag resolution. Production should use Active/Maintenance LTS, but this should probably be resolver-backed. |
| `tally/js/node-alpine-native-module-risk` | Needs `package.json` dependency inspection for `sharp`, Prisma, `bcrypt`, `canvas`, `sqlite3`, Playwright, Cypress, etc. |
| `tally/js/fastify-listen-all-interfaces` | Best detected in application source or package scripts, not Dockerfile alone. |
| `tally/js/next-sharp-on-alpine-memory` | Needs Next image-optimization usage plus base-image/libc/package metadata. |
| `tally/js/next-cache-mount-runtime-cache` | Docker's guide notes `.next/cache` cache mounts can omit fetch cache from final image. Useful, but likely too subtle for v1 default. |
| `tally/js/prisma-generate-in-runtime` | Needs Prisma schema/package awareness and should probably live under a future `tally/js/prisma/*` or `tally/prisma/*` namespace. |
| `tally/js/playwright-browsers-in-runtime` | Needs Playwright package and browser install facts; high value but separate enough for dedicated research. |

---

## 6. Implementation Notes

### 6.1 Shared facts

Add a small JavaScript facts layer rather than making each rule re-parse shell strings independently:

- package manager commands:
  - manager: npm, pnpm, yarn, bun, deno
  - subcommand: install, ci, fetch, deploy, prune, run, task
  - flags: frozen, production, omit dev, offline, workspace filter, network none
- copied or mounted manifests and lockfiles:
  - `package.json`, npm lockfiles, pnpm lock/workspace, Yarn lock/config, Bun lock, Deno config/lock
  - whether the file is copied into a layer, bind-mounted for one `RUN`, or secret-mounted
  - package-manager commitment level: `packageManager` field, committed lockfile type, Yarn PnP/Zero-Install signals, Corepack usage, and whether
    migration to pnpm would require creating a new lockfile
- framework signals:
  - Next, Vite, CRA, Nest, Fastify, Express, Nuxt, Astro, SvelteKit
- runtime command facts:
  - final `CMD`/`ENTRYPOINT`
  - package-manager wrapper command
  - direct Node/Bun/Deno command
  - dev-server-like command
  - Node Permission Model flags and Node version tag eligibility
  - `HEALTHCHECK` command shape and whether it probes the app or only the runtime
- secret facts:
  - `.npmrc` copies/writes and private registry token names
  - runtime app secret names such as `JWT_SECRET`, `NEXTAUTH_SECRET`, `AUTH_SECRET`, and `SESSION_SECRET`
- stage role heuristics:
  - builder, deps, test, dev, runner, release, production
  - final exported stage

Use the existing facts guidance from the repository instructions: build facts once and expose them to rules through `input.Facts` rather than
duplicating parsing logic in each rule.

### 6.2 Context-aware rule mode

The initial rules should work from Dockerfile-only evidence, but the implementation should become sharper when `input.InvocationContext` and
`facts.FileFacts` include observable build context. The current codebase already normalizes direct Dockerfile linting with `--context`, Docker Bake,
and Compose into `BuildInvocation` data, and the facts layer can expose files that are copied, added, bind-mounted, heredoc-created, or available in
the local context through `BuildContextSources` and `ObservableFile` records.

Use context to improve fixes and confidence:

- Verify that a proposed bind-mounted lockfile or manifest exists and is not ignored before suggesting a concrete `RUN --mount=type=bind` edit.
- Read `package.json` for `packageManager`, `scripts`, `workspaces`, runtime entrypoints, and dependencies that imply native addons.
- For `tally/js/prefer-pnpm-two-step-install`, distinguish "new project or no pinned package manager" from "existing npm/Yarn policy"; the former can
  get a stronger recommendation, while the latter should be a migration note.
- Read lockfiles only through structured or package-manager-aware helpers where practical; avoid brittle string matching for dependency metadata when
  a parser is available.
- Treat missing observable lockfiles as a stronger diagnostic than a bind-mount rewrite. If `package-lock.json` is not in the context, `npm ci` cannot
  be the immediate fix.
- Use Bake/Compose healthcheck, secret, target-stage, and build-context metadata to suppress Dockerfile-only suggestions that are already handled by
  the orchestrator entrypoint.
- In remote, Git, tar, or unavailable named-context modes, fall back to conservative Dockerfile-only findings and avoid path-specific fixes.

### 6.3 Severity policy

- Use **warning** for correctness/security/runtime behavior issues: lockfile mutation, dev dependencies in final images, Deno `--allow-all`, missing
  Next standalone assets, dev servers in runtime, missing `--network=none` on pnpm offline installs, copied registry credentials, baked runtime
  secrets, and public build env values that look secret-bearing.
- Use **info** for performance/education rules: bind-mounted install inputs, pnpm preference/migration, pnpm fetch, pnpm deploy, native JS health
  probes, Node Permission Model, Next standalone preference, node-gyp cache mounts, Corepack preference, same-container nginx proxy, and Bun user.
- Use **suggestion** fixes for structural Dockerfile rewrites.
- Use **safe** fixes only when the edit is local and unlikely to alter build semantics: appending `--frozen-lockfile`, adding `HOSTNAME=0.0.0.0`,
  replacing `npm install` with `npm ci` under strong lockfile guardrails, or adding `.next/static` copy when the builder path is obvious.

### 6.4 Namespace boundaries

Do not duplicate these existing or generic rule families:

- generic non-root runtime rules, except for runtime-specific users like `USER bun`;
- generic package manager cache-mount advice, except for pnpm's Docker-specific `fetch`/offline workflow, migration toward that workflow, JS lockfile
  bind mounts, stale Yarn cache-clean advice, and node-gyp-specific cache paths;
- generic BuildKit multi-stage advice;
- generic secret-in-ARG/ENV rules, except for JS public client env prefixes, package-manager auth files, and common JS runtime auth secret names;
- generic reverse-proxy advocacy.

### 6.5 Hadolint compatibility and deprecations

The repository already has a deprecation mechanism in `internal/ruledeprecation` for rules that are dead ends or superseded by BuildKit/native Tally
coverage. Use that mechanism for stale JavaScript-era Hadolint guidance:

- Add `hadolint/DL3016` as `KindSuperseded` when `tally/js/no-ad-hoc-npm-install` ships. The replacement should explain that direct npm package
  pins do not lock transitive dependencies and that project installs should be lockfile-driven.
- Keep `hadolint/DL3060` not planned and add or confirm a supersession path to `tally/prefer-package-cache-mounts`; `tally/js/no-yarn-cache-clean`
  can provide a JS-specific diagnostic when that exact command appears.
- Do not add a `DL3056` npm deprecation. Current upstream `DL3056` is about label semantic-version syntax. If the local Hadolint reference still
  says "Pin npm global versions", that reference should be corrected separately.

---

## 7. Recommended Initial Batch

Ship these first:

1. `tally/js/prefer-npm-ci`
2. `tally/js/no-ad-hoc-npm-install`
3. `tally/js/omit-dev-deps-in-runtime`
4. `tally/js/bind-package-manager-inputs`
5. `tally/js/no-npm-start-entrypoint`
6. `tally/js/no-dev-server-in-runtime`
7. `tally/js/prefer-pnpm-two-step-install`
8. `tally/js/pnpm-frozen-lockfile`
9. `tally/js/pnpm-fetch-for-docker-cache`
10. `tally/js/pnpm-offline-install-network-none`
11. `tally/js/npmrc-secret-mount`
12. `tally/js/no-runtime-secret-in-image`
13. `tally/js/node-gyp-cache-mounts`
14. `tally/js/next-use-standalone-output`
15. `tally/js/next-copy-standalone-assets`
16. `tally/js/next-bind-standalone-host`
17. `tally/js/no-public-build-env-in-runtime-image`
18. `tally/js/deno-no-allow-all`

Then add the runtime/package-manager follow-ups:

19. `tally/js/native-healthcheck-probe`
20. `tally/js/node-permission-model`
21. `tally/js/yarn-immutable-install`
22. `tally/js/no-yarn-cache-clean`
23. `tally/js/prefer-corepack-package-manager`
24. `tally/js/pnpm-deploy-workspace-runtime`
25. `tally/js/prefer-pm2-runtime`
26. `tally/js/bun-frozen-production-install`
27. `tally/js/bun-use-bun-user`
28. `tally/js/deno-frozen-lockfile`
29. `tally/js/no-host-node-modules-copy`
30. `tally/js/no-same-container-nginx-node-proxy`

This ordering maximizes early value from patterns that were frequent in the corpus or clearly under-adopted despite official support. It deliberately
lets `tally/js/*` teach newer BuildKit, pnpm two-step install, secret-mount, and Node permission patterns rather than mirroring current community lag.
