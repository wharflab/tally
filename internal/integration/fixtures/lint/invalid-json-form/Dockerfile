FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# Invalid: unquoted strings in CMD
CMD [bash, -lc, "echo hello"]

# Invalid: single quotes in ENTRYPOINT
ENTRYPOINT ['/usr/bin/app', '--config', '/etc/app.conf']

# Invalid: trailing comma in RUN
RUN ["echo", "hello",]
