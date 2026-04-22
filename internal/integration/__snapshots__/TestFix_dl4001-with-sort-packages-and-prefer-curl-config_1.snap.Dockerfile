FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget
RUN wget -nv -O /tmp/one.tgz https://example.com/one.tgz
RUN wget -nv -O /tmp/two.tgz https://example.com/two.tgz
