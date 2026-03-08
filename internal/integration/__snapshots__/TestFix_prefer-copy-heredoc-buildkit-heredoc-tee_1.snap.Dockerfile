FROM ubuntu:22.04
COPY <<EOF /etc/app.conf
[app]
key=value
EOF
