# Canonical violation: gem install bundler on official ruby:* base.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app
RUN gem install bundler
CMD ["bin/rails", "server"]

# Version-pinned form also triggers — pin matches lockfile's BUNDLED WITH
# would still be a no-op since the image's Bundler is already on PATH.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-pinned
RUN gem install bundler -v 2.5.6

# Chained-command form triggers; fix is a non-edit suggestion.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-chained
RUN apt-get update && gem install bundler && bundle install

# Compliant: alpine base not from official ruby:*, manual install of Ruby.
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS alpine-app
RUN apk add ruby ruby-dev && gem install bundler

# Compliant: jruby is a Ruby derivative, not official ruby:*; no Bundler
# pre-install guarantee, so manual install may be intentional.
FROM jruby:9.4@sha256:7d76d12a1a114f7d64c4625f25ef1ca4ac2a7cb5345e5bbb5c0984fe8a7da48d AS jruby-app
RUN gem install bundler

# Skipped: dev stage.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS dev
RUN gem install bundler
