FROM ubuntu:22.04

# Should trigger: missing -L flag
RUN curl -fsSo /tmp/file.tar.gz https://example.com/file.tar.gz

# Should trigger: no flags at all
RUN curl https://example.com/script.sh | sh

# Should NOT trigger: has -L flag
RUN curl -fsSL https://example.com/file.tar.gz -o /tmp/file.tar.gz

# Should NOT trigger: has --location flag
RUN curl --location https://example.com/file.tar.gz -o /tmp/file.tar.gz

# Should NOT trigger: has -L in combined flags
RUN curl -sLo /tmp/file https://example.com/file

# Should NOT trigger: IP address
RUN curl http://127.0.0.1:8080/health

# Should NOT trigger: IP address with different port
RUN curl http://192.168.1.1/api/data

# Should NOT trigger: --location-trusted (implies redirect following)
RUN curl --location-trusted https://example.com/private/file.tar.gz -o /tmp/file.tar.gz

# Should trigger: multi-line RUN with curl missing -L
RUN apt-get update && \
    curl -fsSo /tmp/app.tar.gz https://example.com/app.tar.gz && \
    tar -xzf /tmp/app.tar.gz

# Should trigger: curl with flags but no -L
RUN curl -fsS -o /tmp/file https://example.com/file
