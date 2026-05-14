FROM alpine:3.19 AS base
ONBUILD COPY . /app
ONBUILD RUN echo hello
