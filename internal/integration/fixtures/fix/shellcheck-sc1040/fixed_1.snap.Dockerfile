FROM alpine:3.20

RUN <<SCRIPT
cat <<-EOF
hello
EOF
EOF
SCRIPT
