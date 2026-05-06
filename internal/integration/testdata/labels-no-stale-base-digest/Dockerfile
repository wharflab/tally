FROM alpine:3.20 AS build
LABEL org.opencontainers.image.base.digest="sha256:builderonly"

FROM alpine:3.20@sha256:1111111111111111111111111111111111111111111111111111111111111111 AS pinned
LABEL org.opencontainers.image.base.digest="sha256:1111111111111111111111111111111111111111111111111111111111111111"

FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.base.digest="${BASE_DIGEST}"

FROM metadata
ARG BASE_DIGEST
LABEL org.opencontainers.image.base.digest="sha256:2222222222222222222222222222222222222222222222222222222222222222"
LABEL org.opencontainers.image.title="demo"
