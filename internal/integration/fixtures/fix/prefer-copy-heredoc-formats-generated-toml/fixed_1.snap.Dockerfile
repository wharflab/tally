FROM ubuntu:22.04

COPY <<EOF /etc/app/config.toml
title = 'demo'

[owner]
name = 'tally'
EOF
