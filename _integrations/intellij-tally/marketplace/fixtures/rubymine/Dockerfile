# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM ruby:3.3-slim@sha256:31f3f9b427927fddb6ef07f27127679cabd21098d0f6b2fec54ddee720276cc7 AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:31f3f9b427927fddb6ef07f27127679cabd21098d0f6b2fec54ddee720276cc7 AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
