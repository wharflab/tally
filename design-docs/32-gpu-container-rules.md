# 32. GPU Container Rules (`tally/gpu/*`)

**Status:** Proposed

**Design Focus:** Consolidate the GPU Dockerfile research into one implementation plan for a new `tally/gpu/*` namespace that is feasible with the
current Tally architecture, strong on real-world signal, and explicit about which ideas should stay out of the namespace.

---

## 1. Decision

Introduce a dedicated `tally/gpu/*` namespace for GPU-specific Dockerfile rules.

The namespace should prioritize:

1. Rules independently rediscovered by all three research efforts.
2. Rules that catch correctness problems or stale GPU-specific configuration.
3. Rules that teach a better GPU container pattern without duplicating generic Dockerfile hygiene.
4. Rules with a credible fix story:
   - `FixSafe` when a narrow text edit is genuinely safe
   - `FixSuggestion` when a resolver or review is needed
   - `FixUnsafe` via AI/ACP when the right fix is structural and repository-specific

The namespace should **not** absorb generic package-manager hygiene such as `pip --no-cache-dir`, `apt --no-install-recommends`, or generic BuildKit
cache-mount advice. Tally already has, or should have, those rules outside the GPU namespace.

---

## 2. Ground Truth

### 2.1 Official guidance that should shape the rules

- Docker and NVIDIA treat GPU selection primarily as a **runtime concern**: GPUs can be selected with `docker run --gpus ...` or via
  `NVIDIA_VISIBLE_DEVICES`.
- NVIDIA documents `NVIDIA_DRIVER_CAPABILITIES` and states that the default capability set is `compute,utility`; `all` exposes more driver surface.
- NVIDIA's CUDA base images already set the standard NVIDIA runtime environment variables and set `NVIDIA_REQUIRE_CUDA`.
- Hugging Face explicitly recommends `nvidia/cuda` as the base image for GPU Docker containers and explicitly warns that **GPU hardware is not
  available during `docker build`**, so commands such as `nvidia-smi` or `torch.cuda.is_available()` should not run in `RUN`.
- uv's official PyTorch integration supports explicit CUDA wheel indexes, backend-aware installs, and lockfile-based workflows, which makes a narrow
  "prefer uv over conda" rule realistic for a subset of GPU Python images.

### 2.2 Corpus signal

One research pass collected **207 unique real-world GPU Dockerfiles** after deduplication and filtering.

| Finding | Count |
|---|---:|
| Dockerfiles using `nvidia/cuda` somewhere | 201 / 207 |
| Dockerfiles using a `devel` tag in any stage | 111 / 207 |
| Dockerfiles keeping `devel` in the final stage | 99 / 207 |
| Final-stage `devel` with no obvious build signal in the final stage | 33 / 207 |
| Multi-stage Dockerfiles | 23 / 207 |
| Multi-stage Dockerfiles with build tools or `nvcc` | 13 / 207 |
| Dockerfiles reinstalling CUDA stack packages on top of `nvidia/cuda` | 5 / 207 |
| Dockerfiles setting `ENV NVIDIA_VISIBLE_DEVICES=...` | 2 / 207 |
| Dockerfiles setting `ENV NVIDIA_DRIVER_CAPABILITIES=...` | 2 / 207 |
| Dockerfiles setting `NVIDIA_DRIVER_CAPABILITIES=all` | 1 / 207 |
| Dockerfiles installing `nvidia-container-toolkit` inside the image | 1 / 207 |

A second GPU+conda pass collected **252 unique Dockerfiles**:

| Finding | Count |
|---|---:|
| `RUN conda|mamba|micromamba install ...` | 85 |
| "simple conda install" pattern | 79 |
| conda installs of ML Python packages | 61 |
| conda installs of GPU Python stack (`cudatoolkit`, `pytorch-cuda`, `cudnn`, `nccl`) | 46 |
| `conda env create ...` | 43 |
| `environment.yml` present | 28 |

The important split is between:

- **narrow, migratable GPU Python images** where conda is mostly a package installer, and
- **real environment-management workflows** where a simple heuristic uv migration would be too aggressive.

### 2.3 Representative examples worth preserving

