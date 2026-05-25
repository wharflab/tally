FROM alpine:3.20 AS scattered
LABEL org.opencontainers.image.title="demo" \
	org.opencontainers.image.description="example image" \
	org.opencontainers.image.source="https://github.com/example/demo" \
	org.opencontainers.image.licenses="Apache-2.0"

FROM alpine:3.20 AS dynamic
ARG LABEL_PREFIX=com.example
LABEL org.opencontainers.image.title="dynamic"
LABEL "$LABEL_PREFIX.name"="dynamic"
LABEL org.opencontainers.image.source="https://github.com/example/dynamic"

FROM alpine:3.20 AS dup
LABEL org.opencontainers.image.title="first"
LABEL org.opencontainers.image.title="second"
LABEL org.opencontainers.image.description="desc"

FROM alpine:3.20 AS too_few
LABEL org.opencontainers.image.title="few"
LABEL org.opencontainers.image.description="few"

FROM alpine:3.20 AS already_grouped
LABEL org.opencontainers.image.title="grouped" \
      org.opencontainers.image.description="already grouped" \
      org.opencontainers.image.source="https://github.com/example/grouped" \
      org.opencontainers.image.licenses="Apache-2.0"
