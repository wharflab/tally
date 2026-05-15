FROM ruby:3.4-slim@sha256:a3ec946fc8771f4f2996fd75aaa79aa05ff5dbe1d32fb9bc4fd8ab95d8e7dbff
RUN bundle install
CMD ["bin/rails", "server"]
