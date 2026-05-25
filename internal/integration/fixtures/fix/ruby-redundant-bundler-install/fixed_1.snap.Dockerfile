FROM ruby:3.3-slim
COPY Gemfile Gemfile.lock ./
RUN bundle install
CMD ["bin/rails", "server"]
