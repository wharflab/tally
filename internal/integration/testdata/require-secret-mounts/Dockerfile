FROM python:3.12-slim
RUN pip install -r requirements.txt
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf \
    pip install -r requirements.txt
RUN --mount=type=secret,id=wrong,target=/root/.config/pip/pip.conf \
    pip install flask
RUN apt-get update && apt-get install -y curl
