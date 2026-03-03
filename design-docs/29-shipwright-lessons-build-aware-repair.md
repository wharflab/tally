# Lessons from Shipwright: Build-Aware Dockerfile Repair

> Status: research
>
> Source: [STAR-RG/shipwright](https://github.com/STAR-RG/shipwright) — ICSE 2021

Shipwright is an academic system for **automatically repairing broken Dockerfiles**.
It was published at ICSE 2021 (International Conference on Software Engineering) by
Jordan Henkel, Denini Silva, Leopoldo Teixeira, Marcelo d'Amorim, and Thomas Reps
(University of Wisconsin–Madison / Federal University of Pernambuco).

This document extracts every actionable lesson from the Shipwright paper, dataset,
and implementation and maps each to a concrete improvement or new capability for tally.

---

## Table of Contents

1. [Background — What Shipwright Does](#1-background--what-shipwright-does)
2. [Key Finding — Static Linters Miss Most Build Failures](#2-key-finding--static-linters-miss-most-build-failures)
3. [Lesson 1 — Comprehensive Error Taxonomy](#3-lesson-1--comprehensive-error-taxonomy)
4. [Lesson 2 — Deprecated / Renamed Package Detection](#4-lesson-2--deprecated--renamed-package-detection)
5. [Lesson 3 — Missing Dependency Detection](#5-lesson-3--missing-dependency-detection)
6. [Lesson 4 — Base Image Version Pinning and Staleness](#6-lesson-4--base-image-version-pinning-and-staleness)
7. [Lesson 5 — Archive / Repository URL Staleness](#7-lesson-5--archive--repository-url-staleness)
8. [Lesson 6 — Build-Time Validation (Slow Checks)](#8-lesson-6--build-time-validation-slow-checks)
9. [Lesson 7 — ML-Guided Pattern Discovery](#9-lesson-7--ml-guided-pattern-discovery)
10. [Lesson 8 — Fix Safety Taxonomy Validation](#10-lesson-8--fix-safety-taxonomy-validation)
11. [Lesson 9 — Search-Based Recommendations](#11-lesson-9--search-based-recommendations)
12. [Lesson 10 — External vs Fixable Error Classification](#12-lesson-10--external-vs-fixable-error-classification)
13. [Lesson 11 — Empirical Benchmark Dataset](#13-lesson-11--empirical-benchmark-dataset)
14. [Lesson 12 — Real-World Fix Validation via PR Submission](#14-lesson-12--real-world-fix-validation-via-pr-submission)
15. [Lesson 13 — Regex Patterns for Error Log Matching](#15-lesson-13--regex-patterns-for-error-log-matching)
16. [Lesson 14 — DDMin for Diagnostic Minimization](#16-lesson-14--ddmin-for-diagnostic-minimization)
17. [Summary — Priority Matrix](#17-summary--priority-matrix)

---

## 1. Background — What Shipwright Does

Shipwright operates in three phases:

1. **Data collection (offline):** clone GitHub repos, attempt to `docker build` each
   Dockerfile in context, capture stdout/stderr/metadata.
2. **Fix extraction (human-in-the-loop):** embed error logs with Sentence-BERT,
   cluster them with HDBSCAN, have humans inspect clusters and write generalized
   repair patterns.
3. **Patch generation (online):** given a new broken Dockerfile, extract keywords
   from the error log, match to a repair pattern, apply transformation or
   provide web-search suggestions.

Key metrics from the paper:

| Metric | Value |
|--------|-------|
| Dockerfiles built | ~20,000 |
| Build failure rate | **26.3%** |
| Hadolint detection rate of real failures | 33.8% |
| Binnacle detection rate of real failures | 20.6% |
| Shipwright automatic repair rate | ~19% |
| PRs submitted to real repos | 45 |
| PRs accepted | 19 (42%) |

---

## 2. Key Finding — Static Linters Miss Most Build Failures

> *"Static tools miss the majority of actual build failures."*

Hadolint detects only 33.8% and Binnacle only 20.6% of Dockerfiles that
actually fail to build. The remaining ~66% of build failures are invisible to
static analysis.

### Relevance to tally

Tally is a static analyzer and will always have this gap. But the takeaway is
**not** "build everything" — it is: **close as many of those static-detectable
gaps as possible**, and **clearly communicate to users** which categories of
failure lie outside tally's scope.

**Actionable items:**

- Audit tally's rule set against Shipwright's 15 repair patterns (§4–§5 below)
  to ensure we cover the statically detectable subset.
- Add a "what tally cannot catch" section to user-facing docs explaining
  external/runtime failures.
- Consider a future `tally build-check` slow-check mode (see §8).

---

## 3. Lesson 1 — Comprehensive Error Taxonomy

Shipwright's `utils.py` contains **130+ regex patterns** classifying Dockerfile
build errors into two buckets: **external failures** (unfixable by modifying the
Dockerfile) and **Dockerfile-fixable errors**.

### External failure categories (examples)

| Category | Example patterns |
|----------|-----------------|
| Git unavailable | `fatal: repository .* not found`, `Permission denied (publickey)` |
| NPM/Yarn | `npm ERR! 404`, `error Couldn't find package` |
| Python pip | `No matching distribution found`, `setup.py .* error` |
| Go modules | `cannot find module`, `unrecognized import path` |
| Download failures | `wget: server returned error: HTTP/1.1 404`, `curl: (6) Could not resolve host` |
| Compilation | `make: \*\*\* .* Error`, `CMake Error`, `gcc: error` |
| Architecture | `exec format error` |
| Auth | `unauthorized: authentication required`, `UNAUTHORIZED` |
| Registry | `manifest unknown`, `image .* not found` |

### Relevance to tally

Many of these can be heuristically detected **statically** when specific patterns
appear in the Dockerfile itself:

- `wget`/`curl` to a URL that is a known-dead domain or non-HTTPS.
- `pip install` without `--no-cache-dir` or pinned versions.
- `go get` of a module path that triggers known proxy errors.
- `git clone` of a URL using `http://` or referencing an archived repo.

**Actionable items:**

- Introduce a `tally/deprecated-url` or `tally/fragile-download` rule that flags
  `wget`/`curl`/`ADD` with patterns known to be unreliable (e.g., plain HTTP,
  sourceforge deep links, deprecated GitHub raw URLs).
- Consider a `tally/missing-error-handling` rule for `RUN` chains that do
  network operations without `set -e` / `|| exit 1` (already partially covered
  by ShellCheck).

---

## 4. Lesson 2 — Deprecated / Renamed Package Detection

Shipwright's `transform.py` includes several repair patterns for **packages that
were renamed or removed** across OS releases:

| Old package | Replacement | Distro |
|-------------|-------------|--------|
| `libpng12-dev` | `libpng-dev` | Ubuntu 18.04+ |
| `python-imaging` | `python-pil` | Ubuntu 18.04+ |
| `python-software-properties` | `software-properties-common` | Ubuntu 14.04+ |
| `python-pip` | install via `get-pip.py` or `python3-pip` | Ubuntu 18.04+ |
| `python-dev` | `python3-dev` | Ubuntu 20.04+ |
| `bzr` (Bazaar) | removed entirely | Ubuntu 20.04+ |

### Relevance to tally

This is a **high-value, low-risk** static check. It can be implemented with a
lookup table keyed on `(base_image_family, package_name)`.

**Actionable items:**

- New rule: `tally/deprecated-package` — detect `apt-get install` / `apk add`
  of packages known to be removed or renamed on the detected base image family.
- Use tally's existing stage-aware semantic model to resolve the base image
  family (Debian/Ubuntu version, Alpine version) and conditionally flag.
- Ship an initial table covering the 10–20 most commonly broken packages from
  Shipwright's dataset. Allow user-extensible overrides in `.tally.toml`.
- Fix safety: `FixSafe` when the replacement is a 1:1 rename; `FixSuggestion`
  when the replacement requires a different install method (e.g., `get-pip.py`).

---

## 5. Lesson 3 — Missing Dependency Detection

Shipwright repairs Dockerfiles where a command is missing because a required
package was never installed. Examples:

| Error signature | Missing package | Fix |
|----------------|-----------------|-----|
| `npm: not found` | `nodejs` / `npm` | Add `RUN apt-get install -y nodejs npm` or use a node base image |
| `pip3: not found` | `py3-pip` (Alpine) | Add `RUN apk add py3-pip` |
| `gpg: command not found` | `gnupg2` | Add `RUN apt-get install -y gnupg2` |
| `curl: not found` | `curl` | Add `RUN apt-get install -y curl` |

### Relevance to tally

Statically detecting "command X is used but never installed" requires:

1. Parsing `RUN` shell commands to extract invoked binaries.
2. Knowing which packages are pre-installed in the base image (or at minimum,
   which are *not* pre-installed in common slim/minimal images).
3. Checking whether an earlier `RUN` installs a package that provides the binary.

This is partially feasible today for common commands (`curl`, `wget`, `git`,
`npm`, `pip`) in well-known base images (`alpine`, `ubuntu`, `debian`).

**Actionable items:**

- New rule (future, context-aware): `tally/missing-command` — detect `RUN`
  instructions that invoke a binary not obviously available in the base image
  and not installed in a preceding layer.
- Start with a high-confidence subset: flag `curl` / `wget` / `git` / `npm` /
  `pip` usage in `FROM scratch`, `FROM alpine`, `FROM debian:*-slim`,
  `FROM ubuntu:*` (minimal images) when no prior `RUN` installs them.
- Fix: `FixSuggestion` — suggest the appropriate install command for the
  detected package manager.

---

## 6. Lesson 4 — Base Image Version Pinning and Staleness

Shipwright's most common repair pattern is **changing or pinning the base image
version** (e.g., `ubuntu` → `ubuntu:18.04`). The paper found that many failures
occur because:

- A `FROM ubuntu` (unpinned `:latest`) silently moved to a new major release.
- A `FROM ruby:2.3` is EOL and its packages are no longer in upstream mirrors.

### Relevance to tally

Tally already has `hadolint/DL3006` (tag-your-base-images) for the pinning
case. The **staleness** angle is new.

**Actionable items:**

- New rule (slow-check, registry-backed): `tally/stale-base-image` — warn when
  a base image tag references an OS/runtime version that is past its official
  EOL date.
  - Maintain a curated EOL table: Ubuntu LTS dates, Debian release dates,
    Alpine branch dates, Node.js LTS schedule, Python release schedule.
  - This fits cleanly into tally's `fix.FixResolver` pattern: the resolver
    fetches the EOL table (bundled or remote) and the rule evaluates locally.
- Extend `hadolint/DL3006` fix to suggest the **latest LTS tag** for the
  detected image family, not just "pin a tag".

---

## 7. Lesson 5 — Archive / Repository URL Staleness

Several Shipwright repairs fix `apt` source list issues:

- Old Ubuntu releases archived to `old-releases.ubuntu.com`.
- Maven Central requiring HTTPS (returning 501 on HTTP).
- `curl` downloads failing because the upstream URL moved (missing `-L` flag
  for redirects).

### Relevance to tally

These are detectable statically:

**Actionable items:**

- New rule: `tally/http-url-in-run` — flag `RUN` instructions containing `http://`
  URLs in `curl`/`wget`/`ADD` where HTTPS is available or expected (Maven Central,
  GitHub releases, PyPI, npmjs, etc.).
  - Fix: `FixSafe` — rewrite `http://` → `https://`.
- New rule: `tally/curl-missing-follow-redirects` — flag `curl` invocations
  that download a URL but lack `-L` / `--location`.
  - Fix: `FixSafe` — add `-L` to the curl flags.
- New rule (future): `tally/ubuntu-eol-archive` — detect `RUN` lines that
  reference `archive.ubuntu.com` in combination with a base image known to be
  EOL, and suggest switching to `old-releases.ubuntu.com` or upgrading the base.

---

## 8. Lesson 6 — Build-Time Validation (Slow Checks)

Shipwright's core thesis is that **actually building** catches failures invisible
to static analysis. While tally is fundamentally a static tool, the existing
slow-checks infrastructure (design-doc 15) provides a natural extension point.

### Potential slow-checks inspired by Shipwright

| Check | What it does | Speed |
|-------|-------------|-------|
| `tally/base-image-exists` | HEAD the manifest for each `FROM` image via registry API | ~100ms per image |
| `tally/base-image-platform` | Verify the manifest includes the user's target `--platform` | Same request |
| `tally/stale-base-image` | Check EOL status (see §6) | Cached lookup |

These all fit the `FixResolver` / async-check model from design-doc 15 and do
**not** require running `docker build`.

**Actionable items:**

- Implement `tally/base-image-exists` as a slow-check gated behind
  `--slow-checks=on` (or `auto` in CI). Uses `containers/image/v5` — already
  a dependency.
- Implement `tally/base-image-platform` alongside it (same manifest request).
- These directly address the #1 failure category Shipwright found: base image
  no longer available or missing platform.

---

## 9. Lesson 7 — ML-Guided Pattern Discovery

Shipwright uses **Sentence-BERT + HDBSCAN** to cluster ~5,400 broken
Dockerfiles by semantic similarity of their error logs, then has humans
inspect clusters and write generalized fixes.

### ML pipeline details

1. **Embedding model:** `bert-large-nli-stsb-mean-tokens` from the
   [sentence-transformers](https://www.sbert.net/) library — a BERT variant
   tuned for English sentence similarity (Reimers & Gurevych, "Sentence-BERT:
   Sentence Embeddings using Siamese BERT-Networks," EMNLP 2019). Applied
   directly to cleaned build-error logs with **no domain-specific fine-tuning**.
2. **Clustering:** HDBSCAN (Campello, Moulavi & Sander, "Density-based
   clustering based on hierarchical density estimates," 2013). Grid search over
   hyperparameters: `min_cluster_size` 2–21, `min_samples` 1–10 (144
   configurations evaluated).
3. **Results:** 144 distinct clusters; ~1,814 Dockerfiles clustered, ~3,586
   left as outliers (HDBSCAN naturally excludes noise points rather than
   forcing everything into a cluster).

### Key insight from the authors (presentation slide 17)

> *"Our approach to clustering isn't sophisticated. We live in a world where
> super-large off-the-shelf models can be applied in many domains to produce
> **decent** results (even without fine-tuning)."*
>
> *"Early on, we realized that a completely automated approach would be too
> limited to be of much use. By making the work for a human **easier** we
> create a tool that is capable of providing actionable fixes in more
> scenarios."*

The takeaway: the ML is not the star — it is a **force multiplier for humans**.
Instead of inspecting 5,400 failures one by one, a human inspects 144 clusters
and writes one generalized fix per cluster.

### Relevance to tally

Tally does not (and should not) run ML at lint time. But the **methodology**
is valuable for **rule discovery and prioritization**:

**Actionable items:**

- **Corpus mining for new rules:** Collect build logs from public CI systems
  (GitHub Actions, GitLab CI) where `docker build` fails. Cluster the errors
  to discover the most common failure patterns not yet covered by tally rules.
- **Prioritization:** Weight rule implementation by the cluster size — larger
  clusters = more developers affected = higher priority.
- **Regression tracking:** After adding a new rule, re-run the corpus check to
  measure how many previously-uncaught failures tally now detects.

This is an **offline, periodic** activity (quarterly or per-release), not a
runtime feature.

---

## 10. Lesson 8 — Fix Safety Taxonomy Validation

Shipwright classifies its outputs into three buckets:

| Bucket | Meaning | Shipwright rate |
|--------|---------|-----------------|
| **Repair** | Automated code transformation applied | ~19% |
| **Suggestion** | Web search URLs provided | ~47–70% |
| **Unknown** | No coverage | ~10–35% |

Tally already has a richer fix-safety model (`FixSafe`, `FixSuggestion`,
`FixUnsafe`). Shipwright's data validates this design:

- Their "repairs" map to `FixSafe` — deterministic, high-confidence transforms.
- Their "suggestions" map to `FixSuggestion` — actionable but requiring human
  judgment.
- Their "unknown" underscores the need for clear "no fix available" messaging.

**Actionable items:**

- Ensure every tally rule without a fix has at least a `FixSuggestion` pointing
  to relevant documentation or best-practice guides.
- Review existing `FixSafe` fixes against Shipwright's 45-PR acceptance data:
  42% acceptance on first pass is a good baseline. Tally's fixes should aim
  for >80% acceptance since they target best practices rather than emergency
  repairs.
- Add a `--explain` flag or `tally explain <rule-id>` subcommand that provides
  extended rationale and links (the tally equivalent of Shipwright's
  search-based suggestions).

---

## 11. Lesson 9 — Search-Based Recommendations

When Shipwright cannot generate a repair, it falls back to **keyword extraction
from error logs → web search → curated URL results** from a whitelist of
domains (Stack Overflow, GitHub Issues, Ask Ubuntu, etc.).

It also uses **DDMin (Delta Debugging Minimization)** to simplify search queries
by iteratively removing keywords while maintaining useful results.

### Relevance to tally

Tally could provide similar contextual help without running builds:

**Actionable items:**

- For rules that flag complex issues (e.g., `tally/prefer-multi-stage-build`,
  `tally/stale-base-image`), include a `help_url` field in the violation
  pointing to a curated doc page or relevant Stack Overflow canonical.
- Consider a `tally explain <rule-id>` subcommand that outputs the rule
  rationale, common causes, example fixes, and links to relevant resources.
- The DDMin approach could be useful if tally ever implements a "search for
  help" feature, but this is low priority vs. direct rule coverage.

---

## 12. Lesson 10 — External vs Fixable Error Classification

Shipwright's `utils.py` distinguishes **130+ patterns** of external failures
from Dockerfile-fixable errors. This classification determines whether the
system attempts a repair or falls back to suggestions.

### Relevance to tally

This maps to tally's existing concern of **false positives**: reporting issues
that users cannot fix by editing their Dockerfile.

**Actionable items:**

- When designing new rules, explicitly document which failure categories are
  **in scope** (Dockerfile-fixable) vs **out of scope** (external/environmental).
- For context-aware rules that touch the network (slow-checks), clearly
  distinguish "the base image does not exist" (Dockerfile-fixable: change the
  `FROM` line) from "the registry is down" (external: retry later).
- Error messages should include the classification: "This is a Dockerfile issue:
  ..." vs "This may be an environmental issue: ...".

---

## 13. Lesson 11 — Empirical Benchmark Dataset

Shipwright provides a dataset of **5,405 broken Dockerfiles** with full build
logs, clustered error data, and metadata. This is a rare public benchmark for
Dockerfile tooling.

### Relevance to tally

**Actionable items:**

- **Benchmark tally against Shipwright's dataset:** Run tally on all 5,405
  broken Dockerfiles and measure:
  - How many does tally flag at least one violation for?
  - How many of the 15 Shipwright repair patterns does tally already cover?
  - What is the false-positive rate on these files?
- **Create a tally-specific benchmark corpus:** Curate a set of real-world
  Dockerfiles (both correct and broken) to use for regression testing and
  rule coverage measurement.
- **Compare across releases:** Track tally's detection rate on the benchmark
  across releases to ensure forward progress.

---

## 14. Lesson 12 — Real-World Fix Validation via PR Submission

Shipwright submitted 45 PRs to real GitHub repositories and achieved a 42%
acceptance rate. This methodology validates that automated fixes are practical.

They also found that in 23 out of 102 cases where developers later fixed the
same Dockerfile, Shipwright had produced an equivalent repair.

### Relevance to tally

**Actionable items:**

- **Validate `--fix` quality:** Take a sample of popular open-source projects
  with known Dockerfile issues, run `tally --fix`, and submit PRs. Track
  acceptance rate as a quality metric.
- **Dogfood internally:** Run `tally --fix` on tally's own test fixtures and
  integration Dockerfiles to verify fix quality.
- **Publish results:** A "tally fixed X Dockerfiles across Y repos with Z%
  acceptance" metric would be compelling marketing/documentation material.

---

## 15. Lesson 13 — Regex Patterns for Error Log Matching

Shipwright's implementation includes battle-tested regex patterns for matching
common Dockerfile build errors. Selected high-value patterns:

```python
# Ruby version requirement
r'requires (r|R)uby version >= \d\.\d(\.\d|)'

# APT package not found
r"Package '.*' has no installation candidate"

# Python install conflict
r"Cannot uninstall '.*'"

# Missing command (sh: <cmd>: not found)
r'sh:.*:*not found'

# GPG key error
r'NO_PUBKEY [A-F0-9]+'

# Deprecated repository
r'does not have a Release file'
```

### Relevance to tally

These patterns are useful for:

1. **Improving error messages:** When tally detects a pattern that commonly
   leads to one of these build errors, include the likely error message in the
   violation explanation.
2. **Future build-log analysis:** If tally ever gains a `tally diagnose`
   command that analyzes build output, these patterns provide a starting regex
   library.
3. **Rule design:** Each pattern above suggests a static check that could
   prevent the error before build time.

---

## 16. Lesson 14 — DDMin for Diagnostic Minimization

Shipwright uses **Delta Debugging Minimization (DDMin)** to simplify search
queries by iteratively removing terms while maintaining useful results.

### Relevance to tally

DDMin is a general-purpose technique applicable beyond search queries:

**Potential future applications:**

- **Minimal reproducer generation:** Given a Dockerfile that triggers a tally
  violation, DDMin could strip irrelevant instructions to produce a minimal
  example for bug reports.
- **Fix verification:** DDMin could minimize a fix to its essential changes,
  ensuring no unnecessary modifications are included.
- **Test case reduction:** Automatically minimize failing test fixtures.

This is **low priority** but worth noting as a technique in the toolbox.

---

## 17. Summary — Priority Matrix

| # | Lesson | New Rule / Feature | Effort | Impact | Priority |
|---|--------|--------------------|--------|--------|----------|
| 1 | Deprecated packages | `tally/deprecated-package` | Medium | High | **P1** |
| 2 | Base image exists | `tally/base-image-exists` (slow-check) | Medium | High | **P1** |
| 3 | HTTP URLs in RUN | `tally/http-url-in-run` | Low | Medium | **P1** |
| 4 | curl -L missing | `tally/curl-missing-follow-redirects` | Low | Medium | **P1** |
| 5 | Base image platform | `tally/base-image-platform` (slow-check) | Low | High | **P1** |
| 6 | Stale base image | `tally/stale-base-image` (slow-check) | Medium | High | **P2** |
| 7 | Missing command | `tally/missing-command` (context-aware) | High | High | **P2** |
| 8 | Ubuntu EOL archive | `tally/ubuntu-eol-archive` | Medium | Low | **P3** |
| 9 | Benchmark against dataset | Testing/validation | Medium | High | **P2** |
| 10 | PR submission validation | Marketing/quality | Low | Medium | **P3** |
| 11 | `tally explain` subcommand | UX | Medium | Medium | **P3** |
| 12 | Corpus mining for rules | Process | High | High | **P3** |
| 13 | DDMin for minimization | Tooling | High | Low | **P4** |

### Quick wins (P1, low effort)

- `tally/http-url-in-run` — simple regex on `RUN` shell content.
- `tally/curl-missing-follow-redirects` — simple shell parse check.
- `tally/base-image-platform` — piggyback on existing registry slow-check infra.

### High-impact medium effort (P1–P2)

- `tally/deprecated-package` — lookup table + base image family detection.
- `tally/base-image-exists` — registry HEAD request via slow-check.
- `tally/stale-base-image` — EOL table + registry query.
- Benchmark tally against Shipwright's 5,405 broken Dockerfiles.

### Strategic investments (P2–P3)

- `tally/missing-command` — requires richer semantic model of available binaries.
- Corpus mining via BERT+HDBSCAN for rule discovery and prioritization.
- Real-world PR submission for fix validation.

---

## References

- Henkel, J., Silva, D., Teixeira, L., d'Amorim, M., Reps, T. (2021).
  "Shipwright: A Human-in-the-Loop System for Dockerfile Repair."
  *ICSE 2021.* [GitHub](https://github.com/STAR-RG/shipwright)
- Tally design-doc 07: Context-Aware Linting Foundation
- Tally design-doc 15: Async Checks (Slow Operations)
- Tally design-doc 20: BuildKit-Parseable but Non-Buildable Dockerfiles

---

## Appendix A — Shipwright's 15 Repair Transforms

For reference, the complete set of automated repair patterns implemented in
Shipwright's `transform.py`:

| # | Transform | Static? | Tally coverage |
|---|-----------|---------|----------------|
| 1 | Change base image tag | Partially (slow-check) | `hadolint/DL3006` (pin only) |
| 2 | Update Ruby version in Gemfile | No (runtime) | — |
| 3 | Install `yum-plugin-ovl` | Yes (heuristic) | — |
| 4 | Rename `libpng12-dev` → `libpng-dev` | Yes | — (proposed: `tally/deprecated-package`) |
| 5 | Rename `python-imaging` → `python-pil` | Yes | — (proposed: `tally/deprecated-package`) |
| 6 | Install missing `npm` | Partially | — (proposed: `tally/missing-command`) |
| 7 | Update base image + fix apt sources | Partially (slow-check) | — (proposed: `tally/stale-base-image`) |
| 8 | Fix Ruby Gemfile version conflict | No (runtime) | — |
| 9 | Set `ENV LANG C.UTF-8` for Ruby | Yes (heuristic) | — |
| 10 | Remove deprecated `bzr` | Yes | — (proposed: `tally/deprecated-package`) |
| 11 | Add `curl -L` flag | Yes | — (proposed: `tally/curl-missing-follow-redirects`) |
| 12 | Fix old Ubuntu archives URL | Yes | — (proposed: `tally/ubuntu-eol-archive`) |
| 13 | Rename `python-dev` → `python3-dev` | Yes | — (proposed: `tally/deprecated-package`) |
| 14 | Add `pip --ignore-installed` | Yes (heuristic) | — |
| 15 | Add `gnupg2` for apt-key | Partially | — (proposed: `tally/missing-command`) |

**Coverage summary:** Of 15 Shipwright transforms, **11 are statically detectable**
(fully or partially). Tally currently covers **1** (`hadolint/DL3006` for base
image pinning). The proposals in this document would cover **10 more**.

---

## Appendix B — Shipwright's External Failure Taxonomy (Selected)

Categories from `utils.py` that are explicitly **out of scope** for tally
(cannot be fixed by editing the Dockerfile, or require network access to verify):

- Git repository not found / access denied
- NPM package not found / registry error
- Python package build failure (C extension compilation)
- Go module proxy errors
- Test suite failures inside `RUN`
- Architecture mismatch (`exec format error`)
- Compilation errors (gcc, make, cmake)
- DNS resolution failures
- Certificate verification errors
- Rate limiting / throttling

These are documented here to **prevent scope creep**: tally should not attempt
to detect or fix these categories, but may mention them in documentation to set
user expectations.
