# Test: tally/platform-mismatch does NOT fire for automatic build platform args.
# $BUILDPLATFORM and $TARGETPLATFORM are dynamic at build time and cannot be
# validated statically, so the rule skips them entirely.
FROM --platform=$BUILDPLATFORM python:3.12 AS builder
RUN echo "build"

FROM --platform=$TARGETPLATFORM python:3.12
RUN echo "run"
