# Test: combined prefer-copy-heredoc + prefer-run-heredoc
# Exercises both rules simultaneously on a realistic multi-stage Dockerfile.
FROM ubuntu:22.04 AS builder

# Case 1: prefer-copy-heredoc — single RUN creating a config file via echo redirect
	COPY <<EOF /etc/nginx/conf.d/default.conf
server { listen 80; }
EOF

# Case 2: prefer-run-heredoc — 3 consecutive RUN instructions
	RUN <<-EOF
		set -e
		apt-get update
		apt-get install -y curl
		apt-get install -y git
		echo step1
		echo step2
		echo step3
		echo "line1" >/app/data.txt
		echo "line2" >>/app/data.txt
		echo "extra" >>/tmp/log.txt
		Welcome to the build container
		echo hello
		echo world
		echo "already heredoc"
		EOF

	WORKDIR /app

FROM alpine:3.20 AS runtime

# Case 9: prefer-copy-heredoc in second stage — file creation
	COPY --chmod=0755 <<EOF /entrypoint.sh
#!/bin/sh
EOF

# Case 10: prefer-run-heredoc in second stage — 3 chained
	RUN <<-EOF
		set -e
		apk update
		apk add --no-cache curl
		apk add --no-cache jq
		EOF
