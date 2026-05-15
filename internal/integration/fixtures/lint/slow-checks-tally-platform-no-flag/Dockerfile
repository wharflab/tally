# Test: tally/platform-mismatch does NOT fire when no --platform is set.
# Even though the mock python:3.12 is linux/arm64-only and the host may be
# different, the rule only validates explicit --platform flags.
FROM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
RUN echo "hello"
