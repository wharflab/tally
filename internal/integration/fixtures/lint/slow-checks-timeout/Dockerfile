# Test: slow-checks timeout cancels delayed registry responses.
# No fast-path errors, so async checks actually run (no fail-fast).
# The slow image has a 30s delay; with --slow-checks-timeout=1s the
# runtime cancels the request and reports a timeout note on stderr.
FROM slowtimeout:latest
RUN echo "no fast-path errors here"
