FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
# [tally] wget configuration for improved robustness
ENV WGETRC=/etc/wgetrc
COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF
RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz
