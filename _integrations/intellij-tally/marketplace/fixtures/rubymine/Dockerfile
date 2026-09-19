# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
FROM ruby:3.3-slim@sha256:79f7a07363931fde1a5b312dee281fd62ddf56c33bdba0c2622af9b4621182fb AS build
WORKDIR /rails
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
RUN bin/rails assets:precompile

FROM ruby:3.3-slim@sha256:79f7a07363931fde1a5b312dee281fd62ddf56c33bdba0c2622af9b4621182fb AS production
WORKDIR /rails
COPY --from=build /rails /rails
CMD ["bin/rails", "server"]
