FROM ubuntu:22.04
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
# [tally] wget configuration for improved robustness
ENV WGETRC=/etc/wgetrc
COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF
RUN apt-get update && apt-get install -y ca-certificates curl wget
RUN curl -fsSL https://example.com/install.sh | bash
RUN wget https://example.com/config.json -O /etc/app/config.json
