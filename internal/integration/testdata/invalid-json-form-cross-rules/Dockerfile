FROM alpine:3.20

# Invalid JSON: both tally/invalid-json-form (error) and
# buildkit/JSONArgsRecommended (info) fire, but the Supersession processor
# suppresses the lower-severity JSONArgsRecommended at the same line.
CMD [bash, -lc, "echo hello"]
