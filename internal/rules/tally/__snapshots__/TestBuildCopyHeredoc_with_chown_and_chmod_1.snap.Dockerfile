COPY --chown=appuser --chmod=0755 <<EOF /app/run.sh
#!/bin/sh
exec app
EOF