FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile app/ lib/
CMD ["bin/rails", "server"]
