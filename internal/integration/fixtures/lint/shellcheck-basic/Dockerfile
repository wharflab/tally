FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# tally ignore=SC2086;reason=covered by directive-span tests
RUN echo foo \
    && echo $1

CMD echo $1
ENTRYPOINT echo $1
HEALTHCHECK CMD echo $1

ONBUILD RUN echo $1
