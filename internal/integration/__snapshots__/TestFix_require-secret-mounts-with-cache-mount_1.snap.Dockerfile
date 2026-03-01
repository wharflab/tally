FROM python:3.12-slim
RUN --mount=type=cache,target=/root/.cache/pip --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf pip install -r requirements.txt
