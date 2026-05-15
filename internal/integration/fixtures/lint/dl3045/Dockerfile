# Test case for DL3045: COPY to relative destination without WORKDIR set
FROM debian:bookworm@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688 AS base
WORKDIR /app
COPY setup.sh setup.sh

FROM debian:bookworm@sha256:85019db29298555fd1a5f4bb57673ae989414a9884117c75d7a3e1a6cce21688
# This should trigger DL3045 - relative destination, no WORKDIR set
COPY app.bin app.bin

# This is OK - absolute destination
COPY config.yaml /etc/app/config.yaml
