FROM ruby:3.3-slim
RUN apt-get update && apt-get install -y --no-install-recommends libjemalloc2 \
    && rm -rf /var/lib/apt/lists/*
RUN ln -sf /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so
ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"
CMD ["bin/rails", "server"]
