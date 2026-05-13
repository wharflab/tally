FROM alpine:3.18

# DL3021: Should trigger - multiple sources without trailing slash
COPY foo bar baz

# This is fine - multiple sources with trailing slash
COPY a b c /dest/

# This is fine - single source
COPY single /dest
