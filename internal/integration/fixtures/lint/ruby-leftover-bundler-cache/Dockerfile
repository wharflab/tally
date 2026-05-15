# Canonical violation: bundle install with no cleanup.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app
RUN bundle install
CMD ["bin/rails", "server"]

# Compliant: Rails-generator-style cleanup chained to install.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-cleaned
RUN bundle install \
    && rm -rf ~/.bundle/ "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git

# Compliant: cleanup in a later RUN (acceptable; avoids cache shipping
# in image, just adds a layer).
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-late-cleanup
RUN bundle install
RUN rm -rf ~/.bundle/

# Compliant: bundle clean --force.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-bundle-clean
RUN bundle install && bundle clean --force

# Skipped: builder stage whose layers are exported via COPY --from.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS builder
RUN bundle install

FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS final
COPY --from=builder /usr/local/bundle /usr/local/bundle

# Skipped: dev stage.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS dev
RUN bundle install
