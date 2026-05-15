# --without (deprecated): triggers.
FROM ruby:3.3-slim AS app-without
RUN bundle install --without development

# --deployment (deprecated): triggers.
FROM ruby:3.3-slim AS app-deployment
RUN bundle install --deployment

# --path (deprecated): triggers (FixSuggestion).
FROM ruby:3.3-slim AS app-path
RUN bundle install --path vendor/bundle

# --without --deployment combined: two violations.
FROM ruby:3.3-slim AS app-combined
RUN bundle install --without development --deployment

# Compliant: no deprecated flags.
FROM ruby:3.3-slim AS app-compliant
ENV BUNDLE_WITHOUT="development"
ENV BUNDLE_DEPLOYMENT="1"
RUN bundle install --jobs 4
