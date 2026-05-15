FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# Invalid: unquoted strings in CMD
CMD [bash, -lc, "echo hello"]

# Invalid: single quotes in ENTRYPOINT
ENTRYPOINT ['/usr/bin/app', '--config', '/etc/app.conf']

# Invalid: trailing comma in RUN
RUN ["echo", "hello",]
