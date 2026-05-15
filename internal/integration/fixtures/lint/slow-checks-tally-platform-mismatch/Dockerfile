# Test: tally/platform-mismatch fires when --platform is explicit and mismatches.
# The mock python:3.12 is linux/arm64-only, but we request linux/amd64.
FROM --platform=linux/amd64 python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
RUN echo "hello"
