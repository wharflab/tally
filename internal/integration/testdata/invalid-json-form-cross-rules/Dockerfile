FROM alpine:3.20

# Invalid JSON: both tally/invalid-json-form (error) and
# buildkit/JSONArgsRecommended (error via config) fire on the same line.
# The .tally.toml elevates JSONArgsRecommended to error so both are visible.
CMD [bash, -lc, "echo hello"]
