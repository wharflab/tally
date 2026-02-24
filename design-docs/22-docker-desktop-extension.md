# Docker Desktop Extension: Tally as an in-product Dockerfile Lint + Fix Marketing Channel

> Status: proposal

## 0. Executive summary

Docker Desktop Extensions are a high-leverage distribution channel: they surface functionality *inside* Docker Desktop, install with one click, and
can be published via Docker Hub + the Extensions Marketplace.

This document proposes a **Tally Docker Desktop Extension** that:

- Runs **tally** against local Dockerfiles/Containerfiles directly on the user’s machine (fast, zero setup).
- Provides a **lint → explain → fix preview → apply** workflow that feels native to Docker Desktop.
- Onboards users into **project-level adoption** by generating `.tally.toml` and CI snippets.
- Doubles as a **packaging vector** for the `tally` CLI (host-installed binary shipped with the extension).

The recommended MVP architecture is **UI + host binaries** (no backend) to minimize complexity and avoid host-filesystem access challenges from the
VM. A backend can be added later for long-running jobs, caching, and “quality trend” features.

## 1. Goals / non-goals

### 1.1 Goals

**Acquisition + awareness**

- Get Tally into the Docker Desktop Marketplace (“Not reviewed” initially) and later apply for “Reviewed”.
- Convert Docker Desktop users into `tally` CLI users.

**Adoption + retention**

- Make it trivial to run Tally on a Dockerfile without installing anything else.
- Create a natural path to commit `.tally.toml` and add CI checks.

**Product clarity**

- Teach users *why* a rule matters and what “good” looks like, not just fail builds.

### 1.2 Non-goals (MVP)

- Replace IDE integrations (we already have VS Code / IntelliJ plans).
- Implement enterprise policy management / remote dashboards.
- Require user accounts, sign-in, or paid SaaS.
- Run linting inside a container that needs special mounts to the host filesystem.

## 2. Why Docker Desktop Extensions

Docker Desktop Extensions:

- Are packaged as **Docker images** with a required `metadata.json` at the image root.
- Can contain a **UI** shown in Docker Desktop, optional **backend services** running in the Desktop VM, and optional
  **executables installed on the host**. (See Docker Extensions SDK architecture docs.)
- Are distributed via **Docker Hub**, can be listed in the **Extensions Marketplace**, and are versioned via **semver tags**.

This maps well to Tally’s goals:

- Tally is already a CLI-first product.
- A Docker Desktop Extension can offer a “UI wrapper” around the CLI, lowering friction.
- The extension becomes a stable, always-visible marketing surface: onboarding, rule education, “what’s new” changelog, links to docs.

## 3. SDK capabilities & constraints that shape design

### 3.1 UI lifecycle & sandbox

- The extension UI is sandboxed (no Node/Electron APIs) and is **terminated when the user navigates away** from the extension tab.
- Implication: do not rely on UI processes for long-running operations.

### 3.2 Docker API access

- The UI can run Docker operations and execute `docker` commands via the extension client.
- It can also stream command output (useful for progress UIs or docker events), but cannot shell-chain/pipeline commands in a single invocation.

### 3.3 Host binaries

- Extensions can ship binaries/scripts that Docker Desktop copies onto the host and can be invoked from the extension UI.
- This is the critical capability for Tally because linting/fixing local files is simplest (and fastest) on the host where the repo lives.

### 3.4 Backend services (optional)

- Backend services run inside the Desktop VM and can be called from the UI.
- Backend is great for persistence and long-running jobs, but it has friction when needing access to host filesystem paths.

## 4. Proposed product: “Tally in Docker Desktop”

### 4.1 MVP feature set (launchable)

1. **Lint a file**
   - Select a Dockerfile/Containerfile (or paste a path).
   - Run `tally lint --format json <file>`.
   - Render results grouped by severity / rule namespace.

2. **Explain & learn**
   - Show rule description + “why it matters” + suggested fix.
   - Link out to `RULES.md` / docs using Docker Desktop’s `openExternal`.

3. **Fix preview & apply**
   - “Preview fix” runs `tally lint --fix` against a *temporary copy* of the file and shows a unified diff.
   - “Apply fix” writes changes back to the selected file.
   - Support “safe fixes” only for MVP; add `--fix-unsafe` behind an explicit toggle later.

4. **Project adoption CTA**
   - “Add Tally to this repo” wizard:
     - Write `.tally.toml` with sane defaults.
     - Provide copy/paste CI snippets (GitHub Actions, GitLab, etc.)

5. **Run on folder/glob**
   - Run `tally lint .` or `tally lint "**/*.Dockerfile"` (using the host binary) and show summary + top offenders.

### 4.2 V1 enhancements (post-launch)

- **“Build-time coach” nudge**: listen to docker events (stream) and show a non-intrusive toast after successful builds suggesting “Run Tally”.
- **Rule Explorer**: searchable catalog of rules (local, offline), with “bad vs good” examples.
- **“Export SARIF”**: run `tally lint --format sarif` and write to a user-chosen path.
- **Context-aware runs**: support `--context <dir>` for rules that need it.

