# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM ruby:3.3-slim@sha256:eaeeb51411875c81f7f9b5353558decb8e3e1dbde38b18ff1d6c910f95cf860b AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:eaeeb51411875c81f7f9b5353558decb8e3e1dbde38b18ff1d6c910f95cf860b AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
