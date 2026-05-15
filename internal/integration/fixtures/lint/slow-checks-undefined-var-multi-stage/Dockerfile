# Test: multi-stage with async resolution of base image env.
# Stage 1 FROM python:3.12 — PYTHON_VERSION is in the image env.
# Stage 2 FROM stage 1 — inherits PYTHON_VERSION via EffectiveEnv.
# $MISSING is truly undefined and should be caught.
FROM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8 AS base
LABEL version=$PYTHON_VERSION

FROM base AS final
LABEL version=$PYTHON_VERSION
COPY $MISSING /app/
