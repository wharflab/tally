FROM ubuntu:22.04

# DL4006: Should warn - RUN with pipe but no pipefail
RUN wget -O - https://some.site | wc -l > /number

# OK: No pipe, just && chain
RUN apt-get update && apt-get install -y curl

FROM alpine:3.18
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# OK: pipefail is set
RUN curl -s https://example.com | grep test

FROM scratch

# DL4006: Should warn - new stage resets pipefail
RUN echo hello | tee /output
