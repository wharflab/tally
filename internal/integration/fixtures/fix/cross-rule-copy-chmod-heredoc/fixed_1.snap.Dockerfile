FROM python:3.12-slim@sha256:401f6e1a67dad31a1bd78e9ad22d0ee0a3b52154e6bd30e90be696bb6a3d7461
WORKDIR /app
COPY --chmod=+x entrypoint.sh /app/entrypoint.sh

COPY <<EOF /app/.env
APP_ENV=production
EOF

COPY --chmod=755 --chown=app:app healthcheck.sh /app/healthcheck.sh

COPY <<EOF /etc/app.conf
log_level = info
workers = 4
EOF

ENTRYPOINT ["/app/entrypoint.sh"]
