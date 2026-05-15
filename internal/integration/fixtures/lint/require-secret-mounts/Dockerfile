FROM python:3.12-slim@sha256:401f6e1a67dad31a1bd78e9ad22d0ee0a3b52154e6bd30e90be696bb6a3d7461
RUN pip install -r requirements.txt
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf \
    pip install -r requirements.txt
RUN --mount=type=secret,id=wrong,target=/root/.config/pip/pip.conf \
    pip install flask
RUN apt-get update && apt-get install -y curl
