FROM ubuntu:22.04
# [tally] wget configuration for improved robustness
ENV WGETRC=/etc/wgetrc
COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF
RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz
