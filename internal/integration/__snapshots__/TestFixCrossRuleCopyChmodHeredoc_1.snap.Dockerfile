FROM python:3.12-slim
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
