# Builder stage — COPY before USER is fine (root context)
FROM ubuntu:22.04 AS builder
COPY build.conf /etc/build.conf
RUN echo "build"

# Stage with USER that flows into child via FROM
FROM ubuntu:22.04 AS base
RUN useradd -r appuser
USER appuser

# Inherited USER from parent — violation on COPY (Alt 1 only, no USER to move)
FROM base AS app
COPY app /app

# Non-root USER with COPY and ADD — violations + both fix alts
FROM ubuntu:22.04 AS runtime
RUN useradd -r appuser
USER appuser
COPY app /app
ADD config.tar.gz /etc/app/
RUN setup.sh

# COPY with --chown already set — no violation
FROM ubuntu:22.04 AS ok-chown
USER appuser
COPY --chown=appuser:appuser app /app

# COPY after USER root — no violation
FROM ubuntu:22.04 AS root-user
USER root
COPY app /app

# RUN chown on same path suppresses the violation
FROM ubuntu:22.04 AS chown-suppressed
USER appuser
COPY app /app
EXPOSE 8080
RUN chown -R appuser:appuser /app

# RUN between USER and COPY — violation with Alt 1 only
FROM ubuntu:22.04 AS run-gap
USER appuser
RUN mkdir -p /app
COPY app /app
