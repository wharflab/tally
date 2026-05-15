FROM ruby:3.3-slim
COPY . .
RUN SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile
CMD ["bin/rails", "server"]
