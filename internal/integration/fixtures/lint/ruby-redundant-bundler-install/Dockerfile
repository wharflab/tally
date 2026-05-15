# Canonical violation: gem install bundler on official ruby:* base.
FROM ruby:3.3-slim AS app
RUN gem install bundler
CMD ["bin/rails", "server"]

# Version-pinned form also triggers — pin matches lockfile's BUNDLED WITH
# would still be a no-op since the image's Bundler is already on PATH.
FROM ruby:3.3-slim AS app-pinned
RUN gem install bundler -v 2.5.6

# Chained-command form triggers; fix is a non-edit suggestion.
FROM ruby:3.3-slim AS app-chained
RUN apt-get update && gem install bundler && bundle install

# Compliant: alpine base not from official ruby:*, manual install of Ruby.
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS alpine-app
RUN apk add ruby ruby-dev && gem install bundler

# Compliant: jruby is a Ruby derivative, not official ruby:*; no Bundler
# pre-install guarantee, so manual install may be intentional.
FROM jruby:9.4 AS jruby-app
RUN gem install bundler

# Skipped: dev stage.
FROM ruby:3.3-slim AS dev
RUN gem install bundler
