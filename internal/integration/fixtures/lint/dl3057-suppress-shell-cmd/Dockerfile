# Test case for DL3057 smart suppression: shell-only CMD
# Interactive container with bare shell CMD — HEALTHCHECK is not beneficial
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    vim \
    && rm -rf /var/lib/apt/lists/*

CMD ["bash"]
