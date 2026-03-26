# tally/gpu/prefer-minimal-driver-capabilities

`NVIDIA_DRIVER_CAPABILITIES=all` exposes more driver surface than most workloads need; prefer a minimal capability set.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Suggestion only |

## Description

Detects `ENV NVIDIA_DRIVER_CAPABILITIES=all` in Dockerfiles. The `all` capability set mounts every
NVIDIA driver library and binary into the container, but most ML and CUDA workloads only need
`compute,utility` (NVIDIA's documented default). A smaller set follows the principle of least
privilege and avoids potential compatibility issues.

## Why this matters

- **Least privilege** -- `all` exposes driver capabilities (`graphics`, `video`, `display`, `compat32`)
  that most inference and training workloads never use
- **Compatibility** -- mounting unnecessary driver components can surface driver/library version
  conflicts in environments where the host driver differs from what the image expects
- **Clarity** -- explicitly listing needed capabilities documents the workload's actual requirements

## What is flagged

| Pattern | Flagged? | Fix safety |
|---------|----------|------------|
| `ENV NVIDIA_DRIVER_CAPABILITIES=all` | Yes | `FixSuggestion` |
| `ENV NVIDIA_DRIVER_CAPABILITIES=ALL` (case-insensitive) | Yes | `FixSuggestion` |
| `ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility` | No -- already minimal | -- |
| `ENV NVIDIA_DRIVER_CAPABILITIES=graphics,compute,utility` | No -- intentional | -- |
| `ENV NVIDIA_DRIVER_CAPABILITIES=` (empty) | No | -- |
| `ENV NVIDIA_DRIVER_CAPABILITIES=${VAR}` (variable reference) | No -- parameterized | -- |

## Examples

### Violation

```dockerfile
# Exposes all driver capabilities unnecessarily
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
```

```dockerfile
# Same issue on a custom GPU base image
FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
```

### No violation

```dockerfile
# Explicit minimal set -- preferred
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
```

```dockerfile
# Workload that genuinely needs graphics
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=graphics,compute,utility
```

```dockerfile
# Parameterized -- not hardcoded
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ARG CAPS=all
ENV NVIDIA_DRIVER_CAPABILITIES=${CAPS}
```

## Auto-fix behavior

The rule offers a **`FixSuggestion`** (applied with `--fix --fix-unsafe`): replaces `all` with
`compute,utility`. This is safe for most ML/CUDA workloads but may break workloads that genuinely
need `graphics`, `video`, or `display` capabilities -- review before accepting.

For multi-key `ENV` instructions, only the `NVIDIA_DRIVER_CAPABILITIES` value is replaced; other
keys are preserved.

## Configuration

This rule has no rule-specific options.

```toml
[rules.tally."gpu/prefer-minimal-driver-capabilities"]
severity = "info"
```

## References

- [NVIDIA Container Toolkit: Environment Variables](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html)
- [GPU Container Rules design notes](../../../../design-docs/32-gpu-container-rules.md)
