# syntax=docker/dockerfile:1

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# VEX is supply-chain metadata. Prefer attaching it as an OCI attestation.
COPY *.vex.json /usr/share/vex/

