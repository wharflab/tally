FROM alpine:3.20 AS swap
LABEL org.opencontainers.image.description="example image" \
      org.opencontainers.image.source="https://github.com/example/demo" \
      org.opencontainers.image.title="demo" \
      org.opencontainers.image.licenses="Apache-2.0"

FROM alpine:3.20 AS single_line_multipair
LABEL org.opencontainers.image.description="d" org.opencontainers.image.title="t"

FROM alpine:3.20 AS dup_key_block
LABEL org.opencontainers.image.description="d" \
      org.opencontainers.image.title="first"
LABEL org.opencontainers.image.title="second"

FROM alpine:3.20 AS clean
LABEL org.opencontainers.image.title="t" \
      org.opencontainers.image.description="d"
