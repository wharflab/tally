FROM ruby:3.3-slim
RUN apt-get update && apt-get install -y --no-install-recommends libjemalloc2 \
    && rm -rf /var/lib/apt/lists/*
CMD ["bin/rails", "server"]
