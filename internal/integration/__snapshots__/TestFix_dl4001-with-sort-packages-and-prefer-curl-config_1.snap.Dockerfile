FROM ubuntu:22.04
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates
RUN wget -nv -O /tmp/one.tgz https://example.com/one.tgz
RUN wget -nv -O /tmp/two.tgz https://example.com/two.tgz
