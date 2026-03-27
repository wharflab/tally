FROM alpine:3.20 AS builder
STOPSIGNAL SIGKILL
RUN echo build

FROM nginx:1.27
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]

FROM postgres:16
STOPSIGNAL SIGSTOP
CMD ["postgres"]
