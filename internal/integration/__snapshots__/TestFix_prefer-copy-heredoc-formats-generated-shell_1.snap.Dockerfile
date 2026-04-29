FROM ubuntu:22.04

COPY <<EOF /usr/local/bin/entrypoint.sh
if true; then
	echo hi
fi
EOF
