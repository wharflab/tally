FROM centos:7
RUN --mount=type=secret,id=YUM_CONF,target=/etc/yum.conf --mount=type=cache,target=/var/cache/yum,id=yum,sharing=locked yum update && yum install -y curl && curl http://127.0.0.1:8080
