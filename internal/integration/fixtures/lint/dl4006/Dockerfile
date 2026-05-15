FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

# DL4006: Should warn - RUN with pipe but no pipefail
RUN wget -O - https://some.site | wc -l > /number

# OK: No pipe, just && chain
RUN apt-get update && apt-get install -y curl

FROM alpine:3.18@sha256:de0eb0b3f2a47ba1eb89389859a9bd88b28e82f5826b6969ad604979713c2d4f
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# OK: pipefail is set
RUN curl -s https://example.com | grep test

FROM scratch

# DL4006: Should warn - new stage resets pipefail
RUN echo hello | tee /output
