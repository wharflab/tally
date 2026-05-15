# Test: platform resolved via declared meta ARG triggers async mismatch.
# The ARG PLATFORM defaults to linux/amd64, but python:3.12 in the mock
# registry is linux/arm64 only. resolvePlatformExpr expands $PLATFORM
# from model.metaArgs, and the async check detects the mismatch.
ARG PLATFORM=linux/amd64
FROM --platform=$PLATFORM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
RUN echo "meta arg platform resolution"
