# syntax=docker/dockerfile:1
# Canonical violation.
FROM ruby:3.3-slim AS app
COPY Gemfile Gemfile.lock ./
RUN bundle install

# Compliant: bind mount + cache mount.
FROM ruby:3.3-slim AS app-bind
RUN --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install

# Suppressed: dev stage.
FROM ruby:3.3-slim AS dev
COPY Gemfile Gemfile.lock ./
RUN bundle install
