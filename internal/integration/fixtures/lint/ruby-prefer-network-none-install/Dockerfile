# syntax=docker/dockerfile:1
# Already on BuildKit bind+cache, but missing --network=none — triggers.
FROM ruby:3.3-slim AS app
RUN --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install

# Compliant: --network=none added.
FROM ruby:3.3-slim AS app-network-none
RUN --network=none \
    --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install --local

# Suppressed: bare bundle install (no bind/cache yet) — other rules
# cover that step.
FROM ruby:3.3-slim AS app-bare
COPY Gemfile Gemfile.lock ./
RUN bundle install
