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
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ADD --unpack http://example.com/archive.tar.gz /opt
RUN wget --progress=dot:giga http://example.com/config.json -O /etc/app/config.json
RUN curl -fsSL http://example.com/script.sh | sh
