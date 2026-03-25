# syntax=docker/dockerfile:1

# Stage 1: devel base reinstalling cuda-toolkit via apt — should fire (devel includes toolkit)
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS redundant-devel
RUN apt-get update && \
    apt-get install -y \
    cuda-toolkit \
    python3

# Stage 2: cudnn-devel base reinstalling libcudnn — should fire (cudnn tag includes cuDNN)
FROM nvidia/cuda:12.2.0-cudnn-devel-ubuntu22.04 AS redundant-cudnn
RUN apt-get update && apt-get install -y libcudnn8

# Stage 3: runtime base with legitimate cuda-toolkit install — should NOT fire
# (runtime does not include devel-only packages like cuda-toolkit)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS legitimate-toolkit
RUN apt-get update && apt-get install -y cuda-toolkit

# Stage 4: runtime base with cuda-runtime reinstall — should fire (runtime includes this)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS redundant-runtime
RUN apt-get update && apt-get install -y cuda-runtime-12-2

# Stage 5: Ubuntu base installing CUDA packages — should NOT fire (not nvidia/cuda base)
FROM ubuntu:22.04 AS ubuntu-cuda
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
