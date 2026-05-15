FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
RUN --mount=type=cache,target=/var/cache/apt \
	--mount=type=bind,source=go.sum,target=go.sum \
	apt-get update \
	&& apt-get install -y curl \
	&& rm -rf /var/lib/apt/lists/*
LABEL org.opencontainers.image.title=myapp \
	org.opencontainers.image.version=1.0
HEALTHCHECK CMD curl -f http://localhost/ \
	|| exit 1
