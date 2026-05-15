FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS build
LABEL org.opencontainers.image.revision="builder-only"

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS metadata
LABEL org.opencontainers.image.revision="${VCS_REF}"

FROM metadata
ARG VCS_REF
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.source="https://github.com/example/demo" \
      com.docker.image.source.entrypoint="docker/Dockerfile" \
      com.example.owner="platform"
