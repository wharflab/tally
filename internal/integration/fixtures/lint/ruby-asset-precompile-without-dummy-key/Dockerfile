# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769
# Build stage: invokes assets:precompile without the dummy key. The rule
# fires here because the stage is Ruby-shaped and not marked dev/test.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS builder
RUN bundle install && bundle exec rake assets:precompile

# Compliant: stage-level ENV provides the placeholder for every RUN below it.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-env-set
ENV SECRET_KEY_BASE_DUMMY=1
RUN bin/rails assets:precompile

# Compliant: inline placeholder on the offending command itself.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-inline
RUN SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile

# Compliant: BuildKit secret mount with env-injection — RAILS_MASTER_KEY is
# never written to image history.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-secret-mount
RUN --mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY \
    bin/rails assets:precompile

# Skipped: dev stage. The rule applies only to production-shaped images.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS dev
RUN bin/rails assets:precompile

# Final non-compliant runtime stage: rails CLI form, no placeholder anywhere.
# This is the canonical violation shape.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae
RUN bin/rails assets:precompile
CMD ["bin/rails", "server"]
