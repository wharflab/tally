FROM ubuntu:22.04

COPY <<EOF /aria2/aria2.conf
dir=/downloads
max-concurrent-downloads=16
EOF
