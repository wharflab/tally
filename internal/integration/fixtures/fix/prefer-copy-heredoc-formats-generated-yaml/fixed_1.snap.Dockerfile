FROM ubuntu:22.04

COPY <<EOF /etc/app/config.yaml
b: 2
a: 1
EOF
