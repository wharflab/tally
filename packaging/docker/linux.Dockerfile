FROM gcr.io/distroless/base-debian12:nonroot

ARG SOURCE_URL=https://github.com/wharflab/tally

LABEL org.opencontainers.image.title="tally" \
      org.opencontainers.image.description="Production-grade Dockerfile and Containerfile linter + formatter." \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.url="${SOURCE_URL}" \
      org.opencontainers.image.documentation="${SOURCE_URL}" \
      org.opencontainers.image.vendor="Wharflab" \
      org.opencontainers.image.licenses="GPL-3.0-only"

COPY --chmod=0755 tally /usr/bin/tally

ENTRYPOINT ["/usr/bin/tally"]
