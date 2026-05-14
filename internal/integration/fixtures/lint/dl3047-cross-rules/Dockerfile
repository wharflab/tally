FROM ubuntu:22.04

# Triggers all three cooperating rules simultaneously:
#   DL3047: wget without --progress flag
#   DL4001: both wget and curl used in the same image
#   tally/prefer-add-unpack: wget piped to tar (should use ADD --unpack)
RUN wget http://example.com/archive.tar.gz | tar -xz -C /opt

# DL3047 only: standalone wget without tar (prefer-add-unpack won't fire)
RUN wget http://example.com/config.json -O /etc/app/config.json

# curl usage — makes DL4001 fire for the wget commands above
RUN curl -fsSL http://example.com/script.sh | sh
