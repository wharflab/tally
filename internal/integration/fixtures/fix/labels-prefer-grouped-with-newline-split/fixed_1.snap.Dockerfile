FROM alpine:3.20 AS scattered
LABEL org.opencontainers.image.title="demo" \
	org.opencontainers.image.description="example image" \
	org.opencontainers.image.source="https://github.com/example/demo"

FROM alpine:3.20 AS one_liner
LABEL org.opencontainers.image.title="oneline" \
	org.opencontainers.image.description="single line three pairs" \
	org.opencontainers.image.source="https://github.com/example/oneline"
