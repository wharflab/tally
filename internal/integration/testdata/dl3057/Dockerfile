# Test case for DL3057: HEALTHCHECK instruction missing
FROM alpine:3.18 AS builder
RUN echo "building"

# This stage has no HEALTHCHECK and is not an ancestor of one
FROM debian:bookworm
RUN echo "running"
