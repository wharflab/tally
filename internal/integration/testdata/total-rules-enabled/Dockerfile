# Test fixture for validating total enabled rules count
# This test runs with all default rules enabled (no --ignore/--select flags)
FROM alpine:3.18
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
RUN echo "hello"
