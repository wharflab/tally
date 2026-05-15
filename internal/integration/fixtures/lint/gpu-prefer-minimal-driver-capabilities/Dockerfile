# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: NVIDIA_DRIVER_CAPABILITIES=all — violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS broad-caps
ENV NVIDIA_DRIVER_CAPABILITIES=all
RUN echo "broad capabilities"

# Stage 2: Minimal capabilities — NO violation
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS minimal-caps
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
RUN echo "minimal capabilities"

# Stage 3: all on non-CUDA base — still a violation (Class 1 rule, no pre-gate)
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS custom-gpu
ENV NVIDIA_DRIVER_CAPABILITIES=all
RUN echo "custom gpu image"

# Stage 4: Variable reference — NO violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS parameterized
ARG DRIVER_CAPS=all
ENV NVIDIA_DRIVER_CAPABILITIES=${DRIVER_CAPS}
RUN echo "parameterized"

# Stage 5: Multi-key ENV with flagged key — violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS multi-key
ENV NVIDIA_DRIVER_CAPABILITIES=all CUDA_HOME=/usr/local/cuda
RUN echo "multi-key env"

# Stage 6: Empty value — NO violation
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS empty-caps
ENV NVIDIA_DRIVER_CAPABILITIES=
RUN echo "empty capabilities"
