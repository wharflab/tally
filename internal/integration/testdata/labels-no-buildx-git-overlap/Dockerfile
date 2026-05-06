FROM alpine:3.20 AS build
LABEL org.opencontainers.image.revision="builder-only"

FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.revision="${VCS_REF}"

FROM metadata
ARG VCS_REF
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.source="https://github.com/example/demo" \
      com.docker.image.source.entrypoint="docker/Dockerfile" \
      com.example.owner="platform"
