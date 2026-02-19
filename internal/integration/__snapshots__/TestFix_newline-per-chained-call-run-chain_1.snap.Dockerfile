FROM alpine:3.20
RUN apt-get update \
    && apt-get install -y curl
