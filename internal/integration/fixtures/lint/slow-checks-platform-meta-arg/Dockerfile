# Test: platform resolved via declared meta ARG triggers async mismatch.
# The ARG PLATFORM defaults to linux/amd64, but python:3.12 in the mock
# registry is linux/arm64 only. resolvePlatformExpr expands $PLATFORM
# from model.metaArgs, and the async check detects the mismatch.
ARG PLATFORM=linux/amd64
FROM --platform=$PLATFORM python:3.12
RUN echo "meta arg platform resolution"
