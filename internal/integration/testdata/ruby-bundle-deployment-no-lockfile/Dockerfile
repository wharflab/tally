FROM ruby:3.3-slim
ENV RAILS_ENV="production"
COPY Gemfile ./
RUN bundle install
CMD ["bin/rails", "server"]
