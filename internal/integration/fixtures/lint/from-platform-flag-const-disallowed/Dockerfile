# BuildKit test case 1: Constant platform → should warn
FROM --platform=linux/amd64 scratch

# BuildKit test case 2: Stage name contains architecture → should NOT warn
FROM --platform=linux/amd64 scratch AS my_amd64_stage

# BuildKit test case 3: Stage name matches OS → should NOT warn
FROM --platform=linux/amd64 scratch AS linux

# Variable platform → should NOT warn
FROM --platform=$BUILDPLATFORM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS builder
RUN echo "cross-compile here"

# Another constant platform → should warn
FROM --platform=linux/arm64 alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN echo "arm64 only"

# Platform with variant → should warn
FROM --platform=linux/arm/v7 debian:bookworm-slim

# Hadolint DL3029 case 1: Single-component constant platform → should warn
FROM --platform=linux debian:bookworm

# Variable with default expansion → should NOT warn
FROM --platform=${BUILDPLATFORM:-} alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# $TARGETPLATFORM → should NOT warn
FROM --platform=$TARGETPLATFORM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
