# Test: tally/platform-mismatch does NOT fire for automatic build platform args.
# $BUILDPLATFORM and $TARGETPLATFORM are dynamic at build time and cannot be
# validated statically, so the rule skips them entirely.
FROM --platform=$BUILDPLATFORM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8 AS builder
RUN echo "build"

FROM --platform=$TARGETPLATFORM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
RUN echo "run"
