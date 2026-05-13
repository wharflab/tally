# Test: fail-fast skips async checks when fast rules produce SeverityError.
# Stage "builder" is duplicated -> DuplicateStageName (SeverityError).
# The slow image reference should never be fetched because fail-fast kicks in.
FROM slowfailfast:latest AS builder
RUN echo "first"

FROM slowfailfast:latest AS builder
RUN echo "duplicate name triggers DuplicateStageName error"
