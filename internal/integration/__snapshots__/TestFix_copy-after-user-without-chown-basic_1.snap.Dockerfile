FROM ubuntu:22.04
USER appuser
COPY --chown=appuser app /app
RUN setup.sh
