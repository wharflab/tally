# syntax=docker/dockerfile:1
# Canonical violation: bundle install without cache mount.
FROM ruby:3.3-slim AS app-violation
RUN bundle install
CMD ["bin/rails", "server"]

# Compliant: cache mount on the Rails-generator default path.
FROM ruby:3.3-slim AS app-compliant
RUN --mount=type=cache,id=bundler,target=/usr/local/bundle/cache,sharing=locked \
    bundle install

# Compliant: variable-form target path.
FROM ruby:3.3-slim AS app-variable
RUN --mount=type=cache,target=${BUNDLE_PATH}/cache \
    bundle install

# Suppressed: dev stage.
FROM ruby:3.3-slim AS dev
RUN bundle install
