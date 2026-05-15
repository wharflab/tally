FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# tally ignore=SC2086;reason=covered by directive-span tests
RUN echo foo \
    && echo $1

CMD echo $1
ENTRYPOINT echo $1
HEALTHCHECK CMD echo $1

ONBUILD RUN echo $1
