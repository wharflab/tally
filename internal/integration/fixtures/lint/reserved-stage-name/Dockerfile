FROM alpine:3.21 AS scratch
RUN echo "should not name stage scratch"

FROM alpine:3.21 AS context
RUN echo "should not name stage context"

FROM alpine:3.21 AS builder
COPY --from=scratch /etc/alpine-release /tmp/
