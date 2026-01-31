# tally global ignore=buildkit/StageNameCasing,hadolint/DL3006
FROM alpine AS Builder
RUN echo "build step"
FROM alpine AS Final
COPY --from=Builder /etc/hostname /tmp/
RUN echo "final step"
