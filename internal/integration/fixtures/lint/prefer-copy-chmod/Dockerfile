FROM python:3.12-slim@sha256:401f6e1a67dad31a1bd78e9ad22d0ee0a3b52154e6bd30e90be696bb6a3d7461 AS builder

RUN pip install --no-cache-dir gunicorn

FROM python:3.12-slim@sha256:401f6e1a67dad31a1bd78e9ad22d0ee0a3b52154e6bd30e90be696bb6a3d7461

COPY --from=builder /usr/local/bin/gunicorn /usr/local/bin/gunicorn

COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

COPY healthcheck.sh /app/healthcheck.sh
RUN chmod 755 /app/healthcheck.sh

COPY --chown=appuser:appuser config.ini /app/config.ini

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["--config", "/app/config.ini"]
