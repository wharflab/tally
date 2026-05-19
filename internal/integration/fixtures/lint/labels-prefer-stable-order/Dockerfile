FROM alpine:3.20 AS ordered
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.description="example image" \
      org.opencontainers.image.source="https://github.com/example/demo" \
      org.opencontainers.image.licenses="Apache-2.0"

FROM alpine:3.20 AS unordered
LABEL org.opencontainers.image.description="example image" \
      org.opencontainers.image.source="https://github.com/example/demo" \
      org.opencontainers.image.title="demo"

FROM alpine:3.20 AS only_unknown
LABEL com.example.zeta="z" \
      com.example.alpha="a"
