# Test: combined prefer-copy-heredoc + prefer-run-heredoc
# Exercises both rules simultaneously on a realistic multi-stage Dockerfile.
FROM ubuntu:22.04 AS builder

# Case 1: prefer-copy-heredoc — single RUN creating a config file via echo redirect
RUN echo "server { listen 80; }" > /etc/nginx/conf.d/default.conf

# Case 2: prefer-run-heredoc — 3 consecutive RUN instructions
RUN apt-get update
RUN apt-get install -y curl
RUN apt-get install -y git

# Case 3: prefer-run-heredoc — chained commands (3+)
RUN echo step1 && echo step2 && echo step3

# Case 4: prefer-copy-heredoc — consecutive RUNs appending to same file
RUN echo "line1" > /app/data.txt
RUN echo "line2" >> /app/data.txt

# Case 5: Should NOT trigger prefer-copy-heredoc — only 1 append (no base write)
RUN echo "extra" >> /tmp/log.txt

# Case 6: prefer-copy-heredoc — echo with cat pattern
RUN cat <<INNEREOF > /etc/motd
Welcome to the build container
INNEREOF

# Case 6b: prefer-copy-heredoc — BuildKit heredoc piped to cat
RUN <<EOF cat > /aria2/aria2.conf
dir=/downloads
max-concurrent-downloads=16
EOF

# Case 6c: prefer-copy-heredoc — BuildKit heredoc piped to tee
RUN <<EOF tee /etc/supervisor/conf.d/app.conf
[program:app]
command=/usr/bin/app
autostart=true
EOF

# Case 7+8: prefer-run-heredoc — 2 shell-form RUNs + 1 heredoc RUN merged (3 RUNs, 4 commands)
RUN echo hello
RUN echo world

# Case 8: prefer-run-heredoc — existing heredoc RUN merged with adjacent RUNs
RUN <<EOF
set -e
echo "already heredoc"
EOF

WORKDIR /app

FROM alpine:3.20 AS runtime

# Case 9: prefer-copy-heredoc in second stage — file creation
RUN echo '#!/bin/sh' > /entrypoint.sh && chmod +x /entrypoint.sh

# Case 10: prefer-run-heredoc in second stage — 3 chained
RUN apk update && apk add --no-cache curl && apk add --no-cache jq
