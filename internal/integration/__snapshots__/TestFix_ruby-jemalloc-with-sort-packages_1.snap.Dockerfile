FROM ruby:3.3-slim
RUN apt-get install -y ca-certificates curl libjemalloc2
RUN ln -s /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so
ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"
