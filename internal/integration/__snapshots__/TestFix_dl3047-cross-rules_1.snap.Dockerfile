FROM ubuntu:22.04
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ADD --unpack http://example.com/archive.tar.gz /opt
RUN wget --progress=dot:giga http://example.com/config.json -O /etc/app/config.json
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN curl -fsSL http://example.com/script.sh | sh
