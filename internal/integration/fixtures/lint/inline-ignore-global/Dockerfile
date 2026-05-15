# tally global ignore=buildkit/StageNameCasing,hadolint/DL3006
FROM alpine@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS Builder
RUN echo "build step"
FROM alpine@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS Final
COPY --from=Builder /etc/hostname /tmp/
RUN echo "final step"
