# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM ruby:3.3-slim@sha256:47b93492bc6afc708dc4f2478c7f1867725c64b3bf43b94ea1bad2dac0314f02 AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:47b93492bc6afc708dc4f2478c7f1867725c64b3bf43b94ea1bad2dac0314f02 AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
