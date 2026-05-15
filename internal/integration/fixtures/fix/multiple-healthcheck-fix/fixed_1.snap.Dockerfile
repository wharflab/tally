FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
# [commented out by tally - Docker will ignore all but last HEALTHCHECK]: HEALTHCHECK CMD curl -f http://localhost/
HEALTHCHECK --interval=60s CMD wget -qO- http://localhost/
