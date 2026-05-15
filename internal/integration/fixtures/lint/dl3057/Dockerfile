# Test case for DL3057: HEALTHCHECK instruction missing
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS builder
RUN echo "building"

# This stage has no HEALTHCHECK and is not an ancestor of one
FROM debian:bookworm
RUN echo "running"
