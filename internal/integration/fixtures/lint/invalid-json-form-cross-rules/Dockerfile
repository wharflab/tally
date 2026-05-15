FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# Invalid JSON: both tally/invalid-json-form (error) and
# buildkit/JSONArgsRecommended (error via config) fire on the same line.
# The .tally.toml elevates JSONArgsRecommended to error so both are visible.
CMD [bash, -lc, "echo hello"]