### 4.3 V2 (optional, if it proves sticky)

- **Trends / history per repo**: store last N runs and show “warnings down” graphs.
- **Policy packs**: baseline/strict/security presets.
- **Cross-file insights**: aggregate common violations and show “top fixes to reduce risk”.

## 5. UX flows

### 5.1 First run (onboarding)

- Landing page:
  - One primary CTA: **“Lint a Dockerfile”**.
  - Secondary CTA: **“Set up Tally in a repo”**.
  - “What is Tally?” explainer with links.

### 5.2 Lint flow

1. Choose file/folder.
2. Optionally choose:
   - config discovery mode (default: auto)
   - output fail-level (default: none inside the UI; don’t “fail” the UI)
3. Run and show results.
4. Clicking a violation reveals:
   - message
   - rule code
   - suggested fix (if available)
   - link to docs

### 5.3 Fix flow

- Preview fixes:
  - show a diff (before/after) and summary (“3 safe fixes”).
- Apply fixes:
  - write to file; show success toast.

### 5.4 Adoption flow

- “Generate `.tally.toml`”
- “Add CI” (copy/paste snippets)
- “Run on PR” recommendation

## 6. Technical architecture

### 6.1 Components

**UI (required)**

- React UI generated via `docker extension init` (or a lightweight custom UI).
- Uses `@docker/extension-api-client` to:
  - invoke host binaries
  - show toast notifications
  - open external docs

**Host binaries (required for MVP)**

- Ship:
  - `tally` (the actual CLI)
  - `tally-ext` (a small wrapper purpose-built for the extension)

`tally-ext` responsibilities:

- Normalize platform differences.
- Execute `tally` with a stable contract.
- Implement **fix preview** safely using temporary copies.
- Produce a compact JSON envelope that the UI can render without coupling to every `tally` reporter detail.

**Backend (optional, not MVP)**

- Only add if we implement persistence/trends, long-running background work, or an in-extension editor experience.
- Note: running a full LSP server is feasible in a backend container, but the Docker Extensions UI ↔ backend communication surface is primarily HTTP
  request/response (via `ddClient.extension.vm.service.*`). LSP’s standard transports (stdio/TCP) are full-duplex, so you’d typically need an explicit
  proxy (e.g., JSON-RPC over HTTP long-polling or a WS bridge) rather than “just run `tally lsp --stdio` and connect to it” from the UI.

### 6.2 Contracts between UI and host

Recommended commands:

- `tally-ext lint --path <file|dir|glob> [--config <path>] [--context <dir>]`
  - Runs `tally lint --format json ...`.
  - Returns `{ summary, violations[], stats }` JSON.

- `tally-ext fix-preview --path <file> [--unsafe]`
  - Copies file to temp, runs `tally lint --fix` (and optional `--fix-unsafe`) on temp, diffs vs original.
  - Returns `{ changed: bool, diff: string, fixCount: number }`.

- `tally-ext fix-apply --path <file> [--unsafe]`
  - Runs on the real file.

### 6.3 Why a wrapper instead of calling `tally` directly from UI

Calling `tally` directly is viable, but a wrapper provides:

- A stable JSON schema for the UI (we can evolve `tally` output formats independently).
- Centralized file-copy + diff logic.
- Better error messages for common OS/permissions problems.

### 6.4 Packaging layout (extension image)

Example conceptual layout:

- `/metadata.json`
- `/ui/` (static web build)
- `/binaries/` (per-platform binaries)

`metadata.json` should declare the UI root + host binaries.

## 7. Distribution & Marketplace plan

### 7.1 Docker Hub repository

- Publish as a Docker image (e.g. `wharflab/tally-docker-desktop-extension:<version>`).
- Tags must follow **semver**.
- Build as a **multi-arch image** (ARM/AMD). Host binaries must include Windows builds as well.

### 7.2 Required extension image labels

Docker requires OCI + extension labels (see Docker docs). At minimum:

- `org.opencontainers.image.title`
- `org.opencontainers.image.description`
- `org.opencontainers.image.vendor`
- `com.docker.desktop.extension.api.version`
- `com.docker.desktop.extension.icon`
- `com.docker.extension.screenshots`
- `com.docker.extension.detailed-description`
- `com.docker.extension.publisher-url`
- `com.docker.extension.changelog`

Recommended:

- `com.docker.extension.additional-urls` (docs/support/privacy)
- `com.docker.extension.categories` (likely: `security,utility-tools,ci-cd`)

### 7.3 Marketplace strategy

- Start as **self-published** (automated validation) to iterate quickly.
- After stability + adoption, apply for **Docker-reviewed** status.

## 8. Security, privacy, trust

### 8.1 Host binary safety

- Host binaries run with user permissions.
- The extension must clearly state:
  - it reads/writes the selected file(s) only
  - it never uploads Dockerfiles by default

### 8.2 Telemetry

