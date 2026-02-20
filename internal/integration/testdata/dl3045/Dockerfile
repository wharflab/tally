# Test case for DL3045: COPY to relative destination without WORKDIR set
FROM debian:bookworm AS base
WORKDIR /app
COPY setup.sh setup.sh

FROM debian:bookworm
# This should trigger DL3045 - relative destination, no WORKDIR set
COPY app.bin app.bin

# This is OK - absolute destination
COPY config.yaml /etc/app/config.yaml
