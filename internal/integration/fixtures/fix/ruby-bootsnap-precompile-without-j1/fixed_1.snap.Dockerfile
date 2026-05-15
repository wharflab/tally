FROM ruby:3.3-slim

RUN bundle install \
    && bundle exec bootsnap precompile -j 1 app/ lib/

CMD ["bin/rails", "server"]
