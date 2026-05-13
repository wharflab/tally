# syntax=docker/dockerfile:1

# Stage 1: Ubuntu base installing nvidia-container-toolkit via apt — should fire
FROM ubuntu:22.04 AS toolkit-install
RUN apt-get update && \
    apt-get install -y \
    nvidia-container-toolkit \
    curl

# Stage 2: CentOS base installing nvidia-docker2 via yum — should fire
FROM centos:7 AS docker2-install
RUN yum install -y nvidia-docker2

# Stage 3: NVIDIA CUDA base with legitimate packages — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS gpu-app
RUN apt-get update && apt-get install -y python3 python3-pip

# Stage 4: Plain Linux stage with no GPU packages — should NOT fire
FROM alpine:3.18 AS builder
RUN apk add --no-cache curl wget
