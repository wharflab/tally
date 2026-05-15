# Canonical violation: COPY . . without --chown after non-root USER.
FROM ruby:3.3-slim AS app-violation
WORKDIR /rails
USER rails:rails
COPY . .
CMD ["bin/rails", "server"]

# Compliant: --chown on the COPY.
FROM ruby:3.3-slim AS app-chown
WORKDIR /rails
USER rails:rails
COPY --chown=rails:rails . .
CMD ["bin/rails", "server"]

# Compliant: runtime chown -R covering all four state dirs before USER.
FROM ruby:3.3-slim AS app-runtime-chown
WORKDIR /rails
COPY . .
RUN chown -R rails:rails db log storage tmp
USER rails:rails
CMD ["bin/rails", "server"]

# Suppressed: root USER (tells us the stage isn't dropping privileges).
FROM ruby:3.3-slim AS app-root
WORKDIR /rails
USER root
COPY . .
CMD ["bin/rails", "server"]
