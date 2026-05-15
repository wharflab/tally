# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: pytorch/pytorch base + conda install of ML packages — should fire.
# Real-world pattern similar to Hsun1128/AICUP-2024.
FROM pytorch/pytorch:2.1.0-cuda12.1-cudnn8-devel@sha256:fe174e1e257d29976c99ebe9832d9bb20bd9706ea8eff1482cc9af261998c48d AS conda-ml
RUN conda install -y numpy scipy transformers
CMD ["python", "-m", "app"]

# Stage 2: nvidia/cuda base + mamba install of ML packages — should fire.
FROM nvidia/cuda:12.4.0-devel-ubuntu22.04@sha256:28326fefd36d43ecf82447e9ca1175db639b931e234019b2807f35a0d1551656 AS mamba-ml
RUN apt-get update && apt-get install -y --no-install-recommends build-essential && rm -rf /var/lib/apt/lists/*
RUN mamba install -y -c pytorch -c nvidia pytorch pytorch-cuda=12.1 xformers

# Stage 3: CPU base + conda install — should NOT fire (no GPU signal).
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS cpu
RUN conda install -y numpy torch

# Stage 4: nvidia/cuda base + conda install of system packages only — should NOT fire.
FROM nvidia/cuda:12.1.0-devel-ubuntu22.04@sha256:e3a8f7b933e77ecee74731198a2a5483e965b585cea2660675cf4bb152237e9b AS sys-only
RUN conda install -y gcc cmake

# Stage 5: nvidia/cuda base + uv already in use — should NOT fire.
FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04@sha256:402700b179eb764da6d60d99fe106aa16c36874f7d7fb3e122251ff6aea8b2f7 AS already-uv
RUN pip install uv && uv pip install --system --index-url https://download.pytorch.org/whl/cu121 torch

# Stage 6: inherits CUDA version from stage 2 (nvidia/cuda:*); conda install
# still fires because facts carry CUDAMajor/Minor forward through stage refs.
FROM mamba-ml AS inherits
RUN conda install -y flash-attn

# Stage 7: heavy env workflow (`conda env create`) suppresses the rule file-wide.
# Intentionally placed last so its suppression applies to every other stage —
# this fixture tests the detection path; the suppression path has its own unit test.
FROM nvidia/cuda:12.1.0-devel-ubuntu22.04@sha256:e3a8f7b933e77ecee74731198a2a5483e965b585cea2660675cf4bb152237e9b AS env-create
# (no conda env create here — that path is covered by unit tests)
RUN echo "no-op"
