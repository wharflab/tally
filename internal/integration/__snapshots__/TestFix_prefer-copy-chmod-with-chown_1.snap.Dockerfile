FROM alpine
COPY --chmod=755 --chown=appuser:appuser entrypoint.sh /app/entrypoint.sh
