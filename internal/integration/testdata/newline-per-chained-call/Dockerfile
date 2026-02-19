FROM ubuntu:24.04

# RUN: chained commands on same line (violation)
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# RUN: already split (no violation)
RUN apt-get update \
	&& apt-get install -y git

# RUN: multiple mounts on same line (violation)
RUN --mount=type=cache,target=/var/cache/apt --mount=type=bind,source=go.sum,target=go.sum apt-get update

# RUN: heredoc with multiple mounts (violation on mounts only)
RUN --mount=type=cache,target=/var/cache/apt --mount=type=bind,source=go.sum,target=go.sum <<EOF
apt-get update
apt-get install -y curl
EOF

# RUN: mounts + chain combined (single violation)
RUN --mount=type=cache,target=/var/cache/apt --mount=type=bind,source=go.sum,target=go.sum apt-get update && apt-get install -y curl

# LABEL: multiple pairs on same line (violation)
LABEL org.opencontainers.image.title=myapp org.opencontainers.image.version=1.0 org.opencontainers.image.vendor=acme

# LABEL: already split (no violation)
LABEL org.opencontainers.image.title=myapp \
	org.opencontainers.image.version=1.0

# HEALTHCHECK CMD: chained commands (violation)
HEALTHCHECK CMD curl -f http://localhost/ && wget -qO- http://localhost/health || exit 1

# HEALTHCHECK CMD with options (violation)
HEALTHCHECK --interval=30s --timeout=10s CMD curl -f http://localhost/ && wget -qO- http://localhost/health

CMD ["sh"]
