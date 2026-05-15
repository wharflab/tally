FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS builder
RUN apt-get update
RUN apt-get install -y curl
RUN apt-get install -y git
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
COPY --from=builder /usr/bin/curl /usr/bin/curl
RUN echo 'done'
