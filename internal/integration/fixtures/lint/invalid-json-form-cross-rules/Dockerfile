FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# Invalid JSON: both tally/invalid-json-form (error) and
# buildkit/JSONArgsRecommended (error via config) fire on the same line.
# The .tally.toml elevates JSONArgsRecommended to error so both are visible.
CMD [bash, -lc, "echo hello"]
