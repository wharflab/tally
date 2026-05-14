FROM alpine:3.20 AS builder
	RUN echo build
FROM scratch
	COPY --from=builder /app /app
