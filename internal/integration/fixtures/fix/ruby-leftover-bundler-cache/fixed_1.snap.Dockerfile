FROM ruby:3.3-slim
COPY Gemfile Gemfile.lock ./
RUN bundle install
RUN rm -rf ~/.bundle/ "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git
CMD ["bin/rails", "server"]
