FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS builder
STOPSIGNAL SIGKILL
RUN echo build

FROM nginx:1.27
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]

FROM postgres:16
STOPSIGNAL SIGSTOP
CMD ["postgres"]
