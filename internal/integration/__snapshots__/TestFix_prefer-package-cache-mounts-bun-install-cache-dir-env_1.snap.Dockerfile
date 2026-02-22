FROM oven/bun:1.2
ENV BUN_INSTALL_CACHE_DIR=/tmp/bun-cache
RUN --mount=type=cache,target=/tmp/bun-cache,id=bun bun install
