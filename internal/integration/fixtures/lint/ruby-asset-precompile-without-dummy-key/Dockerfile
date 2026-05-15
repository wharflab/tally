# syntax=docker/dockerfile:1
# Build stage: invokes assets:precompile without the dummy key. The rule
# fires here because the stage is Ruby-shaped and not marked dev/test.
FROM ruby:3.3-slim AS builder
RUN bundle install && bundle exec rake assets:precompile

# Compliant: stage-level ENV provides the placeholder for every RUN below it.
FROM ruby:3.3-slim AS app-env-set
ENV SECRET_KEY_BASE_DUMMY=1
RUN bin/rails assets:precompile

# Compliant: inline placeholder on the offending command itself.
FROM ruby:3.3-slim AS app-inline
RUN SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile

# Compliant: BuildKit secret mount with env-injection — RAILS_MASTER_KEY is
# never written to image history.
FROM ruby:3.3-slim AS app-secret-mount
RUN --mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY \
    bin/rails assets:precompile

# Skipped: dev stage. The rule applies only to production-shaped images.
FROM ruby:3.3-slim AS dev
RUN bin/rails assets:precompile

# Final non-compliant runtime stage: rails CLI form, no placeholder anywhere.
# This is the canonical violation shape.
FROM ruby:3.3-slim
RUN bin/rails assets:precompile
CMD ["bin/rails", "server"]
