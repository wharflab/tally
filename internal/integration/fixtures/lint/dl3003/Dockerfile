FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

# DL3003: Should trigger - using cd instead of WORKDIR
RUN cd /opt && echo "hello"

# This is fine - using WORKDIR
WORKDIR /app
RUN echo "world"
