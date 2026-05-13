# Test: platform mismatch via manifest index (multi-arch image).
# The multiarch:latest index contains linux/amd64 and linux/arm64.
# Requesting linux/s390x triggers collectAvailablePlatforms which reports
# both available platforms in the violation message.
FROM --platform=linux/s390x multiarch:latest
RUN echo "hello"
