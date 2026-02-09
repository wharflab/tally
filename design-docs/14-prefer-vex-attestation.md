# Prefer VEX Attestation (OpenVEX)

**Design Focus:** Add a tally rule that discourages embedding OpenVEX VEX documents into images via `COPY` and recommends attaching VEX as an OCI
attestation instead.

---

## 1. Problem Statement

VEX (Vulnerability Exploitability eXchange) documents are **supply-chain metadata**: they describe whether a vulnerability applies to a product and
why. When VEX is shipped **inside** a container image (e.g., `COPY *.vex.json /...`), it becomes runtime payload:

- Increases image contents and distribution surface
- Requires rebuilding the image to update a statement
- Is harder for tooling to discover consistently (vs. OCI referrers / attestations)
- Blurs the boundary between “what runs” and “what describes the artifact”

OCI attestations (in-toto predicate documents attached to an image digest) are a better fit for VEX: they can be attached post-build, signed, and
consumed by security tooling without changing the runtime filesystem.

---

## 2. Decision

Introduce a new tally rule:

- **Rule ID:** `tally/prefer-vex-attestation`
- **MVP behavior:** Detect `COPY` of `*.vex.json` (or any `*.vex.json`-suffixed source) and emit a violation recommending OCI attestation-based VEX.

This is intentionally a “lint hint” rule: it informs a best practice and does not block builds by default.

---

## 3. Goals / Non-Goals

### Goals (MVP)

1. Detect common “VEX-in-image” patterns (`COPY *.vex.json …`, `COPY foo.vex.json …`).
2. Produce an actionable recommendation: “attach OpenVEX as an OCI attestation instead of shipping it in the image”.
3. Keep rule evaluation **AST-only** (no filesystem / registry access required).

### Non-Goals (MVP)

1. Parsing or validating the VEX document content.
2. Checking whether an OCI attestation already exists for the image (requires registry context).
3. Auto-fixing the Dockerfile (the “fix” is in the build/publish pipeline, not in the Dockerfile alone).
4. Full “VEX-aware vulnerability scanning” (future work).

---

## 4. Rule Semantics (MVP)

### 4.1 What is flagged?

Flag any `COPY` instruction where **any source path** matches:

- `*.vex.json` glob usage (e.g., `COPY *.vex.json /vex/`)
- A concrete file ending with `.vex.json` (e.g., `COPY app.vex.json /vex/`)

The MVP is intentionally simple: it does not attempt to prove that the file ends up in the final image; it just flags the pattern.

### 4.2 What is recommended?

Recommend attaching the OpenVEX document as an OCI attestation (in-toto predicate) using tooling such as:

```bash
# Example (Docker Scout)
docker scout attestation add --file app.vex.json --predicate-type https://openvex.dev/ns/v0.2.0 <image-ref>

# Example (cosign)
cosign attest --predicate app.vex.json --type https://openvex.dev/ns/v0.2.0 <image-ref>
```

The rule message should stay generic (“attach as OCI attestation”) and avoid hard-coding a single tool requirement.

### 4.3 Severity / Category

Recommended defaults:

- **Severity:** `Info` (best practice recommendation)
- **Category:** `security`
- **Default:** Enabled

---

## 5. Implementation Sketch (MVP)

This rule fits Phase 1 (“context-optional”) in `design-docs/07-context-aware-foundation.md`: it only needs Dockerfile instruction semantics.

### 5.1 Rule wiring

- Location: `internal/rules/tally/prefer_vex_attestation.go`
- Implement `rules.Rule` (same pattern as other `internal/rules/tally/*` rules)
- Iterate through `input.Stages` and `stage.Commands`
- Match `*instructions.CopyCommand` and inspect `CopyCommand.SourcePaths`
- Emit a single violation per offending instruction (or per offending source; MVP can choose either)

### 5.2 Matching logic

Use a minimal suffix match:

- Normalize: `strings.ToLower(src)`
- Trigger if `strings.HasSuffix(src, ".vex.json")`

Optionally treat globs as a stronger signal:

- If `strings.Contains(src, "*") && strings.HasSuffix(src, ".vex.json")`, include a slightly more specific message.

### 5.3 Reported message

Keep the message crisp and action-oriented, e.g.:

> “Do not embed OpenVEX (`*.vex.json`) into the image. Prefer attaching VEX as an OCI attestation (in-toto predicate) instead.”

Include a short “detail” field describing the benefits (update without rebuild; better tooling interoperability).

---

## 6. Examples (MVP)

### 6.1 Violation (glob)

```dockerfile
COPY *.vex.json /usr/share/vex/
```

### 6.2 Violation (concrete file)

```dockerfile
COPY app.vex.json /usr/share/vex/app.vex.json
```

### 6.3 Recommended direction (conceptual)

- Keep VEX in your source repo/build artifacts.
- Attach it to the built image in CI/CD as an OCI attestation (post-build).

---

## 7. Future VEX Linting Integration (Context-Aware Roadmap)

This section intentionally aligns with the “context-aware” foundation in `design-docs/07-context-aware-foundation.md` (optional context, registry
client, caching, progressive enhancement).

### 7.1 Reduce false positives with stage reachability

**Problem:** `COPY app.vex.json …` inside a build stage that never reaches the final image is harmless noise.

