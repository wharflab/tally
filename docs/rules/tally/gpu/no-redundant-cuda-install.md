# tally/gpu/no-redundant-cuda-install

CUDA packages are already provided by the nvidia/cuda base image.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | No |

## Description

Detects `RUN` instructions that install CUDA userspace packages via a package manager
(`apt`, `apt-get`, `yum`, `dnf`, `microdnf`, `apk`) in stages that already inherit from
`nvidia/cuda:*`.

The `nvidia/cuda` base images from NVIDIA already include the CUDA toolkit, runtime
libraries, cuDNN, and other CUDA userspace components appropriate for the image variant
(`base`, `runtime`, or `devel`). Reinstalling these packages through the OS package
manager is usually redundant and can introduce version drift between the base image's
CUDA stack and the newly installed packages.

## Why this matters

- **Redundant work** -- the base image already provides the CUDA stack for the selected variant
- **Version drift** -- the package manager may install a different CUDA version than the one baked into the base image, causing subtle
  incompatibilities
- **Image bloat** -- duplicate CUDA libraries waste space in the image layers
- **Maintenance burden** -- two sources of truth for the CUDA version make upgrades harder

## Examples

### Violation

```dockerfile
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
```

```dockerfile
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y libcudnn8 tensorrt
```

```dockerfile
FROM nvidia/cuda:12.2.0-runtime-centos7
RUN yum install -y cuda-runtime-12-2
```

### No violation

```dockerfile
# nvidia/cuda base with application packages only
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y python3 python3-pip
```

```dockerfile
# Non-nvidia/cuda base -- intentional CUDA install is not flagged
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
```

```dockerfile
# nvcr.io images are not nvidia/cuda -- not flagged
FROM nvcr.io/nvidia/pytorch:23.10-py3
RUN apt-get update && apt-get install -y cuda-toolkit
```

## Matched packages

| Package | Match type |
|---------|-----------|
| `nvidia-cuda-toolkit` | Exact |
| `cuda` | Exact |
| `cuda-toolkit` | Exact |
| `cuda-runtime` | Exact |
| `cuda-nvcc` | Exact |
| `cuda-toolkit-*` | Prefix |
| `cuda-runtime-*` | Prefix |
| `cuda-libraries-*` | Prefix |
| `cuda-compat-*` | Prefix |
| `cuda-nvcc-*` | Prefix |
| `libcudnn*` | Prefix |
| `tensorrt*` | Prefix |

## Applicability

This rule only fires on stages where the base image is `nvidia/cuda:*` (or `docker.io/nvidia/cuda:*`).
It does **not** fire on:

- Stages with a non-NVIDIA base image (e.g., `ubuntu:22.04`)
- Stages using other NVIDIA images (e.g., `nvcr.io/nvidia/pytorch:*`, `nvidia/cudagl:*`)
- Stages that reference another build stage (`FROM builder`)

## Configuration

This rule has no rule-specific options.

```toml
[rules.tally."gpu/no-redundant-cuda-install"]
severity = "warning"
```

## References

- [NVIDIA CUDA Docker Hub](https://hub.docker.com/r/nvidia/cuda/)
- [NVIDIA CUDA image variants](https://gitlab.com/nvidia/container-images/cuda/blob/master/doc/supported-tags.md)
- [GPU Container Rules design notes](../../../../design-docs/32-gpu-container-rules.md)
