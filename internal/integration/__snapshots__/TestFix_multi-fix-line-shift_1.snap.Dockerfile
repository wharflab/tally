FROM ubuntu:22.04
WORKDIR /app
RUN make build
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN apt-get install curl
