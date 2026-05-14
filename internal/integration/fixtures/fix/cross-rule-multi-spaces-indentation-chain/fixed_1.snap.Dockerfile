FROM golang:1.23 AS build
	RUN go build -o /app ./...

FROM debian:bookworm-slim
	COPY --from=build /app /usr/local/bin/app
	RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
	RUN echo "    keepspaces" > /etc/motd
	CMD ["app"]
