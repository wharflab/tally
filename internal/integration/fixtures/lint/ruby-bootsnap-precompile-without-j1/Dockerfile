# Builder stage: bootsnap precompile without -j 1. Even though this is a
# non-final stage, the rule fires here too — bootsnap precompile crashes
# under QEMU regardless of which stage it runs in.
FROM ruby:3.3-slim AS builder
RUN bundle install \
    && bundle exec bootsnap precompile --gemfile

# Compliant stage: -j 1 makes the bootsnap call QEMU-safe per Rails generator.
FROM ruby:3.3-slim AS app-compliant
RUN bundle exec bootsnap precompile -j 1 app/ lib/

# Stage explicitly named dev: rule skips this on principle.
FROM ruby:3.3-slim AS dev
RUN bundle exec bootsnap precompile app/ lib/

# Compliant via the BUILDPLATFORM/TARGETPLATFORM guard: the user has
# explicitly avoided emulated paths, so the rule stands down.
FROM ruby:3.3-slim AS app-platform-guarded
ARG BUILDPLATFORM
ARG TARGETPLATFORM
RUN if [ "$BUILDPLATFORM" = "$TARGETPLATFORM" ]; then \
      bundle exec bootsnap precompile app/ lib/ ; \
    fi

# Non-precompile bootsnap subcommand: rule does not apply.
FROM ruby:3.3-slim AS bootsnap-doctor
RUN bootsnap doctor

# Final stage: bare bootsnap precompile with no -j 1. This is the
# canonical violation — fix should insert ` -j 1` after `precompile`.
FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile app/ lib/
CMD ["bin/rails", "server"]
