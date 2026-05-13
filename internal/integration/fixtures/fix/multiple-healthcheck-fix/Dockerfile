FROM alpine:3.21
HEALTHCHECK CMD curl -f http://localhost/
HEALTHCHECK --interval=60s CMD wget -qO- http://localhost/
