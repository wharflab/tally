# syntax=docker/dockerfile:1

# Stage 1: Cross-major mismatch — should fire (12.1 base with cu118 wheels)
# Real-world pattern: uzh-rpg/rl_vo, akraradets/soil_nutrient
FROM nvidia/cuda:12.1.0-devel-ubuntu20.04 AS cross-major
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch torchvision

# Stage 2: --extra-index-url mismatch — should fire (12.4 base with cu118)
# Real-world pattern: wladradchenko/wunjo.wladradchenko.ru
FROM nvidia/cuda:12.4.1-cudnn-devel-ubuntu22.04 AS extra-index-mismatch
RUN pip install --extra-index-url https://download.pytorch.org/whl/cu118 xformers

# Stage 3: Package suffix mismatch — should fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS pkg-suffix-mismatch
RUN pip3 install torch==2.0.0+cu118

# Stage 4: uv --torch-backend mismatch — should fire
FROM nvidia/cuda:12.4.0-runtime-ubuntu22.04 AS uv-torch-backend
RUN uv pip install --torch-backend cu118 torch

# Stage 5: conda pytorch-cuda mismatch — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS conda-mismatch
RUN conda install -y pytorch pytorch-cuda=11.8 -c pytorch -c nvidia

# Stage 6: Correct match — should NOT fire
# Real-world pattern: Sxela/WarpFusion
FROM nvidia/cuda:11.8.0-base-ubuntu22.04 AS correct-match
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch

# Stage 7: Forward-compatible — should NOT fire
FROM nvidia/cuda:12.6.0-runtime-ubuntu22.04 AS forward-compat
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch

# Stage 8: Non-CUDA base — should NOT fire
FROM ubuntu:22.04 AS non-cuda
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch

# Stage 9: conda correct match — should NOT fire
FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04 AS conda-match
RUN conda install -y pytorch pytorch-cuda=12.1 -c pytorch -c nvidia
