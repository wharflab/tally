FROM python:3.13@sha256:4f2d437a6b02de3c9f9aab4b90ba4bc50bd8ad825c5640c28a558c5639f6ded1
# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1
RUN --mount=type=cache,target=/root/.cache/pip,id=pip pip install -r requirements.txt
RUN --mount=type=cache,target=/root/.cache/uv,id=uv uv sync --frozen
RUN --mount=type=cache,target=/root/.bun/install/cache,id=bun bun install
