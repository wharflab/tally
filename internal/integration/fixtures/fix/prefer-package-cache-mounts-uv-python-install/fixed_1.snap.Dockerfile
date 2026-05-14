FROM python:3.13
RUN --mount=type=cache,target=/root/.cache/uv,id=uv uv python install 3.12