| Pattern | Example |
|---|---|
| Final-stage `devel` with no obvious compiler use | [ARoese/GravitationalRayTracing `Dockerfile-dev`](https://github.com/ARoese/GravitationalRayTracing/blob/4d094a5d6145278898f0a29f52a8d8e508344d66/Dockerfile-dev) |
| Hardcoded `NVIDIA_VISIBLE_DEVICES=all` and `NVIDIA_DRIVER_CAPABILITIES=all` | [MayaVB/SRP-DNN-CP `Dockerfile`](https://github.com/MayaVB/SRP-DNN-CP/blob/1ebae8301520f4bb537cf304d3fe44853e538bbf/Dockerfile) |
| Installing `nvidia-container-toolkit` in the image | [celesrenata/nixos-k3s-configs `Dockerfile.new3`](https://github.com/celesrenata/nixos-k3s-configs/blob/affc596951b210645b18b003352f076c84aa64a6/Blender/docker/Dockerfile.new3) |
| Reinstalling CUDA toolkit on top of `nvidia/cuda` | [birosjh/yolov3_dyhead `Dockerfile`](https://github.com/birosjh/yolov3_dyhead/blob/556595a7c12bf264ec08ab4a00a865312e9fbe2c/Dockerfile) |
| Simple mixed conda/pip GPU stack that looks migratable | [Hsun1128/AICUP-2024 `dockerfile`](https://github.com/Hsun1128/AICUP-2024/blob/33c943c758bf84667b1f0dcbd893f794584c8255/dockerfile) |
| Heavy builder-oriented conda workflow that should not trigger a narrow uv rule | [Preemo-Inc/text-generation-inference `Dockerfile`](https://github.com/Preemo-Inc/text-generation-inference/blob/972e9a7f7c111b58679248f82bebab9237adbbf2/Dockerfile) |
| `environment.yml`-driven workflow that should not trigger a narrow uv rule | [BayraktarLab/cell2location `Dockerfile`](https://github.com/BayraktarLab/cell2location/blob/afb07fda7f89458e44a4afdf21c26aeb7251ebfa/Dockerfile) |

Positive reference implementations repeatedly mentioned across the research:

- [huggingface/text-generation-inference](https://github.com/huggingface/text-generation-inference)
- [pytorch/pytorch](https://github.com/pytorch/pytorch)
- [NVIDIA/DeepLearningExamples](https://github.com/NVIDIA/DeepLearningExamples)
- [ollama/ollama](https://github.com/ollama/ollama)
- [vllm-project/vllm](https://github.com/vllm-project/vllm)
- [mudler/LocalAI](https://github.com/mudler/LocalAI)
- [google-research/corenet](https://github.com/google-research/corenet)

---

## 3. Applicability Detection

Treat a Dockerfile as GPU-relevant if any of the following are true:

1. Base image matches `nvidia/cuda:*`, `nvcr.io/nvidia/*`, or `nvidia/cudagl:*`.
2. Base image matches `pytorch/pytorch:*cuda*` or `tensorflow/tensorflow:*gpu*`.
3. The Dockerfile sets `NVIDIA_VISIBLE_DEVICES`, `NVIDIA_DRIVER_CAPABILITIES`, or `CUDA_VISIBLE_DEVICES`.
4. The Dockerfile installs CUDA userspace packages such as `cuda-toolkit*`, `cuda-runtime*`, `libcudnn*`, or `tensorrt*`.
5. The Dockerfile installs PyTorch/TensorFlow/JAX GPU wheels using CUDA-specific indexes or suffixes such as `cu118`, `cu121`, or `cu128`.
6. The Dockerfile builds known CUDA extensions such as `flash-attn`, `xformers`, `torch-scatter`, `torch-sparse`, or `apex`.

The namespace should remain focused on **GPU-specific** correctness, image-shape, runtime-policy, and CUDA/PyTorch alignment problems.

### 3.1 How gating should work in implementation

Yes: default-enabled `tally/gpu/*` rules should be **gated by GPU signals**.

This should follow the same high-level pattern as the Windows design, with one important difference:

- **Windows rules** have a strong semantic gate: `StageInfo.BaseImageOS == Windows`.
- **GPU rules** do **not** have one equally strong binary gate. "GPU-relevant" is heuristic and sometimes stage-local, sometimes file-wide.

So the GPU namespace should **not** use config-level gating and should **not** rely on one global "GPU mode" switch.
Instead, **each rule gates itself** inside `Check()` using shared GPU signal helpers.

Recommended shape:

- Add a helper package, for example `internal/rules/tally/gpu/gate.go`.
- Provide stage/file helpers such as:
  - `StageLooksGPURelevant(...)`
  - `DockerfileLooksGPURelevant(...)`
  - `StageUsesOfficialCUDABase(...)`
  - `StageHasExplicitGPURuntimeEnv(...)`
  - `StageInstallsCUDAUserspace(...)`
  - `StageUsesCUDAWheelIndex(...)`
- Rules use those helpers at the top of `Check()`, just like Windows rules use `BaseImageOS` gating.

In other words, the applicability signals in this section are **rule-evaluation gates**, not just documentation.

### 3.2 No single GPU gate

Unlike `tally/windows/*`, GPU rules should not all use one identical early return.

There are two classes of GPU rules:

1. **Rules where the trigger is itself the GPU signal**
2. **Rules that need an explicit pre-gate to avoid noise**

#### Class 1: the trigger is already GPU-specific

These rules do not need a separate pre-gate beyond their own trigger:

- `tally/gpu/no-container-runtime-in-image`
- `tally/gpu/no-buildtime-gpu-queries`
- `tally/gpu/no-hardcoded-visible-devices`
- `tally/gpu/prefer-minimal-driver-capabilities`
- `tally/gpu/require-torch-cuda-arch-list`
- `tally/gpu/cuda-image-upgrade`
- `tally/gpu/deprecated-cuda-image`

Example:

- if a Dockerfile sets `ENV NVIDIA_DRIVER_CAPABILITIES=all`, that line is already sufficient GPU signal.
- if a Dockerfile runs `nvidia-smi` or `torch.cuda.is_available()` in `RUN`, that query is already sufficient GPU signal.

#### Class 2: rule needs explicit GPU pre-gating

These rules should only evaluate when some broader GPU signal is present:

- `tally/gpu/prefer-runtime-final-stage`
- `tally/gpu/prefer-uv-over-conda`
- `tally/gpu/cuda-version-mismatch`
- `tally/gpu/no-redundant-cuda-install`
- `tally/gpu/hardcoded-cuda-path`
- `tally/gpu/ld-library-path-overwrite`
- `tally/gpu/model-download-in-build`
- `tally/gpu/missing-nvidia-require` (if shipped later)

Example:

- `ld-library-path-overwrite` is too generic unless the stage already looks GPU-related.
- `model-download-in-build` is also broader than GPU; if it stays in this namespace, it must be gated by other GPU signals first.

### 3.3 Stage-level gate first, file-level gate second

Implementation should prefer the narrowest gate that matches the rule shape:

- **Stage-local rules** should gate on stage-local signals.
- **Cross-stage rules** should aggregate signals across the Dockerfile.

Examples:

- `prefer-runtime-final-stage`:
  - primary gate is the final stage base/tag itself (`nvidia/cuda:*devel*`, `nvcr.io/nvidia/*`, etc.)
  - supporting evidence can come from the whole Dockerfile
- `cuda-version-mismatch`:
  - gate on a stage that both uses a CUDA-capable base and installs CUDA-specific framework artifacts
- `prefer-uv-over-conda`:
  - gate on file/stage evidence that the workflow is both GPU-oriented and conda-based

### 3.4 Selection and config behavior

The gate should be inside the rule, not in config selection.

That means:

- `--select tally/gpu/*` still works normally
- enabled-by-default GPU rules still appear in the global rule registry
- on non-GPU Dockerfiles, the rules simply return no violations after the internal applicability check

This is the same model as the Windows design:

- namespace is for organization and user control
- applicability is enforced inside rule evaluation

### 3.5 Recommended policy

Use this decision rule:

- if firing on a non-GPU Dockerfile would feel obviously wrong, add a GPU applicability pre-gate
- if the pattern itself is already unmistakably GPU-specific, the trigger is enough

That keeps the namespace usable by default without requiring users to opt into a special "GPU mode".

---

## 4. Naming Decisions

These are the normalized rule names the namespace should use.

| Final rule ID | Alternatives merged | Why this name wins |
|---|---|---|
| `tally/gpu/prefer-runtime-final-stage` | `devel-as-final`, `separate-build-stages` | Says exactly what the user should prefer, not just what was wrong. |
| `tally/gpu/no-redundant-cuda-install` | `no-redundant-cuda-package-install`, `redundant-cuda-install` | Shorter, more natural, and still accurate; MVP scope stays limited to package-manager installs. |
| `tally/gpu/no-buildtime-gpu-queries` | `nvidia-smi-in-build` | Covers `nvidia-smi` and runtime framework checks such as `torch.cuda.is_available()`. |
| `tally/gpu/no-hardcoded-visible-devices` | `no-hardcoded-visible-devices`, `cuda-visible-devices-in-env` | Unifies both `NVIDIA_VISIBLE_DEVICES` and `CUDA_VISIBLE_DEVICES` under one deployment-policy concept. |
| `tally/gpu/prefer-minimal-driver-capabilities` | `driver-capabilities-too-broad` | Positive least-privilege phrasing is more aligned with existing Tally naming. |
| `tally/gpu/require-torch-cuda-arch-list` | `torch-cuda-arch-list`, `missing-torch-cuda-arch-list` | More explicit about the expectation and when the rule applies. |
| `tally/gpu/prefer-uv-over-conda` | same | Already intuitive and aligned with the actual migration story. |

---

## 5. Strategic Priority Ranking

This ranking answers "what should define the namespace?" It is intentionally **not** the same thing as engineering ship order.

| Rank | Rule | Why it should be high | Fix path |
|---|---:|---|---|
| 1 | `tally/gpu/prefer-runtime-final-stage` | Independently rediscovered by all three reports; biggest real-world image-shape problem; strongest GPU-specific ACP story. | Detect now, AI AutoFix later |
| 2 | `tally/gpu/prefer-uv-over-conda` | Independently rediscovered by all three reports; strongest AI/ACP and marketing story; real corpus support for a narrow migratable subset. | Detect now, AI AutoFix later |
| 3 | `tally/gpu/cuda-version-mismatch` | Actual runtime correctness issue; educational and fixable with a resolver. | Detect now, async resolver later |
| 4 | `tally/gpu/no-redundant-cuda-install` | High-confidence warning with clear value and low implementation cost. | Diagnostic only |
| 5 | `tally/gpu/no-container-runtime-in-image` | High-confidence warning about host/runtime responsibility boundaries. | Diagnostic only |
| 6 | `tally/gpu/no-buildtime-gpu-queries` | Catches real build failures using explicit official guidance. | Diagnostic only |
| 7 | `tally/gpu/no-hardcoded-visible-devices` | Prevents baked-in deployment policy and redundant runtime defaults. | Narrow safe fix + suggestion |
| 8 | `tally/gpu/prefer-minimal-driver-capabilities` | Good least-privilege heuristic; low cost and easy to explain. | Suggestion only |
| 9 | `tally/gpu/require-torch-cuda-arch-list` | Valuable for build time and artifact size, but only for a narrower subset of Dockerfiles. | Suggestion only |
| 10 | `tally/gpu/hardcoded-cuda-path` | Real upgrade fragility, but lower impact than the rules above. | Suggestion first |
| 11 | `tally/gpu/ld-library-path-overwrite` | Can break base-image library search paths, but intent is sometimes ambiguous. | Suggestion first |
| 12 | `tally/gpu/deprecated-cuda-image` | Useful, but needs a maintained support policy table. | Suggestion only |
| 13 | `tally/gpu/cuda-image-upgrade` | Valuable resolver demo, but patch-level freshness is a slower and more moving target. | Async resolver |
| 14 | `tally/gpu/model-download-in-build` | Good architectural guidance, but low fixability and more noise-prone. | Diagnostic only |
| 15 | `tally/gpu/missing-nvidia-require` | Useful in some custom images, but weaker than the rules above. | Suggestion only |

The top two rules are on top because all three reports independently converged on them, and because they line up with Tally's AI/ACP differentiation.

---

## 6. Recommended Engineering Ship Order

### Wave 1: low-risk AST rules

Ship first:

1. `tally/gpu/no-container-runtime-in-image`
2. `tally/gpu/no-redundant-cuda-install`
3. `tally/gpu/no-buildtime-gpu-queries`
4. `tally/gpu/no-hardcoded-visible-devices`
5. `tally/gpu/prefer-minimal-driver-capabilities`

Why: these are cheap to implement, easy to test, and do not need new infrastructure.

### Wave 2: stage-aware and version-aware heuristics

Ship next:

1. `tally/gpu/prefer-runtime-final-stage` (detection only)
2. `tally/gpu/cuda-version-mismatch` (detection only, resolver optional)
3. `tally/gpu/require-torch-cuda-arch-list`
4. `tally/gpu/hardcoded-cuda-path`
5. `tally/gpu/ld-library-path-overwrite`
6. `tally/gpu/model-download-in-build`

Why: these need more semantic awareness or tighter heuristics, but they are still static analysis.

### Wave 3: async resolver and AI/ACP work

Ship last:

1. ACP objective for `tally/gpu/prefer-runtime-final-stage`
2. ACP objective for `tally/gpu/prefer-uv-over-conda`
3. FixResolver for `tally/gpu/cuda-version-mismatch`
4. `tally/gpu/deprecated-cuda-image`
5. `tally/gpu/cuda-image-upgrade`
6. `tally/gpu/missing-nvidia-require`

Why: these either require network lookups or extending the current AI objective plumbing.

---

## 7. Consolidated Rule Catalog

## `tally/gpu/prefer-runtime-final-stage`

**Default severity:** `warning`

**Why**

GPU Dockerfiles frequently ship `nvidia/cuda:*devel*` as the final runtime image even when the final stage does not compile anything. That adds
`nvcc`, headers, development libraries, and often multiple gigabytes of avoidable payload.

This is the strongest rule in the namespace because:

- all three reports independently found it,
- the corpus shows it is common,
- it is easy to explain to users,
- and it is a natural fit for AI AutoFix.

**Detection**

Start with two heuristic buckets:

- High-confidence warning:
  - the final stage uses `nvidia/cuda:*devel*`, and
  - the final stage copies artifacts from earlier stages or otherwise looks like a runtime image.
- Lower-confidence warning:
  - the final stage uses `nvidia/cuda:*devel*`, and
  - the final stage has no obvious compile signal such as `nvcc`, `gcc`, `g++`, `make`, `cmake`, `ninja`, `build-essential`, or compiler/dev
    package installs.

**Representative examples**

- Bad:
  [ARoese/GravitationalRayTracing `Dockerfile-dev`](https://github.com/ARoese/GravitationalRayTracing/blob/4d094a5d6145278898f0a29f52a8d8e508344d66/Dockerfile-dev)
- Good patterns: [huggingface/text-generation-inference](https://github.com/huggingface/text-generation-inference),
  [google-research/corenet](https://github.com/google-research/corenet), [vllm-project/vllm](https://github.com/vllm-project/vllm)

**Fix strategy**

- Detection can ship as a normal heuristic rule.
- AI AutoFix is the real differentiator:
  - split build and runtime stages,
  - keep `devel` only where compilation happens,
  - move final stage to `runtime` or `base`,
  - preserve `CMD`, `ENTRYPOINT`, `USER`, `WORKDIR`, labels, and GPU-related runtime env.

This must be `FixUnsafe`.

### First implementation slice

For the **first warning-only implementation**, wider research should **not** be necessary. A narrow, shippable slice is:

1. **Find the final stage and gate on an obvious NVIDIA devel base**
   - Require `input.Semantic`.
   - Find the stage where `sem.StageInfo(i).IsLastStage == true`.
   - Require a real source stage (`strings.TrimSpace(stage.SourceCode) != ""`).
   - In v1, only fire when the final stage base name clearly matches `nvidia/cuda:*devel*`.
   - Defer broader NVIDIA image families (`nvcr.io/nvidia/*`) until after the first rule lands.

2. **Check for strong positive evidence that this is a shipped runtime stage**
   - If the final stage has any `COPY --from=...`, treat that as a strong signal that it is a runtime stage receiving artifacts from builders.
   - Otherwise keep going only if the final stage still looks GPU-relevant and shipped, not obviously like a dev shell.

3. **Suppress when the final stage shows obvious build-time needs**
   - Scan only the final stage commands for clear compile signals:
     - `nvcc`
     - `gcc`, `g++`
     - `make`, `cmake`, `ninja`
     - package installs such as `build-essential`
   - If such a signal is present, return no violation in MVP.
   - If absent, emit one warning on the final `FROM`.

That is enough to ship a useful first version with low false-positive risk.

### Concrete MVP rule shape

Recommended first file:

- `internal/rules/tally/gpu_prefer_runtime_final_stage.go`

Recommended first metadata:

- Code: `tally/gpu/prefer-runtime-final-stage`
- Severity: `warning`
- Category: `best-practices` or `performance`
- Default: enabled
- No fix attached in PR1

Recommended first message:

> Final stage uses an NVIDIA devel image without clear build-time needs; prefer a runtime image for the shipped stage and keep devel in builder
> stages.

### Things that are still under-specified for later PRs

These do **not** block the first rule, but they should not be improvised silently:

- Whether to include `nvcr.io/nvidia/*` and other NVIDIA image families in the same detector
- Whether notebook/devcontainer-style images should be auto-suppressed
- Whether the rule should use stage reachability beyond "last stage" once target-aware linting exists
- How the ACP objective should validate that runtime semantics were preserved

### Suggested first three PR steps

If I were implementing this rule, I would break it down like this:

1. **PR1:** warning-only rule for `nvidia/cuda:*devel*` final stage with final-stage compile-signal suppression
2. **PR2:** integration tests and fixture expansion, including multi-stage positive cases and no-fire cases
3. **PR3:** ACP objective for structural refactor once the detector has proven useful

That means the current document is good enough for the first implementation slice, but not yet specific enough for the ACP fix contract without an
additional implementation note.

---

## `tally/gpu/prefer-uv-over-conda`

**Default severity:** `info`

**Why**

This rule matters because the research did not just say "uv is trendy." It found a real subset of GPU Dockerfiles where:

- conda is being used mainly as a Python package installer,
- the image is already GPU/PyTorch-oriented,
- and the install flow could plausibly become uv plus an explicit PyTorch CUDA index.

That makes it both:

- a real developer-experience improvement, and
- an unusually marketable AI-assisted migration story for Tally.

**Detection**

Only fire on narrow, high-confidence cases:

1. The Dockerfile is clearly GPU/PyTorch-oriented.
2. It bootstraps and uses `conda`, `mamba`, or `micromamba`.
3. It uses conda primarily for package installation.
4. It does **not** use `conda env create`, `environment.yml`, multiple named envs, or heavy environment-activation scripting.

**Corpus support**

- 85 GPU Dockerfiles used `conda|mamba|micromamba install`
- 61 installed ML Python packages through conda
- 46 installed GPU Python stack packages through conda
- 79 matched the narrow "simple conda install" pattern that looks realistically migratable

**Representative examples**

- Likely fire: [Hsun1128/AICUP-2024 `dockerfile`](https://github.com/Hsun1128/AICUP-2024/blob/33c943c758bf84667b1f0dcbd893f794584c8255/dockerfile)
- Should not fire:
  [Preemo-Inc/text-generation-inference `Dockerfile`](https://github.com/Preemo-Inc/text-generation-inference/blob/972e9a7f7c111b58679248f82bebab9237adbbf2/Dockerfile)
- Should not fire:
  [BayraktarLab/cell2location `Dockerfile`](https://github.com/BayraktarLab/cell2location/blob/afb07fda7f89458e44a4afdf21c26aeb7251ebfa/Dockerfile)
- Supporting docs: [uv PyTorch integration](https://docs.astral.sh/uv/guides/integration/pytorch/)

**Fix strategy**

- Detection can ship immediately as a heuristic educational rule.
- The interesting fix is an AI migration:
  - infer Python dependencies,
  - map conda channel usage to PyPI versus PyTorch CUDA indexes,
  - generate `pyproject.toml` / `uv.lock` or a `uv pip` flow,
  - rewrite the Dockerfile accordingly.

This must be `FixUnsafe`.

---

## `tally/gpu/cuda-version-mismatch`

**Default severity:** `warning`

**Why**

Installing CUDA-specific framework wheels that do not line up with the base image's CUDA toolchain can lead to silent CPU fallback, build failures,
or runtime breakage.

**Detection**

- Parse the base image CUDA version from `FROM nvidia/cuda:X.Y...`.
- Parse CUDA wheel/index suffixes such as `cu118`, `cu121`, `cu128`, or TensorFlow equivalents.
- Flag only clear mismatches:
  - cross-major mismatches,
  - or wheel CUDA versions materially newer than the base image's toolkit.

Do **not** warn on every minor-version difference inside the same major when forward compatibility is expected.

**Examples found in research**

- `nvidia/cuda:12.6.2-*` base with `cu129` wheels
- `nvidia/cuda:11.8.0-*` base with `cu116` artifacts

**Fix strategy**

Use an async `FixResolver` that:

1. Parses the base image CUDA version.
2. Queries the PyTorch wheel index.
3. Selects the best compatible published `cu*` suffix.
4. Rewrites the `--index-url` or `--extra-index-url`.

This is a strong `FixSuggestion`, not a `FixSafe` edit.

---

## `tally/gpu/no-redundant-cuda-install`

**Default severity:** `warning`

**Why**

If a stage already starts from `nvidia/cuda:*`, reinstalling CUDA userspace packages through the OS package manager is usually redundant and can
introduce version drift.

**Detection**

For a stage that already inherits from `nvidia/cuda:*`, flag package-manager installs of CUDA-stack packages such as:

- `nvidia-cuda-toolkit`
- `cuda`
- `cuda-toolkit*`
- `cuda-runtime*`
- `cuda-libraries*`
- `cuda-compat*`
- `cuda-nvcc*`
- `libcudnn*`
- `tensorrt*`

**Representative examples**

- [birosjh/yolov3_dyhead `Dockerfile`](https://github.com/birosjh/yolov3_dyhead/blob/556595a7c12bf264ec08ab4a00a865312e9fbe2c/Dockerfile)

**Notes**

- Keep MVP scope narrow: package-manager installs only.
- Do not auto-fix by deleting packages blindly.
- Allow false-positive escape hatches for images intentionally carrying multiple CUDA versions side by side.

---

## `tally/gpu/no-container-runtime-in-image`

**Default severity:** `warning`

**Why**

Installing `nvidia-container-toolkit`, `nvidia-docker2`, or `libnvidia-container*` inside the image confuses host/runtime responsibilities. These
packages belong to the host-side NVIDIA Container Toolkit setup; they do not make an arbitrary image "GPU-enabled" by themselves.

**Detection**

Flag `RUN` instructions that install:

- `nvidia-container-toolkit`
- `nvidia-docker2`
- `libnvidia-container*`

via `apt`, `apt-get`, `yum`, `dnf`, `microdnf`, or `apk`.

**Representative example**

- [celesrenata/nixos-k3s-configs `Dockerfile.new3`](https://github.com/celesrenata/nixos-k3s-configs/blob/affc596951b210645b18b003352f076c84aa64a6/Blender/docker/Dockerfile.new3)

**Fix strategy**

Diagnostic only. The right fix is to remove the host-runtime bootstrap from the Dockerfile and configure the host or cluster runtime instead.

---

## `tally/gpu/no-buildtime-gpu-queries`

**Default severity:** `error`

**Why**

This catches real build failures, not style. GPU hardware is not available during a normal `docker build`, so build steps that query runtime GPU
state are broken by design.

**Detection**

Flag `RUN` instructions containing:

- `nvidia-smi`
- `torch.cuda.is_available()`
- `torch.cuda.device_count()`
- equivalent runtime hardware checks

Do not fire on `CMD` or `ENTRYPOINT`.

**Supporting guidance**

- [Hugging Face Docker Spaces GPU docs](https://huggingface.co/docs/hub/main/en/spaces-sdks-docker)

**Fix strategy**

Diagnostic only. The fix is architectural:

- move the check to runtime,
- or replace it with a build-time smoke test that does not require actual GPU hardware.

---

## `tally/gpu/no-hardcoded-visible-devices`

**Default severity:** `warning`

**Why**

GPU visibility is deployment policy. Hardcoding it inside the image makes the image less portable and can override orchestrator intent.

This rule deliberately merges two related patterns:

- `ENV NVIDIA_VISIBLE_DEVICES=...`
- `ENV CUDA_VISIBLE_DEVICES=...`

**Detection**

Flag:

- `ENV CUDA_VISIBLE_DEVICES=...` for non-empty values.
- `ENV NVIDIA_VISIBLE_DEVICES=<explicit index list or GPU UUID>`.
- `ENV NVIDIA_VISIBLE_DEVICES=all` when the base image already inherits from official CUDA images where `all` is already the default.

Do not flag `none`, `void`, empty, or unset forms in MVP.

**Representative example**

- [MayaVB/SRP-DNN-CP `Dockerfile`](https://github.com/MayaVB/SRP-DNN-CP/blob/1ebae8301520f4bb537cf304d3fe44853e538bbf/Dockerfile)

**Fix strategy**

- `FixSafe` only for the narrow redundant case: official CUDA base + `ENV NVIDIA_VISIBLE_DEVICES=all`.
- All other cases are suggestion-only because removing device policy can change deployment behavior.

---

## `tally/gpu/prefer-minimal-driver-capabilities`

**Default severity:** `info`

**Why**

`NVIDIA_DRIVER_CAPABILITIES=all` mounts more driver libraries and binaries than most ML/CUDA workloads need. A smaller capability set is better
from both least-privilege and compatibility standpoints.

**Detection**

Flag only the narrow case:

```dockerfile
ENV NVIDIA_DRIVER_CAPABILITIES=all
```

**Representative examples**

- Bad: [MayaVB/SRP-DNN-CP `Dockerfile`](https://github.com/MayaVB/SRP-DNN-CP/blob/1ebae8301520f4bb537cf304d3fe44853e538bbf/Dockerfile)
- Good minimal example:
  [Iamjunade/AIVA-Backend `Dockerfile`](https://github.com/Iamjunade/AIVA-Backend/blob/cacd7f26ca55be7c2c5ee4f590c445379229bd67/Dockerfile)

**Fix strategy**

Suggestion only. Some workloads genuinely need `graphics`, `video`, or `display`.

---

## `tally/gpu/require-torch-cuda-arch-list`

**Default severity:** `info`

**Why**

When CUDA extensions are built from source without `TORCH_CUDA_ARCH_LIST`, build time and artifact size can grow dramatically because compilation
targets many architectures by default.

**Detection**

Flag Dockerfiles that:

- build CUDA extensions from source, or
- install known CUDA-extension packages such as `flash-attn`, `xformers`, `torch-scatter`, `torch-sparse`, or `apex`,

without a prior `ARG` or `ENV TORCH_CUDA_ARCH_LIST`.

**Positive examples repeatedly mentioned**

- [huggingface/text-generation-inference](https://github.com/huggingface/text-generation-inference)
- [vllm-project/vllm](https://github.com/vllm-project/vllm)

**Fix strategy**

Suggestion only. The correct value is hardware- and distribution-specific.

---

## `tally/gpu/hardcoded-cuda-path`

**Default severity:** `info`

**Why**

Hardcoding `/usr/local/cuda-X.Y` instead of `/usr/local/cuda` makes upgrades brittle.

**Detection**

Flag `ENV CUDA_HOME=`, `ENV PATH=`, or `ENV LD_LIBRARY_PATH=` values containing `/usr/local/cuda-<major>.<minor>`.

**Notes**

- Start as suggestion-only.
- Only consider `FixSafe` after adding guardrails against deliberate side-by-side CUDA installs.

---

## `tally/gpu/ld-library-path-overwrite`

**Default severity:** `warning`

**Why**

Blindly replacing `LD_LIBRARY_PATH` can discard base-image library paths, including CUDA-provided paths already configured by the image.

**Detection**

Flag `ENV LD_LIBRARY_PATH=<value>` when `<value>` does not reference `$LD_LIBRARY_PATH` or `${LD_LIBRARY_PATH}`.

**Notes**

- Start as suggestion-only, not auto-fix.
- Intentional resets do exist, so the rule should explain why it fired rather than assuming the rewrite is always safe.

---

## `tally/gpu/deprecated-cuda-image`

**Default severity:** `warning`

**Why**

This is the right place to warn about obviously EOL CUDA image lines, but only once Tally carries an explicit support policy table.

**Detection**

Flag `nvidia/cuda:*` tags below a configurable minimum supported CUDA line.

**Notes**

- Keep thresholds configurable.
- Do not ship this rule until the support policy source is agreed and testable.

---

## `tally/gpu/cuda-image-upgrade`

**Default severity:** `info`

**Why**

Patch-level CUDA image staleness is real, and it is a good showcase for async resolver fixes, but it is lower priority than semantic GPU mistakes.

**Detection**

Parse `FROM nvidia/cuda:<version>-<variant>-<os>` and compare it against the latest patch available for the same series, variant, and OS.

**Fix strategy**

Use a resolver that queries Docker Hub tags and rewrites the `FROM` reference. This is a `FixSuggestion`, not a `FixSafe` edit.

---

## `tally/gpu/model-download-in-build`

**Default severity:** `info`

**Why**

Downloading large model artifacts in `RUN` bakes large mutable assets into layers, which is often a bad fit for modern model delivery and update
flows.

**Detection**

Look for:

- `huggingface-cli download` / `hf download`
- `wget` / `curl` / `git lfs` against known model-hosting domains
- large model file extensions such as `*.safetensors`, `*.gguf`, `*.pt`, `*.ckpt`, `*.bin`

**Notes**

- Diagnostic only.
- Lower priority because some teams do this intentionally for reproducibility.

---

## `tally/gpu/missing-nvidia-require`

**Default severity:** `info`

**Why**

`NVIDIA_REQUIRE_CUDA` can make driver incompatibility fail fast with a clearer error, which is useful in custom GPU images that do not inherit it
from official CUDA bases.

**Detection**

Only consider this for custom GPU images that do not already inherit the variable from official CUDA images.

**Notes**

- Low priority.
- Suggestion only.
- Do not ship before the rule can infer a trustworthy constraint value.

---

## 8. Proposals To Defer Or Keep Out Of `tally/gpu/*`

### Do not create GPU-specific versions of generic cache/no-cache rules

Keep these **out** of the GPU namespace:

- `pip-no-cache-dir`
- `apt-no-install-recommends`
- `prefer-buildkit-cache`

Reason: these are generic container-build hygiene rules. Tally already has `tally/prefer-package-cache-mounts`, and generic package-manager
guidance belongs in general Tally or Hadolint coverage, not in GPU-specific rule space.

### Defer `missing-nvidia-env`

Do **not** prioritize a rule that blindly inserts:

```dockerfile
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
```

Official NVIDIA docs do show this pattern for custom CUDA containers. However:

- `docker run --gpus ...` is also a supported runtime path,
- orchestrators often provide device selection externally,
- and a GPU namespace that simultaneously says "you are missing `NVIDIA_VISIBLE_DEVICES=all`" and "do not hardcode visible devices" will confuse
  users.

Revisit only as an explicit opt-in policy rule for custom non-NVIDIA CUDA bases.

### Defer `nccl-configuration`

This needs more than the Dockerfile alone. Reliable NCCL guidance depends on distributed-training context, framework usage, and version alignment that
the Dockerfile often does not encode.

### Defer `cleanup-static-libs`

This is weaker than `prefer-runtime-final-stage` and risks stepping on deliberate build/runtime choices. The image-shape problem is better captured
one level up by not shipping `devel` as the final runtime stage.

### Do not add overly broad GPU rules

Do not prioritize:

- `forbid-devel`
- `require-pinned-cuda-tag`

Reason: the meaningful problem is not "using `devel` at all" or "GPU images are not pinned." The meaningful problem is **shipping the wrong stage**
or **shipping stale/EOL GPU bases with actionable evidence**.

---

## 9. Implementation Feasibility In The Current Codebase

The current Tally architecture is already a good fit for most of this namespace.

### What is immediately feasible

- Stage-aware and instruction-aware static checks over `FROM`, `RUN`, and `ENV`
- Per-rule severities and docs in the existing `tally/*` style
- Normal `SuggestedFix` edits for narrow safe cases
- Async `FixResolver` work for rules that need network lookups

### What needs a small extension, not a new platform

Tally already has real ACP integration and async fix plumbing. However, the current AI resolver is still effectively single-objective and centered on
`tally/prefer-multi-stage-build`.

That means:

- `tally/gpu/prefer-runtime-final-stage` is feasible as the **next ACP objective** with modest extension work.
- `tally/gpu/prefer-uv-over-conda` is also feasible, but only after the AI objective plumbing can route multiple rule-specific objectives and
  validation strategies.

This is incremental engineering work, not foundational risk.

### Suggested integration fixtures

- `gpu/no-container-runtime-in-image`
- `gpu/no-redundant-cuda-install`
- `gpu/no-buildtime-gpu-queries`
- `gpu/no-hardcoded-visible-devices`
- `gpu/prefer-minimal-driver-capabilities`
- `gpu/prefer-runtime-final-stage-single-stage`
- `gpu/prefer-runtime-final-stage-multistage`
- `gpu/cuda-version-mismatch`
- `gpu/require-torch-cuda-arch-list`
- `gpu/prefer-uv-over-conda-simple`
- `gpu/prefer-uv-over-conda-env-yml-no-fire`

---

## 10. Recommendation

Adopt `tally/gpu/*` now, but keep it disciplined.

The namespace should be anchored by two flagship rules:

1. `tally/gpu/prefer-runtime-final-stage`
2. `tally/gpu/prefer-uv-over-conda`

They belong at the top because all three research efforts independently converged on them, and because they are the best GPU-specific fit for
Tally's AI/ACP story.

The first implementation wave should still start with the cheapest high-confidence static rules:

- `tally/gpu/no-container-runtime-in-image`
- `tally/gpu/no-redundant-cuda-install`
- `tally/gpu/no-buildtime-gpu-queries`
- `tally/gpu/no-hardcoded-visible-devices`
- `tally/gpu/prefer-minimal-driver-capabilities`

Then add stage-aware, version-aware, and AI-assisted rules in later waves.

The main discipline point is simple: **do not dilute `tally/gpu/*` with generic package-manager hygiene**. Keep the namespace focused on GPU-specific
mistakes, stale CUDA alignment, runtime policy, and high-value structural improvements.

---

## References

- [Docker Desktop GPU support](https://docs.docker.com/desktop/features/gpu/)
- [NVIDIA Container Toolkit: Docker](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html)
- [Hugging Face Docker Spaces GPU docs](https://huggingface.co/docs/hub/main/en/spaces-sdks-docker)
- [uv: PyTorch integration](https://docs.astral.sh/uv/guides/integration/pytorch/)
- [NVIDIA CUDA image tags](https://hub.docker.com/r/nvidia/cuda/)
