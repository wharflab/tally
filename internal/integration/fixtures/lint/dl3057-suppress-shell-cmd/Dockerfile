# Test case for DL3057 smart suppression: shell-only CMD
# Interactive container with bare shell CMD — HEALTHCHECK is not beneficial
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    vim \
    && rm -rf /var/lib/apt/lists/*

CMD ["bash"]
