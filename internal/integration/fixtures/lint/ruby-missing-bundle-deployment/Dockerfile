# Production-shaped stage running bundle install without BUNDLE_DEPLOYMENT.
# Canonical violation.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app
ENV RAILS_ENV="production"
COPY Gemfile Gemfile.lock ./
RUN bundle install

# Compliant: ENV BUNDLE_DEPLOYMENT="1" matches the Rails generator template.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-compliant
ENV RAILS_ENV="production"
ENV BUNDLE_DEPLOYMENT="1"
RUN bundle install

# Compliant: bundle config set --local deployment 'true' before install.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-config
RUN bundle config set --local deployment 'true' \
    && bundle install

# Compliant: bundle install --deployment (deprecated 2.x flag, but counts
# for THIS rule — deprecated-flags rule catches it separately).
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-deprecated-flag
RUN bundle install --deployment

# Stage marked dev: rule skips on principle.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS dev
RUN bundle install

# RAILS_ENV=development demotes the stage to non-production.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-explicit-dev
ENV RAILS_ENV="development"
RUN bundle install
