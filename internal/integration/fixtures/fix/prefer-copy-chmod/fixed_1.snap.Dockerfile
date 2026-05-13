FROM python:3.12-slim AS builder

RUN pip install --no-cache-dir gunicorn

FROM python:3.12-slim

COPY --from=builder /usr/local/bin/gunicorn /usr/local/bin/gunicorn

COPY --chmod=+x entrypoint.sh /app/entrypoint.sh

COPY --chmod=755 healthcheck.sh /app/healthcheck.sh

COPY --chown=appuser:appuser config.ini /app/config.ini

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["--config", "/app/config.ini"]
