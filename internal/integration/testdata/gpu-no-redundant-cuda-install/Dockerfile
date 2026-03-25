# syntax=docker/dockerfile:1

# Stage 1: nvidia/cuda base reinstalling CUDA toolkit via apt — should fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS redundant-install
RUN apt-get update && \
    apt-get install -y \
    cuda-toolkit \
    python3

# Stage 2: nvidia/cuda base reinstalling libcudnn and tensorrt — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS redundant-prefix
RUN apt-get update && apt-get install -y libcudnn8 tensorrt

# Stage 3: nvidia/cuda base with legitimate packages only — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS clean-gpu-app
RUN apt-get update && apt-get install -y python3 python3-pip curl

# Stage 4: Ubuntu base installing CUDA packages — should NOT fire (not nvidia/cuda base)
FROM ubuntu:22.04 AS ubuntu-cuda
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
