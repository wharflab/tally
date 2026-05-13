FROM python:3.12-slim
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf --mount=type=cache,target=/root/.cache/pip,id=pip pip install -r requirements.txt
