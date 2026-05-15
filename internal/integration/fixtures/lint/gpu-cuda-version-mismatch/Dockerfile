# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: Cross-major mismatch — should fire (12.1 base with cu118 wheels)
# Real-world pattern: uzh-rpg/rl_vo, akraradets/soil_nutrient
FROM nvidia/cuda:12.1.0-devel-ubuntu20.04@sha256:0678daf5d9f3800c714df6c3983d31065152510591efe99c53348cbe4b393f6a AS cross-major
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch torchvision

# Stage 2: --extra-index-url mismatch — should fire (12.4 base with cu118)
# Real-world pattern: wladradchenko/wunjo.wladradchenko.ru
FROM nvidia/cuda:12.4.1-cudnn-devel-ubuntu22.04@sha256:622e78a1d02c0f90ed900e3985d6c975d8e2dc9ee5e61643aed587dcf9129f42 AS extra-index-mismatch
RUN pip install --extra-index-url https://download.pytorch.org/whl/cu118 xformers

# Stage 3: Package suffix mismatch — should fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS pkg-suffix-mismatch
RUN pip3 install torch==2.0.0+cu118

# Stage 4: uv --torch-backend mismatch — should fire
FROM nvidia/cuda:12.4.0-runtime-ubuntu22.04@sha256:af8bd179ed3bf69d4b63b19a763662a6141f0f62ef099283f68d0b14b4bab0e3 AS uv-torch-backend
RUN uv pip install --torch-backend cu118 torch

# Stage 5: conda pytorch-cuda mismatch — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS conda-mismatch
RUN conda install -y pytorch pytorch-cuda=11.8 -c pytorch -c nvidia

# Stage 6: Correct match — should NOT fire
# Real-world pattern: Sxela/WarpFusion
FROM nvidia/cuda:11.8.0-base-ubuntu22.04@sha256:f895871972c1c91eb6a896eee68468f40289395a1e58c492e1be7929d0f8703b AS correct-match
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch

# Stage 7: Forward-compatible — should NOT fire
FROM nvidia/cuda:12.6.0-runtime-ubuntu22.04@sha256:22fc009e5cea0b8b91d94c99fdd419d2366810b5ea835e47b8343bc15800c186 AS forward-compat
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch

# Stage 8: Non-CUDA base — should NOT fire
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS non-cuda
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch

# Stage 9: conda correct match — should NOT fire
FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04@sha256:402700b179eb764da6d60d99fe106aa16c36874f7d7fb3e122251ff6aea8b2f7 AS conda-match
RUN conda install -y pytorch pytorch-cuda=12.1 -c pytorch -c nvidia
