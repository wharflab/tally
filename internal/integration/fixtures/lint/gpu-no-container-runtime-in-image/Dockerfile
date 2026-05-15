# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: Ubuntu base installing nvidia-container-toolkit via apt — should fire
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS toolkit-install
RUN apt-get update && \
    apt-get install -y \
    nvidia-container-toolkit \
    curl

# Stage 2: CentOS base installing nvidia-docker2 via yum — should fire
FROM centos:7@sha256:be65f488b7764ad3638f236b7b515b3678369a5124c47b8d32916d6487418ea4 AS docker2-install
RUN yum install -y nvidia-docker2

# Stage 3: NVIDIA CUDA base with legitimate packages — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS gpu-app
RUN apt-get update && apt-get install -y python3 python3-pip

# Stage 4: Plain Linux stage with no GPU packages — should NOT fire
FROM alpine:3.18@sha256:de0eb0b3f2a47ba1eb89389859a9bd88b28e82f5826b6969ad604979713c2d4f AS builder
RUN apk add --no-cache curl wget
