FROM alpine:3.20

# tally ignore=SC2086;reason=covered by directive-span tests
RUN echo foo \
    && echo $1

CMD echo $1
ENTRYPOINT echo $1
HEALTHCHECK CMD echo $1

ONBUILD RUN echo $1
