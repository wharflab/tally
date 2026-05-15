# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: devel base reinstalling cuda-toolkit via apt — should fire (devel includes toolkit)
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS redundant-devel
RUN apt-get update && \
    apt-get install -y \
    cuda-toolkit \
    python3

# Stage 2: cudnn-devel base reinstalling libcudnn — should fire (cudnn tag includes cuDNN)
FROM nvidia/cuda:12.2.0-cudnn-devel-ubuntu22.04 AS redundant-cudnn
RUN apt-get update && apt-get install -y libcudnn8

# Stage 3: runtime base with legitimate cuda-toolkit install — should NOT fire
# (runtime does not include devel-only packages like cuda-toolkit)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS legitimate-toolkit
RUN apt-get update && apt-get install -y cuda-toolkit

# Stage 4: runtime base with cuda-runtime reinstall — should fire (runtime includes this)
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS redundant-runtime
RUN apt-get update && apt-get install -y cuda-runtime-12-2

# Stage 5: Ubuntu base installing CUDA packages — should NOT fire (not nvidia/cuda base)
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS ubuntu-cuda
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
