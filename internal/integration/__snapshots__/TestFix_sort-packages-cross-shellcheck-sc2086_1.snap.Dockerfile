FROM alpine:3.20
RUN apk add --no-cache bar foo zoo  "$EXTRA_PKGS" 
