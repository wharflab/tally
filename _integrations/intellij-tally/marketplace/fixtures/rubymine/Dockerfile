# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
FROM ruby:3.3-slim@sha256:37d45ed5edf175280231e7361ac53bdccdcd476147a74d70cd44b8a7704ef85e AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:37d45ed5edf175280231e7361ac53bdccdcd476147a74d70cd44b8a7704ef85e AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
