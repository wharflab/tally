# Meta-ARG with Bundler host credential: violation.
ARG BUNDLE_GITHUB__COM

FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-bundle-github
ENV BUNDLE_GITHUB__COM="user:token"
RUN bundle install

# Generic Bundler host pattern: violation.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-private-gems
ARG BUNDLE_GEMS__MYCOMPANY__COM
RUN bundle install

# RubyGems push key: violation.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-gem-push
ENV GEM_HOST_API_KEY="abc123"

# NPM token (used during asset:precompile): violation.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-yarn
ENV NPM_TOKEN="ghp_abc"
RUN yarn install

# Compliant: BuildKit secret mount.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-secret-mount
RUN --mount=type=secret,id=github_token,env=BUNDLE_GITHUB__COM \
    bundle install

# Suppressed: BUNDLE_PATH is config, not a credential (no host pattern).
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-bundle-path
ENV BUNDLE_PATH="/usr/local/bundle"
RUN bundle install
