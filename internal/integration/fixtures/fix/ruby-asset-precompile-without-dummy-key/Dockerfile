FROM ruby:3.3-slim
COPY . .
RUN bin/rails assets:precompile
CMD ["bin/rails", "server"]
