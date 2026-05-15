FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS build
LABEL org.opencontainers.image.revision="builder-only"

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS metadata
LABEL org.opencontainers.image.revision="${VCS_REF}"

FROM metadata
ARG VCS_REF
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.source="https://github.com/example/demo" \
      com.docker.image.source.entrypoint="docker/Dockerfile" \
      com.example.owner="platform"
