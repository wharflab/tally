# Test: multi-stage with async resolution of base image env.
# Stage 1 FROM python:3.12 — PYTHON_VERSION is in the image env.
# Stage 2 FROM stage 1 — inherits PYTHON_VERSION via EffectiveEnv.
# $MISSING is truly undefined and should be caught.
FROM python:3.12 AS base
LABEL version=$PYTHON_VERSION

FROM base AS final
LABEL version=$PYTHON_VERSION
COPY $MISSING /app/
