FROM python:3.13
# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1
RUN --mount=type=cache,target=/root/.cache/pip,id=pip pip install -r requirements.txt
RUN --mount=type=cache,target=/root/.cache/uv,id=uv uv sync --frozen
RUN --mount=type=cache,target=/root/.bun/install/cache,id=bun bun install
