FROM ubuntu:22.04 AS builder
RUN apt-get update
RUN apt-get install -y curl
RUN apt-get install -y git
FROM alpine:3.20
COPY --from=builder /usr/bin/curl /usr/bin/curl
RUN echo 'done'