- Default: **no telemetry**.
- If we add optional telemetry later:
  - explicit opt-in toggle in settings
  - publish a privacy policy link via extension labels
  - record only coarse events (e.g., “lint run”, “fix applied”) without paths/content

### 8.3 Supply chain

- Consider signing extension images (cosign) once published.
- Publish checksums for host binaries (even though they’re shipped inside the image).

## 9. Testing & release engineering

- UI unit tests (React) + minimal e2e smoke tests using Docker Desktop extension development workflow.
- Release pipeline (suggested):
  1. Build `tally` + `tally-ext` for all supported targets.
  2. Build extension image with binaries + UI assets.
  3. `docker buildx build --push --platform ...` to Docker Hub.
  4. Update screenshots/changelog labels per release.

## 10. Rollout phases

- **Phase 0 (internal)**: local install, dogfood across macOS/Windows.
- **Phase 1 (public self-published)**: MVP shipped; gather feedback.
- **Phase 2 (quality + polish)**: rule explorer, SARIF export, improved onboarding.
- **Phase 3 (reviewed)**: apply for Docker-reviewed listing.

## 11. Success metrics

Acquisition

- Marketplace installs and active users.
- Click-through to GitHub/docs.

Adoption

- “Generate `.tally.toml`” completion rate.
- “Copy CI snippet” interactions.

Retention

- Repeat runs per week.
- Fix-apply usage.

## 12. Open questions

- File picker API: Docker docs mention `showOpenDialog` but it is marked deprecated; confirm current replacement strategy in the SDK.
- Best UX for selecting repo + Dockerfile without native file picker.
- How tightly the UI should couple to existing `tally lint --format json` schema vs wrapper-owned schema.

## 13. Alternatives considered

1. **Backend-only (VM) linting**
   - Pros: easier persistence, long-running jobs.
   - Cons: host filesystem access and path mapping complexity.

2. **Call `tally` directly from UI, no wrapper**
   - Pros: simplest.
   - Cons: diff preview/apply logic and stable UI contract become harder.

3. **Ship only educational content (no host binary)**
   - Pros: trivial to publish.
   - Cons: weak value proposition; unlikely to drive adoption.

4. **Run `tally` as an LSP server in the backend and lint via LSP**
   - Pros:
     - Reuses tally’s existing LSP server surface (diagnostics, code actions, etc.).
     - Enables a richer “in-extension editor” (e.g., Monaco) with incremental diagnostics.
   - Cons / feasibility constraints:
     - Stdio-only LSP is fine *if* the client can open a full-duplex stdin/stdout pipe. Today, the Extensions SDK exec APIs expose stdout/stderr
       streaming but `ExecProcess` only exposes `close()` (no writable stdin/write API; see docker/extensions-sdk#205), so you can’t just run
       `tally lsp --stdio` and talk to it directly from the UI.
     - UI ↔ backend is primarily HTTP request/response; making LSP work generally requires a proxy/bridge:
       - Backend spawns `tally lsp --stdio` and speaks stdio to it.
       - UI speaks a custom transport to the backend (e.g., JSON-RPC messages over HTTP long-polling).
     - The existing VS Code extension logic is tightly coupled to VS Code APIs; what’s reusable is mainly the LSP *server* (tally) and protocol types,
       not the client plumbing.
     - Unless we embed an editor in the extension, LSP adds complexity without much UX benefit over `tally lint --format json`.

## 14. References

- Docker Extensions SDK: architecture and components (UI/backend/host binaries)
  - <https://docs.docker.com/extensions/extensions-sdk/architecture/>
- Extension packaging/distribution (Docker Hub, semver tags, multi-arch)
  - <https://docs.docker.com/extensions/extensions-sdk/extensions/DISTRIBUTION/>
- Extension `metadata.json` structure
  - <https://docs.docker.com/extensions/extensions-sdk/architecture/metadata/>
- UI API overview and client entrypoint
  - <https://docs.docker.com/extensions/extensions-sdk/dev/api/overview/>
- Docker API + exec from extension
  - <https://docs.docker.com/extensions/extensions-sdk/dev/api/docker/>
- Toasts + openExternal
  - <https://docs.docker.com/extensions/extensions-sdk/dev/api/dashboard/>
- Navigation intents (jump to container logs, etc.)
  - <https://docs.docker.com/extensions/extensions-sdk/dev/api/dashboard-routes-navigation/>
- Host binaries guide
  - <https://docs.docker.com/extensions/extensions-sdk/guides/invoke-host-binaries/>
- Extensions SDK exec API reference
  - <https://docs.docker.com/reference/api/extensions-sdk/Exec/>
- ExecProcess API reference
  - <https://docs.docker.com/reference/api/extensions-sdk/ExecProcess/>
- Stdin support feature request (ExecProcess)
  - <https://github.com/docker/extensions-sdk/issues/205>
- Required extension labels
  - <https://docs.docker.com/extensions/extensions-sdk/extensions/labels/>
- Marketplace overview (reviewed vs self-published)
  - <https://docs.docker.com/extensions/marketplace/>
