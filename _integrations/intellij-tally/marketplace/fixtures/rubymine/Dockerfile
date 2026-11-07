# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM ruby:3.3-slim@sha256:8743a2d8ce3a33aed0de5c009dca0542e5fa85a4bd5b43170ce7b4af8c23809a AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:8743a2d8ce3a33aed0de5c009dca0542e5fa85a4bd5b43170ce7b4af8c23809a AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
