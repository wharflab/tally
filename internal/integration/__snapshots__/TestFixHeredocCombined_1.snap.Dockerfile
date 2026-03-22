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
		EOF

# Case 4: prefer-copy-heredoc — consecutive RUNs appending to same file
	COPY <<EOF /app/data.txt
line1
line2
EOF

# Case 5: Should NOT trigger prefer-copy-heredoc — only 1 append (no base write)
	RUN echo "extra" >> /tmp/log.txt

# Case 6: prefer-copy-heredoc — echo with cat pattern
COPY <<EOF /etc/motd
Welcome to the build container
EOF

# Case 6b: prefer-copy-heredoc — BuildKit heredoc piped to cat
COPY <<EOF /aria2/aria2.conf
dir=/downloads
max-concurrent-downloads=16
EOF

# Case 6c: prefer-copy-heredoc — BuildKit heredoc piped to tee
COPY <<EOF /etc/supervisor/conf.d/app.conf
[program:app]
command=/usr/bin/app
autostart=true
EOF

# Case 7+8: prefer-run-heredoc — 2 shell-form RUNs + 1 heredoc RUN merged (3 RUNs, 4 commands)
	RUN <<-EOF
		set -e
		echo hello
		echo world
		echo "already heredoc"
		EOF

	WORKDIR /app

FROM alpine:3.20 AS runtime

# Case 9: prefer-copy-heredoc in second stage — file creation
	COPY --chmod=+x <<EOF /entrypoint.sh
#!/bin/sh
EOF

# Case 10: prefer-run-heredoc in second stage — 3 chained
	RUN <<-EOF
		set -e
		apk update
		apk add --no-cache curl
		apk add --no-cache jq
		EOF
