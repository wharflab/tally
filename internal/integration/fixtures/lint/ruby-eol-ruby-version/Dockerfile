# EOL Ruby 2.x — error severity (long retired).
FROM ruby:2.7-slim@sha256:39f68389fd3fe8c04ed896b10cc90ba4553a42e555ae18b37b41ea8c3ce965fd AS app-2x
RUN bundle install

# EOL Ruby 3.0 — error severity.
FROM ruby:3.0@sha256:dcb8a8a6ee981f949da505cb9f4f15cde696e7bcb088955bc1f92f23a4cd7a72 AS app-30
RUN bundle install

# EOL Ruby 3.1 — error severity.
FROM ruby:3.1.6@sha256:aaf664741b8de45efe7c5db430430784c11af7d069027b2f008b74a53b8882ae AS app-31
RUN bundle install

# Supported Ruby 3.3 — does not trigger.
FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae AS app-supported
RUN bundle install

# Supported Ruby 3.4 — does not trigger.
FROM ruby:3.4@sha256:6fc0fbac8a022d34a98325ce7f9cebdda813e3251d08c4777217824e3a3cf30b AS app-latest
RUN bundle install

# JRuby — does not trigger (different release cadence).
FROM jruby:9.4@sha256:7d76d12a1a114f7d64c4625f25ef1ca4ac2a7cb5345e5bbb5c0984fe8a7da48d AS jruby-app
RUN bundle install
