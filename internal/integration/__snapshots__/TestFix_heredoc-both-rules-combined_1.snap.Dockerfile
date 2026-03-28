FROM ubuntu:22.04
COPY <<EOF /etc/nginx.conf
server {}
EOF
RUN apt-get update
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN apt-get install -y curl
RUN apt-get install -y git
