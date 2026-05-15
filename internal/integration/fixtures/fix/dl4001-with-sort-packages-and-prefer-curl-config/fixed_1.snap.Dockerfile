FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget
RUN wget -nv -O /tmp/one.tgz https://example.com/one.tgz
RUN wget -nv -O /tmp/two.tgz https://example.com/two.tgz
