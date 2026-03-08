# Test: tally/platform-mismatch fires when --platform is explicit and mismatches.
# The mock python:3.12 is linux/arm64-only, but we request linux/amd64.
FROM --platform=linux/amd64 python:3.12
RUN echo "hello"
