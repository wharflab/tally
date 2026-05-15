FROM centos:7@sha256:be65f488b7764ad3638f236b7b515b3678369a5124c47b8d32916d6487418ea4
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN --mount=type=secret,id=YUM_CONF,target=/etc/yum.conf --mount=type=cache,target=/var/cache/yum,id=yum,sharing=locked yum update && yum install -y curl && curl http://127.0.0.1:8080
