FROM python:3.13
RUN --mount=type=cache,target=/root/.cache/pip,id=pip pip install -r requirements.txt
