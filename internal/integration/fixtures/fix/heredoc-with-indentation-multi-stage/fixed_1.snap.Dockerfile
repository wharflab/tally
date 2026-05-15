FROM ubuntu:22.04 AS builder
	RUN <<-EOF
		set -e
		apt-get update
		apt-get install -y curl
		apt-get install -y git
		EOF
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
	COPY --from=builder /usr/bin/curl /usr/bin/curl
	RUN echo 'done'
