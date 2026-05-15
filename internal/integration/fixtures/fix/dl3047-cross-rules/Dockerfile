FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
RUN wget http://example.com/archive.tar.gz | tar -xz -C /opt
RUN wget http://example.com/config.json -O /etc/app/config.json
RUN curl -fsSL http://example.com/script.sh | sh
