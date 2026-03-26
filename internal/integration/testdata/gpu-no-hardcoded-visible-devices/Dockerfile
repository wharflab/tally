# syntax=docker/dockerfile:1

# Stage 1: Redundant NVIDIA_VISIBLE_DEVICES=all on nvidia/cuda base — violation (FixSafe)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS redundant
ENV NVIDIA_VISIBLE_DEVICES=all

# Stage 2: Hardcoded device index — violation (FixSuggestion)
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS hardcoded-index
ENV NVIDIA_VISIBLE_DEVICES=0,1

# Stage 3: Hardcoded CUDA_VISIBLE_DEVICES — violation (FixSuggestion)
FROM ubuntu:22.04 AS cuda-vis
ENV CUDA_VISIBLE_DEVICES=0

# Stage 4: Hardcoded GPU UUID — violation (FixSuggestion)
FROM ubuntu:22.04 AS gpu-uuid
ENV NVIDIA_VISIBLE_DEVICES=GPU-aaaa-bbbb-cccc-dddd-eeee-ffffffffffff

# Stage 5: NVIDIA_VISIBLE_DEVICES=all on non-CUDA base — NO violation (intentional)
FROM ubuntu:22.04 AS custom-gpu
ENV NVIDIA_VISIBLE_DEVICES=all

# Stage 6: Disable signal — NO violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS disabled
ENV NVIDIA_VISIBLE_DEVICES=none

# Stage 7: Variable reference — NO violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS parameterized
ARG GPU_DEVICES=all
ENV NVIDIA_VISIBLE_DEVICES=${GPU_DEVICES}

# Stage 8: Multi-key ENV with flagged key — violation (FixSafe, partial removal)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS multi-key
ENV NVIDIA_VISIBLE_DEVICES=all CUDA_HOME=/usr/local/cuda
