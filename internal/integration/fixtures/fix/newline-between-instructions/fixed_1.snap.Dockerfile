FROM alpine:3.20

RUN echo hello

ENV FOO=bar
ENV BAZ=qux

COPY . /app