**Idea:** Use the semantic model’s “reachable/final stages” (similar to `tally/no-unreachable-stages`) and only flag VEX copies in stages that
contribute to the produced image (default last stage, or `--target` when provided).

This is still “Phase 1” (no external context) and improves signal-to-noise.

### 7.2 Expand detection beyond `COPY *.vex.json`

Common variants worth detecting:

- `ADD *.vex.json …` (same outcome: embeds VEX as runtime payload)
- `COPY <<EOF /path/app.vex.json` heredocs (VEX authored inline)
- `RUN curl/wget … -o *.vex.json` (download VEX into image)
- Packaging VEX under well-known paths (`/usr/share/doc`, `/etc`, `/vex/`) regardless of filename suffix (optional heuristic)

These expansions likely require tighter parsing (shell analysis for `RUN`, heredoc tracking, etc.) and should remain opt-in until mature.

### 7.3 Validate VEX documents (filesystem context)

With build context enabled (`--context`, or future auto-detection), tally could:

- Verify that referenced `*.vex.json` files exist (if they remain in build context for attaching)
- Parse and validate basic OpenVEX structure (e.g., `@context`, required fields, statement shapes)
- Enforce internal policy: required `author`, timestamp freshness, required statement justifications, etc.

Implementation options:

- Lightweight schema validation in tally (using `encoding/json/v2`)
- Or reuse upstream OpenVEX parsing: `github.com/openvex/go-vex` (Apache-2.0). Note that it uses `encoding/json` internally; treat as an external
  compatibility boundary.

### 7.4 Registry-aware checks: “does my image have a VEX attestation?”

Once tally grows a `RegistryClient` / “attestation client” in `BuildContext` (see §Advanced Context Features in `07-context-aware-foundation.md`),
we can add rules like:

- `tally/require-vex-attestation` (policy rule, opt-in): fail/warn if an image digest lacks a VEX attestation
- `tally/base-image-vex-present` (informational): report whether each `FROM` image has VEX metadata attached

Key design points:

- **Context optional:** if registry access is not configured, skip cleanly.
- **Cache aggressively:** use `BuildContext.Cache` to avoid repeated remote calls per image/tag.
- **Digest-first:** prefer digest-pinned refs for accurate attestation lookup; otherwise results can be ambiguous.
- **Predicate versions:** treat `https://openvex.dev/ns/*` as “VEX predicate types” and allow configuration to pin/extend accepted versions (future
  proofing for spec evolution).
- **Multi-arch aware:** image indexes may have per-platform attestations; prefer `--platform` when available (similar to how other context-aware
  checks treat platform in `07-context-aware-foundation.md`).
- **Referrers vs fallback:** registries vary in OCI referrers support; our client should handle both native referrers and the common “separate repo /
  tag scheme” fallback used by signing/attestation tooling.

### 7.5 Reusing open-source attestation discovery logic (instead of Docker Scout internals)

Docker Scout’s implementation is not directly reusable as source code: the `docker/scout-cli` repository distributes installable binaries and is
licensed under the Docker Subscription Service Agreement (not an open-source license).

However, there are open-source building blocks we *can* import:

- `github.com/openvex/discovery` (Apache-2.0): provides an “OpenVEX discovery agent” with an OCI prober. It demonstrates fetching OpenVEX
  attestations using Sigstore/cosign APIs (`cosign.FetchAttestations` with predicate type `https://openvex.dev/ns/v0.2.0`), including handling
  multi-arch indexes via platform selection and supporting common cosign repository override mechanisms.
- `github.com/openvex/go-vex` (Apache-2.0): VEX data model + helpers.
- `github.com/sigstore/cosign/v2` + `github.com/google/go-containerregistry` (per upstream licensing): OCI registry access and attestation fetch
  primitives.

This approach matches tally’s “context-aware foundation”: it keeps the rule engine simple while allowing optional enrichment when registry context
is available.

As an additional (optional) integration path, tally could **shell out** to `docker scout` when present to fetch/verify VEX (e.g., `docker scout vex
get --verify`) or to publish attestations. This must remain an explicit opt-in “external tool” integration (binary dependency + credentials +
license constraints), not a core library dependency.

### 7.6 Signature / trust policy checks (advanced)

For ecosystems that sign VEX attestations (e.g., vendor-provided images), tally could optionally verify:

- Signature validity (cosign verification)
- Trusted keys / keyless identities (policy-configured)
- Transparency log requirements (opt-in)

This would likely live behind a separate “trust policy” configuration block and remain disabled by default.

### 7.7 VEX-aware vulnerability reporting (longer-term)

If tally adds vulnerability scanning hooks later (mentioned as a Phase 3 target in `07-context-aware-foundation.md`), VEX can become a first-class
input:

- Apply VEX statements to suppress “not_affected” vulnerabilities from reports
- Highlight mismatches (scanner says vulnerable, VEX says not affected) as a “needs review” bucket
- Track coverage (“% of CVEs in scan have VEX statements”) for supply-chain maturity

This should be introduced only after tally has stable registry context + caching, to avoid fragile network-heavy behavior in the default lint path.

---

## 8. Open Questions

1. Should the MVP rule be `Info` or `Warning` by default?
2. Should the rule also flag `ADD *.vex.json` in MVP (same intent/outcome), or keep the initial scope strictly to `COPY`?
3. Do we want an opt-in “policy mode” rule (`require-vex-attestation`) that can fail CI, separate from the best-practice hint?
