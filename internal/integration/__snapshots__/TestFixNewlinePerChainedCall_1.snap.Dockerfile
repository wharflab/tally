FROM alpine:3.20
RUN --mount=type=cache,target=/var/cache/apt \
	--mount=type=bind,source=go.sum,target=go.sum \
	apt-get update \
	&& apt-get install -y curl \
	&& rm -rf /var/lib/apt/lists/*
LABEL org.opencontainers.image.title=myapp \
	org.opencontainers.image.version=1.0
HEALTHCHECK CMD curl -f http://localhost/ \
	|| exit 1
