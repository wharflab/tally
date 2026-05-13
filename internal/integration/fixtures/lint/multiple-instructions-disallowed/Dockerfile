FROM alpine:3.21 AS builder
CMD echo "build"
RUN echo "hello"
CMD echo "run"
FROM alpine:3.21
ENTRYPOINT ["/bin/bash"]
ENTRYPOINT ["/bin/sh"]
HEALTHCHECK CMD curl -f http://localhost/
HEALTHCHECK --interval=60s CMD wget -qO- http://localhost/
CMD echo "final"
