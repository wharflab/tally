FROM gcr.io/distroless/base-debian12:nonroot

LABEL org.opencontainers.image.title="tally" \
      org.opencontainers.image.description="Production-grade Dockerfile and Containerfile linter + formatter." \
      org.opencontainers.image.vendor="Wharflab" \
      org.opencontainers.image.licenses="GPL-3.0-only"

COPY --chmod=0755 tally /usr/bin/tally

ENTRYPOINT ["/usr/bin/tally"]
