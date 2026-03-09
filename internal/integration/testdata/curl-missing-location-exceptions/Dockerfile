FROM ubuntu:22.04

# Exception: has -L flag
RUN curl -fsSL https://example.com/file.tar.gz -o /tmp/file.tar.gz

# Exception: has --location flag
RUN curl --location https://example.com/file.tar.gz -o /tmp/file.tar.gz

# Exception: has -L in combined flags
RUN curl -sLo /tmp/file https://example.com/file

# Exception: --location-trusted (implies redirect following)
RUN curl --location-trusted https://example.com/private/file.tar.gz -o /tmp/file.tar.gz

# Exception: IP address (local services don't redirect)
RUN curl http://127.0.0.1:8080/health

# Exception: IP address with port
RUN curl http://192.168.1.1/api/data

# Exception: IP address in multi-line RUN
RUN apt-get update && \
    curl http://10.0.0.5:9090/config && \
    echo done
