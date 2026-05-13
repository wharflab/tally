# Test: platform mismatch detection via async check.
# The base image is linux/arm64-only, but we request linux/amd64 (host default).
FROM --platform=linux/amd64 python:3.12
RUN echo "hello"
