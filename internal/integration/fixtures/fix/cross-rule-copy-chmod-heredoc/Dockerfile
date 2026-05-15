FROM python:3.12-slim@sha256:401f6e1a67dad31a1bd78e9ad22d0ee0a3b52154e6bd30e90be696bb6a3d7461
WORKDIR /app
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
RUN echo 'APP_ENV=production' > /app/.env
COPY --chown=app:app healthcheck.sh /app/healthcheck.sh
RUN chmod 755 /app/healthcheck.sh
RUN cat <<'CONF' > /etc/app.conf
log_level = info
workers = 4
CONF
ENTRYPOINT ["/app/entrypoint.sh"]
