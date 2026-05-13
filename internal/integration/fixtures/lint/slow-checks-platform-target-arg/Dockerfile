# Test: automatic $TARGETPLATFORM arg resolves via defaultFromArgs().
# DOCKER_DEFAULT_PLATFORM=linux/arm64 is set in the test environment,
# so $TARGETPLATFORM resolves to linux/arm64. The mock python:3.12 is
# also linux/arm64, so platforms match and no violation is emitted.
# This proves the meta arg was resolved (not skipped as unresolved).
FROM --platform=$TARGETPLATFORM python:3.12
RUN echo "automatic TARGETPLATFORM resolution"
