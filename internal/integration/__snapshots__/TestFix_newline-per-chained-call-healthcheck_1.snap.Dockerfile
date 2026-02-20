FROM alpine:3.20
HEALTHCHECK CMD curl -f http://localhost/ \
	&& wget -qO- http://localhost/health \
	|| exit 1
