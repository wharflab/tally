# Test: tally/platform-mismatch does NOT fire when no --platform is set.
# Even though the mock python:3.12 is linux/arm64-only and the host may be
# different, the rule only validates explicit --platform flags.
FROM python:3.12
RUN echo "hello"
