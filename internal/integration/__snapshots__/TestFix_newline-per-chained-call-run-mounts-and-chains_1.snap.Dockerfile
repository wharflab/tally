FROM alpine:3.20
RUN --mount=type=cache,target=/var/cache/apt \
    --mount=type=bind,source=go.sum,target=go.sum \
    apt-get update \
    && apt-get install -y curl
