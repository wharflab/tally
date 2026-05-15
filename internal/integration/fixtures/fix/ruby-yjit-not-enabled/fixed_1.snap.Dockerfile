FROM ruby:3.3-slim
ENV RUBY_YJIT_ENABLE="1"
COPY . .
CMD ["bin/rails", "server"]
