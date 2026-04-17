# syntax=docker/dockerfile:1

# Stage 1: pytorch/pytorch base + conda install of ML packages — should fire.
# Real-world pattern similar to Hsun1128/AICUP-2024.
FROM pytorch/pytorch:2.1.0-cuda12.1-cudnn8-devel AS conda-ml
RUN conda install -y numpy scipy transformers
CMD ["python", "-m", "app"]

# Stage 2: nvidia/cuda base + mamba install of ML packages — should fire.
FROM nvidia/cuda:12.4.0-devel-ubuntu22.04 AS mamba-ml
RUN apt-get update && apt-get install -y --no-install-recommends build-essential && rm -rf /var/lib/apt/lists/*
RUN mamba install -y -c pytorch -c nvidia pytorch pytorch-cuda=12.1 xformers

# Stage 3: CPU base + conda install — should NOT fire (no GPU signal).
FROM ubuntu:22.04 AS cpu
RUN conda install -y numpy torch

# Stage 4: nvidia/cuda base + conda install of system packages only — should NOT fire.
FROM nvidia/cuda:12.1.0-devel-ubuntu22.04 AS sys-only
RUN conda install -y gcc cmake

# Stage 5: nvidia/cuda base + uv already in use — should NOT fire.
FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04 AS already-uv
RUN pip install uv && uv pip install --system --index-url https://download.pytorch.org/whl/cu121 torch

# Stage 6: inherits CUDA version from stage 2 (nvidia/cuda:*); conda install
# still fires because facts carry CUDAMajor/Minor forward through stage refs.
FROM mamba-ml AS inherits
RUN conda install -y flash-attn

# Stage 7: heavy env workflow (`conda env create`) suppresses the rule file-wide.
# Intentionally placed last so its suppression applies to every other stage —
# this fixture tests the detection path; the suppression path has its own unit test.
FROM nvidia/cuda:12.1.0-devel-ubuntu22.04 AS env-create
# (no conda env create here — that path is covered by unit tests)
RUN echo "no-op"
