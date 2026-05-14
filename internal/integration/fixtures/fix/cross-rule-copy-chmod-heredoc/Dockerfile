FROM python:3.12-slim
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
