# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769
# Already on BuildKit bind+cache, but missing --network=none — triggers.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app
RUN --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install

# Compliant: --network=none added.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-network-none
RUN --network=none \
    --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install --local

# Suppressed: bare bundle install (no bind/cache yet) — other rules
# cover that step.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-bare
COPY Gemfile Gemfile.lock ./
RUN bundle install
