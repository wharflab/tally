FROM ubuntu:22.04
USER appuser
COPY --chown=appuser --chmod=+x entrypoint.sh /app/entrypoint.sh
