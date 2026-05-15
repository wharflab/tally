# Variant 3 violation: HEALTHCHECK uses curl, which on `ruby:*-slim`
# requires an extra `apt-get install curl` step. Switch to the Ruby
# stdlib's Net::HTTP form (already in the image).
FROM ruby:3.3-slim
HEALTHCHECK CMD curl -fsS http://127.0.0.1:3000/up || exit 1
CMD ["bin/rails", "server"]
