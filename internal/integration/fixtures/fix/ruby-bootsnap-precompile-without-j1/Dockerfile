FROM ruby:3.3-slim
RUN bundle install \
    && bundle exec bootsnap precompile app/ lib/
CMD ["bin/rails", "server"]
