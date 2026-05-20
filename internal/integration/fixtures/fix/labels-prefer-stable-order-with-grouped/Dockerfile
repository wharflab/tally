FROM alpine:3.20 AS scattered_unordered
LABEL org.opencontainers.image.description="example image"
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.source="https://github.com/example/demo"

FROM alpine:3.20 AS multiline_unordered
LABEL org.opencontainers.image.description="d" \
      org.opencontainers.image.source="https://example.com" \
      org.opencontainers.image.title="t"

FROM alpine:3.20 AS single_line_unordered
LABEL org.opencontainers.image.description="d" org.opencontainers.image.title="t" org.opencontainers.image.source="https://example.com"
