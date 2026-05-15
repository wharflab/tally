FROM node:22@sha256:e3ca095133ba41a0a73b009f19e4253f1a878e70bb9499f6a9d21b19d082bd91
RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/root/.npm,id=npm --mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked --mount=type=tmpfs,target=/tmp NPM_CONFIG_DEVDIR="/root/.cache/node-gyp" npm ci --omit=dev
