# Test: truly undefined var is still caught after async resolution.
# PYTHON_VERSION exists in the python:3.12 image env, so using it is fine.
# But $NONEXISTENT does not exist in any image env — async resolution
# should still flag it.
FROM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8
LABEL version=$PYTHON_VERSION
COPY $NONEXISTENT /app/
