FROM ubuntu:22.04

# DL3047: Should warn - wget without progress flag
RUN wget http://example.com/large-file.tar.gz

# DL3047: Should warn - wget in pipeline without progress
RUN wget http://example.com/archive.tar.gz | tar xz

# OK: wget with --progress flag
RUN wget --progress=dot:giga http://example.com/large-file.tar.gz

# OK: wget with -q (quiet)
RUN wget -q http://example.com/small-file.txt

# OK: wget with -nv (no-verbose)
RUN wget -nv http://example.com/small-file.txt

# OK: wget with --output-file
RUN wget --output-file=/tmp/wget.log http://example.com/file.tar.gz

# DL3047: Should warn - wget in multiline RUN
RUN apt-get update && \
    wget http://example.com/script.sh && \
    chmod +x script.sh
