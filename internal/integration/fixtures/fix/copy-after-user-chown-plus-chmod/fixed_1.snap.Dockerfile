FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
USER appuser
COPY --chown=appuser --chmod=+x entrypoint.sh /app/entrypoint.sh
