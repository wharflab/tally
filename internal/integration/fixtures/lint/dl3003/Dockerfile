FROM ubuntu:22.04

# DL3003: Should trigger - using cd instead of WORKDIR
RUN cd /opt && echo "hello"

# This is fine - using WORKDIR
WORKDIR /app
RUN echo "world"
