# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
FROM ruby:3.3-slim@sha256:97fde153c07a5106a684cedebb49732fc14872c6b15c56d7c97752ee0a07e2b3 AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:97fde153c07a5106a684cedebb49732fc14872c6b15c56d7c97752ee0a07e2b3 AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
