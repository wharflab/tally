FROM ruby:3.4-slim
RUN bundle install
CMD ["bin/rails", "server"]
