# Builder: installs libjemalloc2 + build tooling but never loads it. Non-final
# stage, so this rule does not fire here even though preload is missing.
FROM ruby:3.3-slim AS builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    libjemalloc2 build-essential \
    && rm -rf /var/lib/apt/lists/*

# Compliant final-named stage: installs jemalloc AND points LD_PRELOAD at the
# canonical path. No violation.
FROM ruby:3.3-slim AS app-compliant
RUN apt-get update && apt-get install -y libjemalloc2 \
    && ln -s /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so \
    && rm -rf /var/lib/apt/lists/*
ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"

# Compliant via Mastodon-style MALLOC_CONF: the jemalloc-specific knobs are
# evidence the allocator is loaded another way (e.g. via base-image LD_PRELOAD).
FROM ruby:3.3-slim AS app-malloc-conf
RUN apt-get install -y libjemalloc2
ENV MALLOC_CONF="narenas:2,background_thread:true,thp:never"

# Non-compliant final stage: installs libjemalloc2 but never preloads it.
# This is the only stage that should produce a violation, with a fixable
# canonical apt-style preload pair.
FROM ruby:3.3-slim
RUN apt-get update && apt-get install -y --no-install-recommends libjemalloc2 \
    && rm -rf /var/lib/apt/lists/*
CMD ["bin/rails", "server"]
