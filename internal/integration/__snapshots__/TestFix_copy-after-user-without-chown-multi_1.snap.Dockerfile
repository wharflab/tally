FROM ubuntu:22.04
USER appuser
COPY --chown=appuser a /a
COPY --chown=appuser b /b
RUN setup.sh
