# Production-shaped stage running bundle install without BUNDLE_WITHOUT.
# Canonical violation: development-group gems would ship into the image.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app
ENV RAILS_ENV="production"
COPY Gemfile Gemfile.lock ./
RUN bundle install

# Compliant: ENV BUNDLE_WITHOUT explicitly excludes development. The
# Rails generator template's exact wording.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-compliant
ENV RAILS_ENV="production"
ENV BUNDLE_WITHOUT="development"
COPY Gemfile Gemfile.lock ./
RUN bundle install

# Compliant: BUNDLE_WITHOUT covers BOTH dev and test groups.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-compliant-both
ENV BUNDLE_WITHOUT="development:test"
RUN bundle install

# Compliant via Bundler 2.5+ inverse selector.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-only
ENV BUNDLE_ONLY="default:production"
RUN bundle install

# Compliant: bundle config set --local without development before install.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-config
RUN bundle config set --local without development \
    && bundle install

# Stage explicitly marked dev: rule skips this on principle.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS dev
RUN bundle install

# Stage explicit RAILS_ENV=development: rule demotes to silent.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-explicit-dev
ENV RAILS_ENV="development"
RUN bundle install
