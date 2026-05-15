FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
RUN apt-get update && apt-get install -y --no-install-recommends wget curl ca-certificates
RUN curl -fsSL https://example.com/one.tgz -o /tmp/one.tgz
RUN curl -fsSL https://example.com/two.tgz -o /tmp/two.tgz
