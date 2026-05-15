# EOL Ruby 2.x — error severity (long retired).
FROM ruby:2.7-slim AS app-2x
RUN bundle install

# EOL Ruby 3.0 — error severity.
FROM ruby:3.0 AS app-30
RUN bundle install

# EOL Ruby 3.1 — error severity.
FROM ruby:3.1.6 AS app-31
RUN bundle install

# Supported Ruby 3.3 — does not trigger.
FROM ruby:3.3-slim AS app-supported
RUN bundle install

# Supported Ruby 3.4 — does not trigger.
FROM ruby:3.4 AS app-latest
RUN bundle install

# JRuby — does not trigger (different release cadence).
FROM jruby:9.4 AS jruby-app
RUN bundle install
