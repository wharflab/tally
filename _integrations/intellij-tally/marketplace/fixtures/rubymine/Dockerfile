# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM ruby:3.3-slim@sha256:af55abc3e35217b7bf17a9f4a456b35e2e73550eaaa2e3be949f9db7f6e65396 AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:af55abc3e35217b7bf17a9f4a456b35e2e73550eaaa2e3be949f9db7f6e65396 AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
