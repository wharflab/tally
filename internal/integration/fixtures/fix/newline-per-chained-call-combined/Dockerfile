FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN --mount=type=cache,target=/var/cache/apt --mount=type=bind,source=go.sum,target=go.sum apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
LABEL org.opencontainers.image.title=myapp org.opencontainers.image.version=1.0
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
