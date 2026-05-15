# BuildKit test case 1: Constant platform → should warn
FROM --platform=linux/amd64 scratch

# BuildKit test case 2: Stage name contains architecture → should NOT warn
FROM --platform=linux/amd64 scratch AS my_amd64_stage

# BuildKit test case 3: Stage name matches OS → should NOT warn
FROM --platform=linux/amd64 scratch AS linux

# Variable platform → should NOT warn
FROM --platform=$BUILDPLATFORM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS builder
RUN echo "cross-compile here"

# Another constant platform → should warn
FROM --platform=linux/arm64 alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
RUN echo "arm64 only"

# Platform with variant → should warn
FROM --platform=linux/arm/v7 debian:bookworm-slim@sha256:67b30a61dc87758f0caf819646104f29ecbda97d920aaf5edc834128ac8493d3

# Hadolint DL3029 case 1: Single-component constant platform → should warn
FROM --platform=linux debian:bookworm@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688

# Variable with default expansion → should NOT warn
FROM --platform=${BUILDPLATFORM:-} alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

# $TARGETPLATFORM → should NOT warn
FROM --platform=$TARGETPLATFORM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
