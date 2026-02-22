FROM node:20
ENV npm_config_cache=/tmp/npm-cache
RUN --mount=type=cache,target=/tmp/npm-cache,id=npm npm install
