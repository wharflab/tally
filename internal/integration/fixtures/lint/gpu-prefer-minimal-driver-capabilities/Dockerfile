# syntax=docker/dockerfile:1

# Stage 1: NVIDIA_DRIVER_CAPABILITIES=all — violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS broad-caps
ENV NVIDIA_DRIVER_CAPABILITIES=all
RUN echo "broad capabilities"

# Stage 2: Minimal capabilities — NO violation
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS minimal-caps
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
RUN echo "minimal capabilities"

# Stage 3: all on non-CUDA base — still a violation (Class 1 rule, no pre-gate)
FROM ubuntu:22.04 AS custom-gpu
ENV NVIDIA_DRIVER_CAPABILITIES=all
RUN echo "custom gpu image"

# Stage 4: Variable reference — NO violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS parameterized
ARG DRIVER_CAPS=all
ENV NVIDIA_DRIVER_CAPABILITIES=${DRIVER_CAPS}
RUN echo "parameterized"

# Stage 5: Multi-key ENV with flagged key — violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS multi-key
ENV NVIDIA_DRIVER_CAPABILITIES=all CUDA_HOME=/usr/local/cuda
RUN echo "multi-key env"

# Stage 6: Empty value — NO violation
FROM ubuntu:22.04 AS empty-caps
ENV NVIDIA_DRIVER_CAPABILITIES=
RUN echo "empty capabilities"
