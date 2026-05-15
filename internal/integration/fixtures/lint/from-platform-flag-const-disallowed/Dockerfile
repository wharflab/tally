# BuildKit test case 1: Constant platform → should warn
FROM --platform=linux/amd64 scratch

# BuildKit test case 2: Stage name contains architecture → should NOT warn
FROM --platform=linux/amd64 scratch AS my_amd64_stage

# BuildKit test case 3: Stage name matches OS → should NOT warn
FROM --platform=linux/amd64 scratch AS linux

# Variable platform → should NOT warn
FROM --platform=$BUILDPLATFORM alpine:3.21 AS builder
RUN echo "cross-compile here"

# Another constant platform → should warn
FROM --platform=linux/arm64 alpine:3.21
RUN echo "arm64 only"

# Platform with variant → should warn
FROM --platform=linux/arm/v7 debian:bookworm-slim

# Hadolint DL3029 case 1: Single-component constant platform → should warn
FROM --platform=linux debian:bookworm-20260505@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688

# Variable with default expansion → should NOT warn
FROM --platform=${BUILDPLATFORM:-} alpine:3.21

# $TARGETPLATFORM → should NOT warn
FROM --platform=$TARGETPLATFORM alpine:3.21
