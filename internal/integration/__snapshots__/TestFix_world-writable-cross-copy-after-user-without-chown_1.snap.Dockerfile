FROM ubuntu:22.04
USER appuser
COPY --chown=appuser app /app
RUN chmod 775 /app
