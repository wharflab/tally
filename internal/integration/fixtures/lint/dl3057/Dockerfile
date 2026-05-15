# Test case for DL3057: HEALTHCHECK instruction missing
FROM alpine:3.18 AS builder
RUN echo "building"

# This stage has no HEALTHCHECK and is not an ancestor of one
FROM debian:bookworm-20260505@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688
RUN echo "running"
