FROM ubuntu:22.04

COPY <<EOF /etc/app/config.json
{
  "b": 2,
  "a": 1
}
EOF
