FROM alpine:3.20

# Invalid: unquoted strings in CMD
CMD [bash, -lc, "echo hello"]

# Invalid: single quotes in ENTRYPOINT
ENTRYPOINT ['/usr/bin/app', '--config', '/etc/app.conf']

# Invalid: trailing comma in RUN
RUN ["echo", "hello",]
