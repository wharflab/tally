FROM alpine:3.21
# [commented out by tally - Docker will ignore all but last HEALTHCHECK]: HEALTHCHECK CMD curl -f http://localhost/
HEALTHCHECK --interval=60s CMD wget -qO- http://localhost/
