FROM ubuntu:22.04 AS builder
	RUN <<-EOF
		set -e
		apt-get update
		apt-get install -y curl
		apt-get install -y git
		EOF
FROM alpine:3.20
	COPY --from=builder /usr/bin/curl /usr/bin/curl
	RUN echo 'done'
