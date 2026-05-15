FROM ruby:3.3-slim
WORKDIR /rails
USER rails:rails
COPY .--chown=rails:rails  .
CMD ["bin/rails", "server"]
