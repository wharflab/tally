FROM python:3.12-slim
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf pip install -r requirements.txt
