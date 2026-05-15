FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS build
LABEL org.opencontainers.image.base.digest="sha256:builderonly"

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS pinned
LABEL org.opencontainers.image.base.digest="sha256:1111111111111111111111111111111111111111111111111111111111111111"

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS metadata
LABEL org.opencontainers.image.base.digest="${BASE_DIGEST}"

FROM metadata
ARG BASE_DIGEST
LABEL org.opencontainers.image.base.digest="sha256:2222222222222222222222222222222222222222222222222222222222222222"
LABEL org.opencontainers.image.title="demo"
