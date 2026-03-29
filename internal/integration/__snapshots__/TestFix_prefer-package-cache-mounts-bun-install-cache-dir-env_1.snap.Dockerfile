FROM oven/bun:1.2
# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1
ENV BUN_INSTALL_CACHE_DIR=/tmp/bun-cache
RUN --mount=type=cache,target=/tmp/bun-cache,id=bun bun install
