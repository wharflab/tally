# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: Redundant NVIDIA_VISIBLE_DEVICES=all on nvidia/cuda base — violation (FixSafe)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS redundant
ENV NVIDIA_VISIBLE_DEVICES=all

# Stage 2: Hardcoded device index — violation (FixSuggestion)
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS hardcoded-index
ENV NVIDIA_VISIBLE_DEVICES=0,1

# Stage 3: Hardcoded CUDA_VISIBLE_DEVICES — violation (FixSuggestion)
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS cuda-vis
ENV CUDA_VISIBLE_DEVICES=0

# Stage 4: Hardcoded GPU UUID — violation (FixSuggestion)
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS gpu-uuid
ENV NVIDIA_VISIBLE_DEVICES=GPU-aaaa-bbbb-cccc-dddd-eeee-ffffffffffff

# Stage 5: NVIDIA_VISIBLE_DEVICES=all on non-CUDA base — NO violation (intentional)
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS custom-gpu
ENV NVIDIA_VISIBLE_DEVICES=all

# Stage 6: Disable signal — NO violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS disabled
ENV NVIDIA_VISIBLE_DEVICES=none

# Stage 7: Variable reference — NO violation
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS parameterized
ARG GPU_DEVICES=all
ENV NVIDIA_VISIBLE_DEVICES=${GPU_DEVICES}

# Stage 8: Multi-key ENV with flagged key — violation (FixSafe, partial removal)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS multi-key
ENV NVIDIA_VISIBLE_DEVICES=all CUDA_HOME=/usr/local/cuda
