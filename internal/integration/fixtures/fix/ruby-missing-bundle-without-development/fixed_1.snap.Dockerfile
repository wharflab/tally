FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="development"
ENV RAILS_ENV="production"
COPY Gemfile Gemfile.lock ./
RUN bundle install
CMD ["bin/rails", "server"]
