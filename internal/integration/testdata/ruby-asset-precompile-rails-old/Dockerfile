FROM ruby:3.2-slim
COPY . .
RUN bin/rails assets:precompile
CMD ["bin/rails", "server"]
