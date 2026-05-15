# Meta-ARG with literal RAILS_MASTER_KEY: violation.
ARG RAILS_MASTER_KEY=abc123def

# Stage with ENV SECRET_KEY_BASE: violation.
FROM ruby:3.3-slim AS app-with-secrets
ENV SECRET_KEY_BASE="literal-secret-here"
ENV DEVISE_SECRET_KEY="some-pepper"
RUN echo "this leaks the keys to image history"

# Compliant: SECRET_KEY_BASE=1 is the Rails-accepted placeholder.
FROM ruby:3.3-slim AS app-placeholder
ENV SECRET_KEY_BASE=1
RUN bin/rails assets:precompile

# Compliant: SECRET_KEY_BASE_DUMMY=1 (the canonical Rails 7.1+ build-time form).
FROM ruby:3.3-slim AS app-dummy
ENV SECRET_KEY_BASE_DUMMY=1
RUN bin/rails assets:precompile

# Compliant via BuildKit secret mount: secret never enters image history.
FROM ruby:3.3-slim AS app-secret-mount
RUN --mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY \
    bundle exec rails db:schema:load
