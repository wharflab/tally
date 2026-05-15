FROM alpine:3.18@sha256:de0eb0b3f2a47ba1eb89389859a9bd88b28e82f5826b6969ad604979713c2d4f

# DL3021: Should trigger - multiple sources without trailing slash
COPY foo bar baz

# This is fine - multiple sources with trailing slash
COPY a b c /dest/

# This is fine - single source
COPY single /dest
