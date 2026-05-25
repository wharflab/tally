FROM alpine:3.20
COPY --chown=app:app src/ /app/
