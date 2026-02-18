FROM python:3.13
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen
RUN --mount=type=cache,target=/root/.bun/install/cache bun install
