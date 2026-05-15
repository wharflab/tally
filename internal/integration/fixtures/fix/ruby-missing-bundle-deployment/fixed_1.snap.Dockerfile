FROM ruby:3.3-slim@sha256:a26bfb9409c02987e6b7f8649f0d4c71cc8a4a97475f3f1edfc2fc6a490021ae
ENV BUNDLE_DEPLOYMENT="1"
ENV RAILS_ENV="production"
COPY Gemfile Gemfile.lock ./
RUN bundle install
CMD ["bin/rails", "server"]
