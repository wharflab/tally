FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# DL3021: Should trigger - multiple sources without trailing slash
COPY foo bar baz

# This is fine - multiple sources with trailing slash
COPY a b c /dest/

# This is fine - single source
COPY single /dest
